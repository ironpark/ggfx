// Copyright 2026 The ggfx Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package remote

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"sync"

	"github.com/gogpu/naga/ir"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/shader"
)

// Default resource limits for a Replayer.
const (
	DefaultMaxImages            = 1024
	DefaultMaxPixels            = 128 << 20 // 128M logical backing pixels (~512 MiB of RGBA8)
	DefaultMaxShaders           = 256
	DefaultMaxShaderSourceBytes = 1 << 20              // 1 MiB total WGSL source across all live shaders
	DefaultMaxUploadBytes       = 4 * DefaultMaxPixels // 512 MiB pixel-write + synthetic-clear payload per two-epoch window
	DefaultMaxPipelines         = 1024
)

// Limits bounds the logical budget of resources one Replayer may let an app make the viewer hold
// or stage across a two-epoch retirement window. Any zero or negative field uses the corresponding
// DefaultMax* value.
//
// These limits are a logical budget, not a measured physical GPU or OS memory cap:
//   - MaxImages counts all live image IDs tracked by the Replayer (both offscreen images and
//     logical screen image IDs) plus disposed offscreen images awaiting retirement.
//   - MaxPixels counts the backing pixel area of the active screen target (counted once,
//     via graphics.UnmanagedInternalImageSize in ScreenSize preflight and using the actual
//     Target.InternalWidth*Target.InternalHeight in Replay) plus live and pending-retirement
//     offscreen images.
//   - MaxShaders and MaxShaderSourceBytes count live shaders plus disposed shaders awaiting
//     retirement. MaxShaderSourceBytes (default 1 MiB) is the total across all live and
//     pending-retirement shaders combined, not a per-shader cap; there is no compile-time
//     deadline on shader compilation. Shader sources are additionally subject to the
//     untrusted-shader size limits documented in docs/shaders.md (enforced via
//     shader.CompileUntrusted).
//   - MaxPipelines counts distinct (shader, blend, destination pixel format) combinations drawn
//     with, as a logical bound on pipeline objects the app can cause the driver to build (not
//     measured driver pipeline memory); pipeline counts are released with the shader after
//     retirement.
//   - MaxUploadBytes caps aggregate payload bytes of explicit opWritePixels calls and synthetic
//     opNewImage clears across the active two-epoch retirement window. It does not measure
//     backend staging alignment/padding, vertex/index buffer uploads, or uniform uploads. With
//     the defaults, clearing and filling a single 8192×8192 RGBA8 image (2 × 256 MiB = 512 MiB)
//     uses the entire default upload allowance for two epochs; hosts expecting larger uploads
//     should size MaxUploadBytes accordingly.
//
// Callers MUST call (*Replayer).Retire once after each ended or presented host frame to credit
// resources disposed and upload payloads staged since the call before last (a two-flush delay).
// This approximates but does not prove backend GPU reclamation (for example, D3D12 frees a
// frame slot's deferred objects only when that slot comes round again, and Metal retains
// unfinished command-buffer generations until the GPU signals completion). Until Retire is
// called, disposed resources remain charged so a host that never calls Retire fails closed;
// Dispose releases all charges.
//
// In addition to these limits, a Replayer may lazily allocate at most one internal
// InternalImageSize(1)×InternalImageSize(1) (16×16) RGBA8 placeholder texture to bind for
// shader source slots left unbound by a draw. This fixed per-Replayer overhead is not
// counted against MaxImages, MaxPixels, or MaxUploadBytes so a stream at its configured cap
// does not fail merely for leaving a conditional source slot unbound; it is freed on Dispose.
type Limits struct {
	MaxImages            int
	MaxPixels            int64
	MaxShaders           int
	MaxShaderSourceBytes int
	// MaxUploadBytes bounds aggregate payload bytes of opWritePixels and synthetic opNewImage
	// clears across the active two-epoch window (excluding backend staging rounding, vertex/index
	// uploads, and uniform uploads).
	MaxUploadBytes int64
	// MaxPipelines bounds distinct (shader, blend, destination pixel format) combinations drawn
	// with across live and pending-retirement shaders.
	MaxPipelines int
}

// DefaultLimits returns the generous default resource limits for a Replayer.
func DefaultLimits() Limits {
	return Limits{
		MaxImages:            DefaultMaxImages,
		MaxPixels:            DefaultMaxPixels,
		MaxShaders:           DefaultMaxShaders,
		MaxShaderSourceBytes: DefaultMaxShaderSourceBytes,
		MaxUploadBytes:       DefaultMaxUploadBytes,
		MaxPipelines:         DefaultMaxPipelines,
	}
}

func resolveLimits(l Limits) Limits {
	d := DefaultLimits()
	if l.MaxImages > 0 {
		d.MaxImages = l.MaxImages
	}
	if l.MaxPixels > 0 {
		d.MaxPixels = l.MaxPixels
	}
	if l.MaxShaders > 0 {
		d.MaxShaders = l.MaxShaders
	}
	if l.MaxShaderSourceBytes > 0 {
		d.MaxShaderSourceBytes = l.MaxShaderSourceBytes
	}
	if l.MaxUploadBytes > 0 {
		d.MaxUploadBytes = l.MaxUploadBytes
	}
	if l.MaxPipelines > 0 {
		d.MaxPipelines = l.MaxPipelines
	}
	return d
}

// Target is the viewer's image that the app's screen draws go to: an offscreen image of the
// driver's whose top-left Width by Height pixels are the screen, and which draws at its internal
// size.
type Target struct {
	Image                         graphicsdriver.Image
	Width, Height                 int
	InternalWidth, InternalHeight int
}

// Result is what a batch did.
type Result struct {
	// Presented reports that the batch ended with a present of the screen.
	Presented bool
	// Damage is what the present changed, in the screen's pixels; nil is all of it.
	Damage []image.Rectangle
	// Draws counts the draw calls the batch made.
	Draws int
}

type replayedImage struct {
	img           graphicsdriver.Image // nil for the screen, which is the replay's target
	width, height int
	pixels        int64 // internal texture pixel count (0 for the screen)
	format        graphicsdriver.PixelFormat
	screen        bool
}

// bounds returns the pixels of i a driver call may touch: those of its texture, which is of the
// image's internal size, but for the screen, which is the target's top-left width by height.
func (i *replayedImage) bounds() image.Rectangle {
	if i.screen {
		return image.Rect(0, 0, i.width, i.height)
	}
	return image.Rect(0, 0, graphics.InternalImageSize(i.width), graphics.InternalImageSize(i.height))
}

type pipelineKey struct {
	blend  graphicsdriver.Blend
	format graphicsdriver.PixelFormat
}

type replayedShader struct {
	shader       graphicsdriver.Shader
	program      *shader.Program
	seq          uint64
	sourceBytes  int
	usedTextures uint8
	pipelines    map[pipelineKey]struct{}
}

// retiredCharges holds resource charges from disposed resources and staged uploads accumulated
// during one retirement epoch.
type retiredCharges struct {
	images      int
	pixels      int64
	shaders     int
	shaderBytes int64
	pipelines   int
	uploadBytes int64
}

// Replayer runs one app's batches on the viewer's driver. Each app gets its own: the ids are the
// app's, and Dispose frees what one app made when it goes away. Replay, ReadPixels, and Dispose
// must still run on the render thread inside a command batch of the viewer's; Replayer serializes
// its internal state with a mutex so Retire (and inspection methods such as ScreenSize or Limits)
// may be called concurrently from another goroutine.
type Replayer struct {
	mu             sync.Mutex
	images         map[uint32]*replayedImage
	shaders        map[uint32]*replayedShader
	activeScreenID uint32
	emptyImg       graphicsdriver.Image // lazily created 16x16 transparent image for unbound shader slots

	limits          Limits
	livePixels      int64 // backing pixels of live offscreen images
	screenPixels    int64 // physical backing pixels of the active screen target
	liveShaderBytes int64
	livePipelines   int
	nextShaderSeq   uint64

	// Two-epoch retirement ring: curEpoch holds charges from disposed resources and staged uploads
	// since the last Retire call; prevEpoch holds charges from the epoch before that. Callers must
	// call Retire once after each ended/presented host frame to release prevEpoch.
	curEpoch  retiredCharges
	prevEpoch retiredCharges

	// Last uniform versions seen in the current Replay batch. Issuing a fresh driver-side version
	// whenever the (app version, shader) pair (or screen retargeting for internal uniforms) changes
	// prevents an untrusted stream from aliasing uniform blocks across shaders or colliding with
	// the viewer's own versions.
	lastInternalApp    uint64
	lastInternalShader uint64
	lastInternalScreen bool
	lastInternalDriver uint64

	lastUserApp    uint64
	lastUserShader uint64
	lastUserDriver uint64

	// vertices and indices are the counts the last SetVertices gave, which draws are checked
	// against.
	vertices, indices int

	// Scratch slices reused across replayed commands to avoid per-draw allocations.
	regions     []graphicsdriver.DstRegion
	retargetBuf []uint32
	pixelsArgs  []graphicsdriver.PixelsArgs
}

// NewReplayer returns the replayer of a new app with DefaultLimits.
func NewReplayer() *Replayer {
	return &Replayer{
		images:  map[uint32]*replayedImage{},
		shaders: map[uint32]*replayedShader{},
		limits:  DefaultLimits(),
	}
}

// SetLimits sets the resource limits enforced on subsequent operations. Any zero or negative
// field uses the corresponding default limit.
func (r *Replayer) SetLimits(limits Limits) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limits = resolveLimits(limits)
}

// Limits returns the resource limits currently enforced by r.
func (r *Replayer) Limits() Limits {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.limits
}

// Retire advances the retirement epoch by one host flush. Call it once after each ended or
// presented host frame: charges for resources disposed (images, pixels, shaders, shader source
// bytes, pipelines) and upload payloads staged since the call before last are credited after
// two host flushes. This two-flush window approximates but does not prove backend reclamation
// (for example, D3D12 frees a frame slot's deferred objects only when that slot comes round
// again, and Metal holds unfinished command-buffer generations). Until Retire is called,
// disposed resources remain charged against Limits so a host that never calls Retire fails
// closed; Dispose releases all charges.
//
// Retire is safe to call concurrently with Replay, ScreenSize, and Dispose on the same Replayer.
func (r *Replayer) Retire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prevEpoch = r.curEpoch
	r.curEpoch = retiredCharges{}
}

func (r *Replayer) pendingImages() int   { return r.curEpoch.images + r.prevEpoch.images }
func (r *Replayer) pendingPixels() int64 { return r.curEpoch.pixels + r.prevEpoch.pixels }
func (r *Replayer) pendingShaders() int  { return r.curEpoch.shaders + r.prevEpoch.shaders }
func (r *Replayer) pendingShaderBytes() int64 {
	return r.curEpoch.shaderBytes + r.prevEpoch.shaderBytes
}
func (r *Replayer) pendingPipelines() int   { return r.curEpoch.pipelines + r.prevEpoch.pipelines }
func (r *Replayer) epochUploadBytes() int64 { return r.curEpoch.uploadBytes + r.prevEpoch.uploadBytes }

// internalSize64 rounds x up to a multiple of 16 in checked int64, matching
// graphics.InternalImageSize without risking 32-bit int overflow.
func internalSize64(x int64) (int64, bool) {
	const grain = 16
	if x <= 0 || x > math.MaxInt64-(grain-1) {
		return 0, false
	}
	return ((x + grain - 1) / grain) * grain, true
}

func checkedMul64(a, b int64) (int64, bool) {
	if a <= 0 || b <= 0 || a > math.MaxInt64/b {
		return 0, false
	}
	return a * b, true
}

func backingPixels64(w, h int) (int64, bool) {
	iw, ok := internalSize64(int64(w))
	if !ok {
		return 0, false
	}
	ih, ok := internalSize64(int64(h))
	if !ok {
		return 0, false
	}
	return checkedMul64(iw, ih)
}

// unmanagedScreenAxes64 computes the backing texture dimensions that the viewer allocates for
// an unmanaged w×h screen target via graphics.UnmanagedInternalImageSize, guarding against
// int overflow on 32-bit platforms before calling the helper.
func unmanagedScreenAxes64(w, h, maxImageSize int) (iw, ih int64, ok bool) {
	const maxSafeInput = math.MaxInt - 16
	if w <= 0 || h <= 0 || w > maxSafeInput || h > maxSafeInput || maxImageSize > maxSafeInput {
		return 0, 0, false
	}
	return int64(graphics.UnmanagedInternalImageSize(w, maxImageSize)),
		int64(graphics.UnmanagedInternalImageSize(h, maxImageSize)),
		true
}

func (r *Replayer) checkScreenSize(w, h, maxImageSize int) (int64, error) {
	if maxImageSize > 0 && (w > maxImageSize || h > maxImageSize) {
		return 0, fmt.Errorf("remote: screen of %dx%d exceeds max image size %d", w, h, maxImageSize)
	}
	iw, ih, ok := unmanagedScreenAxes64(w, h, maxImageSize)
	if !ok {
		return 0, fmt.Errorf("remote: screen of %dx%d exceeds live pixel limit (%d)", w, h, r.limits.MaxPixels)
	}
	if maxImageSize > 0 && (iw > int64(maxImageSize) || ih > int64(maxImageSize)) {
		return 0, fmt.Errorf("remote: screen of %dx%d (%dx%d physical) exceeds max image size %d", w, h, iw, ih, maxImageSize)
	}
	pixels, ok := checkedMul64(iw, ih)
	if !ok || pixels > r.limits.MaxPixels-r.livePixels-r.pendingPixels() {
		return 0, fmt.Errorf("remote: screen of %dx%d exceeds live pixel limit (%d)", w, h, r.limits.MaxPixels)
	}
	return pixels, nil
}

// ScreenSize parses batch's header and validates its screen size against maxImageSize (when > 0)
// and r's live pixel budget (together with r's live and pending-retirement offscreen images,
// using graphics.UnmanagedInternalImageSize). Callers should call this before allocating a screen
// target for Replay. It returns 0, 0, nil when the batch names no screen.
func (r *Replayer) ScreenSize(batch []byte, maxImageSize int) (width, height int, err error) {
	h, err := ParseHeader(batch)
	if err != nil {
		return 0, 0, err
	}
	if h.Screen == 0 {
		return 0, 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.checkScreenSize(h.Width, h.Height, maxImageSize); err != nil {
		return 0, 0, err
	}
	return h.Width, h.Height, nil
}

// Live returns how many images and shaders the app has on the viewer's side.
func (r *Replayer) Live() (images, shaders int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, i := range r.images {
		if !i.screen {
			images++
		}
	}
	return images, len(r.shaders)
}

func (r *Replayer) internalVersion(app, shaderSeq uint64, retargeted bool) uint64 {
	if r.lastInternalDriver == 0 || r.lastInternalApp != app || r.lastInternalShader != shaderSeq || r.lastInternalScreen != retargeted {
		r.lastInternalApp = app
		r.lastInternalShader = shaderSeq
		r.lastInternalScreen = retargeted
		r.lastInternalDriver = graphicsdriver.NewUniformVersion()
	}
	return r.lastInternalDriver
}

func (r *Replayer) userVersion(app, shaderSeq uint64) uint64 {
	if r.lastUserDriver == 0 || r.lastUserApp != app || r.lastUserShader != shaderSeq {
		r.lastUserApp = app
		r.lastUserShader = shaderSeq
		r.lastUserDriver = graphicsdriver.NewUniformVersion()
	}
	return r.lastUserDriver
}

// validateShaderGlobals checks that every global variable reachable in p's compacted IR conforms
// to the ggfx shader contract: handle-space globals must be texture_2d<f32> at prelude source
// texture bindings 0..p.TextureCount-1, uniform globals must be the internal or user uniform
// block, private globals must be unbound, and no other address space (storage, immediate, etc.)
// is permitted. It returns the bitmask of source texture slots referenced by the shader.
func validateShaderGlobals(p *shader.Program) (uint8, error) {
	if p == nil || p.Module == nil {
		return 0, errors.New("remote: compiled shader has no IR module")
	}
	var mask uint8
	var seenInternal, seenUser bool
	for i := range p.Module.GlobalVariables {
		g := &p.Module.GlobalVariables[i]
		switch g.Space {
		case ir.SpacePrivate:
			if g.Binding != nil {
				return 0, fmt.Errorf("remote: private shader global %q has resource binding", g.Name)
			}
		case ir.SpaceUniform:
			if g.Binding == nil {
				return 0, fmt.Errorf("remote: uniform shader global %q has no binding", g.Name)
			}
			switch *g.Binding {
			case shader.InternalUniformBinding:
				if seenInternal {
					return 0, fmt.Errorf("remote: duplicate internal uniform binding on %q", g.Name)
				}
				seenInternal = true
			case shader.UserUniformBinding:
				if seenUser {
					return 0, fmt.Errorf("remote: duplicate user uniform binding on %q", g.Name)
				}
				seenUser = true
			default:
				return 0, fmt.Errorf("remote: shader uniform %q has disallowed binding (%d,%d)", g.Name, g.Binding.Group, g.Binding.Binding)
			}
		case ir.SpaceHandle:
			if g.Binding == nil {
				return 0, fmt.Errorf("remote: handle shader global %q has no binding", g.Name)
			}
			slot := -1
			for k := range p.TextureCount {
				if k < graphics.ShaderSrcImageCount && *g.Binding == shader.TextureBinding(k) {
					slot = k
					break
				}
			}
			if slot < 0 {
				return 0, fmt.Errorf("remote: shader resource %q has disallowed binding (%d,%d)", g.Name, g.Binding.Group, g.Binding.Binding)
			}
			if mask&(1<<slot) != 0 {
				return 0, fmt.Errorf("remote: duplicate shader texture binding for slot %d", slot)
			}
			if int(g.Type) >= len(p.Module.Types) {
				return 0, fmt.Errorf("remote: shader resource %q has invalid type handle", g.Name)
			}
			imgType, ok := p.Module.Types[g.Type].Inner.(ir.ImageType)
			if !ok || imgType.Dim != ir.Dim2D || imgType.Arrayed || imgType.Multisampled ||
				imgType.Class != ir.ImageClassSampled || imgType.SampledKind != ir.ScalarFloat {
				return 0, fmt.Errorf("remote: shader resource %q at slot %d must be texture_2d<f32>", g.Name, slot)
			}
			mask |= 1 << slot
		default:
			return 0, fmt.Errorf("remote: shader global %q uses disallowed address space %d", g.Name, g.Space)
		}
	}
	return mask, nil
}

// Replay runs batch on g. target is the image the screen's draws go to; it may be nil for a batch
// whose header names no screen. A batch that fails a check returns an error: the operations
// before it have run, and the app's state on this side is then unreliable.
//
// Disposed resources and staged upload bytes remain charged against Limits across Replay calls
// until the host advances the retirement window with (*Replayer).Retire (two-epoch delay) or
// calls (*Replayer).Dispose.
func (r *Replayer) Replay(g graphicsdriver.Graphics, batch []byte, target *Target) (res Result, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		// The checks are meant to keep every call the driver gets valid; a panic past them is
		// still the app's input, which must not take the viewer down.
		if e := recover(); e != nil {
			err = fmt.Errorf("remote: replaying the batch: %v", e)
		}
	}()
	h, err := ParseHeader(batch)
	if err != nil {
		return Result{}, err
	}
	maxSize := g.MaxImageSize()
	if h.Screen != 0 {
		if target == nil || target.Image == nil || target.Width != h.Width || target.Height != h.Height {
			return Result{}, fmt.Errorf("remote: the batch draws to a screen of %dx%d, which the target is not", h.Width, h.Height)
		}
		if h.Width > maxSize || h.Height > maxSize {
			return Result{}, fmt.Errorf("remote: screen of %dx%d exceeds max image size %d", h.Width, h.Height, maxSize)
		}
		if target.InternalWidth < target.Width || target.InternalHeight < target.Height ||
			target.InternalWidth > maxSize || target.InternalHeight > maxSize {
			return Result{}, fmt.Errorf("remote: target internal size %dx%d is invalid for screen %dx%d (max %d)", target.InternalWidth, target.InternalHeight, target.Width, target.Height, maxSize)
		}
		targetPixels, ok := checkedMul64(int64(target.InternalWidth), int64(target.InternalHeight))
		if !ok || targetPixels > r.limits.MaxPixels-r.livePixels-r.pendingPixels() {
			return Result{}, fmt.Errorf("remote: screen target of %dx%d (%dx%d physical) exceeds live pixel limit (%d)", target.Width, target.Height, target.InternalWidth, target.InternalHeight, r.limits.MaxPixels)
		}
		r.screenPixels = targetPixels
	} else if r.activeScreenID == 0 {
		// Keep an existing active screen's charge until opDisposeImage explicitly disposes it so
		// a malformed Screen=0 batch cannot allocate offscreen images past MaxPixels before the
		// end-of-batch header check rejects it.
		r.screenPixels = 0
	}
	// Reset vertex/index counts and uniform version state on each Replay: between replays the
	// viewer may overwrite or clear the driver's buffers or resize the target.
	r.vertices, r.indices = 0, 0
	r.lastInternalDriver, r.lastUserDriver = 0, 0
	d := &dec{b: batch, p: HeaderSize}
	for d.more() {
		op := d.u8()
		if err := r.op(g, d, op, target, &res); err != nil {
			return res, err
		}
		if d.err != nil {
			return res, d.err
		}
	}
	if d.err != nil {
		return res, d.err
	}
	// Validate that the batch header matches the final active screen at the end of the batch.
	if r.activeScreenID == 0 {
		if h.Screen != 0 {
			return res, fmt.Errorf("remote: batch header names screen %d (%dx%d), but no screen is active", h.Screen, h.Width, h.Height)
		}
	} else {
		sc := r.images[r.activeScreenID]
		if sc == nil || h.Screen != r.activeScreenID || h.Width != sc.width || h.Height != sc.height {
			return res, fmt.Errorf("remote: batch header screen (%d, %dx%d) does not match active screen (%d, %dx%d)", h.Screen, h.Width, h.Height, r.activeScreenID, sc.width, sc.height)
		}
	}
	return res, nil
}

func (r *Replayer) newID(id uint32) error {
	if id == 0 {
		return errors.New("remote: id 0")
	}
	if _, ok := r.images[id]; ok {
		return fmt.Errorf("remote: id %d is taken", id)
	}
	if _, ok := r.shaders[id]; ok {
		return fmt.Errorf("remote: id %d is taken", id)
	}
	return nil
}

func (r *Replayer) image(id uint32) (*replayedImage, error) {
	i := r.images[id]
	if i == nil {
		return nil, fmt.Errorf("remote: no image %d", id)
	}
	return i, nil
}

// driverImage returns i's image of the driver's: the target for the active screen.
func (r *Replayer) driverImage(id uint32, i *replayedImage, target *Target) (graphicsdriver.Image, error) {
	if !i.screen {
		return i.img, nil
	}
	if id != r.activeScreenID {
		return nil, fmt.Errorf("remote: screen %d is not the active screen (%d)", id, r.activeScreenID)
	}
	if target == nil || target.Image == nil || target.Width != i.width || target.Height != i.height {
		return nil, fmt.Errorf("remote: the screen of %dx%d has no target of its size", i.width, i.height)
	}
	return target.Image, nil
}

// createClearedImage creates a driver image and zeroes its entire backing texture, disposing
// the native image (and charging the current retirement epoch for it) if clearing returns an
// error or panics before ownership transfers to r.images.
func (r *Replayer) createClearedImage(g graphicsdriver.Graphics, w, h, iw, ih int, f graphicsdriver.PixelFormat, pixels, clearBytes int64) (graphicsdriver.Image, error) {
	img, err := g.NewImage(w, h, f)
	if err != nil {
		return nil, err
	}
	owned := false
	defer func() {
		if !owned {
			r.curEpoch.images++
			r.curEpoch.pixels += pixels
			img.Dispose()
		}
	}()
	r.curEpoch.uploadBytes += clearBytes
	if err := clearImage(img, iw, ih, f.BytesPerPixel()); err != nil {
		return nil, err
	}
	owned = true
	return img, nil
}

func (r *Replayer) op(g graphicsdriver.Graphics, d *dec, op uint8, target *Target, res *Result) error {
	switch op {
	case opSetVertices:
		vertices := d.f32s()
		indices := d.u32s()
		if d.err != nil {
			return d.err
		}
		if len(vertices)%graphics.VertexFloatCount != 0 {
			return fmt.Errorf("remote: %d floats are not whole vertices", len(vertices))
		}
		n := uint32(len(vertices) / graphics.VertexFloatCount)
		for _, i := range indices {
			if i >= n {
				return fmt.Errorf("remote: index %d past %d vertices", i, n)
			}
		}
		r.vertices, r.indices = int(n), len(indices)
		if len(indices) == 0 {
			return nil
		}
		return g.SetVertices(vertices, indices)

	case opNewImage:
		id, w, h, f := d.u32(), d.u32(), d.u32(), graphicsdriver.PixelFormat(d.u8())
		if d.err != nil {
			return d.err
		}
		if err := r.newID(id); err != nil {
			return err
		}
		if max := uint32(g.MaxImageSize()); w == 0 || h == 0 || w > max || h > max {
			return fmt.Errorf("remote: an image of %dx%d", w, h)
		}
		if (f != graphicsdriver.PixelFormatRGBA8 && f != graphicsdriver.PixelFormatA8) || !g.SupportsPixelFormat(f) {
			return fmt.Errorf("remote: pixel format %d", f)
		}
		if len(r.images)+r.pendingImages() >= r.limits.MaxImages {
			return fmt.Errorf("remote: live image limit (%d) exceeded", r.limits.MaxImages)
		}
		pixels, ok := backingPixels64(int(w), int(h))
		if !ok || pixels > r.limits.MaxPixels-r.livePixels-r.pendingPixels()-r.screenPixels {
			return fmt.Errorf("remote: live pixel limit (%d) exceeded", r.limits.MaxPixels)
		}
		clearBytes, ok := checkedMul64(pixels, int64(f.BytesPerPixel()))
		if !ok || clearBytes > r.limits.MaxUploadBytes-r.epochUploadBytes() {
			return fmt.Errorf("remote: upload byte limit (%d) exceeded", r.limits.MaxUploadBytes)
		}
		iw, ih := graphics.InternalImageSize(int(w)), graphics.InternalImageSize(int(h))
		// Drivers do not guarantee zero-initialized GPU memory for NewImage (e.g., OpenGL passes
		// nil to TexImage2D, Metal and D3D11 leave contents undefined). Zero the entire backing
		// texture so an untrusted app cannot read stale viewer or other-app GPU memory.
		img, err := r.createClearedImage(g, int(w), int(h), iw, ih, f, pixels, clearBytes)
		if err != nil {
			return err
		}
		r.livePixels += pixels
		r.images[id] = &replayedImage{img: img, width: int(w), height: int(h), pixels: pixels, format: f}
		return nil

	case opNewScreenImage:
		id, w, h := d.u32(), d.u32(), d.u32()
		if d.err != nil {
			return d.err
		}
		if err := r.newID(id); err != nil {
			return err
		}
		if max := uint32(g.MaxImageSize()); w == 0 || h == 0 || w > max || h > max {
			return fmt.Errorf("remote: a screen of %dx%d", w, h)
		}
		if len(r.images)+r.pendingImages() >= r.limits.MaxImages {
			return fmt.Errorf("remote: live image limit (%d) exceeded", r.limits.MaxImages)
		}
		if _, err := r.checkScreenSize(int(w), int(h), g.MaxImageSize()); err != nil {
			return err
		}
		r.images[id] = &replayedImage{width: int(w), height: int(h), screen: true}
		r.activeScreenID = id
		return nil

	case opDisposeImage:
		id := d.u32()
		if d.err != nil {
			return d.err
		}
		i, err := r.image(id)
		if err != nil {
			return err
		}
		img := i.img
		r.livePixels -= i.pixels
		delete(r.images, id)
		if id == r.activeScreenID {
			r.activeScreenID = 0
			if target == nil {
				r.screenPixels = 0
			}
		}
		if img != nil {
			r.curEpoch.images++
			r.curEpoch.pixels += i.pixels
			img.Dispose()
		}
		return nil

	case opNewShader:
		id, textures := d.u32(), d.u32()
		src := d.bytes()
		if d.err != nil {
			return d.err
		}
		if err := r.newID(id); err != nil {
			return err
		}
		if textures > graphics.ShaderSrcImageCount {
			return fmt.Errorf("remote: a shader of %d textures", textures)
		}
		if len(r.shaders)+r.pendingShaders() >= r.limits.MaxShaders {
			return fmt.Errorf("remote: live shader limit (%d) exceeded", r.limits.MaxShaders)
		}
		if int64(len(src)) > int64(r.limits.MaxShaderSourceBytes)-r.liveShaderBytes-r.pendingShaderBytes() {
			return fmt.Errorf("remote: shader source byte limit (%d) exceeded", r.limits.MaxShaderSourceBytes)
		}
		p, err := compile(src, int(textures))
		if err != nil {
			return err
		}
		usedTextures, err := validateShaderGlobals(p)
		if err != nil {
			return err
		}
		s, err := g.NewShader(p.Descriptor())
		if err != nil {
			return err
		}
		r.nextShaderSeq++
		r.liveShaderBytes += int64(len(src))
		r.shaders[id] = &replayedShader{
			shader:       s,
			program:      p,
			seq:          r.nextShaderSeq,
			sourceBytes:  len(src),
			usedTextures: usedTextures,
		}
		return nil

	case opDisposeShader:
		id := d.u32()
		if d.err != nil {
			return d.err
		}
		s := r.shaders[id]
		if s == nil {
			return fmt.Errorf("remote: no shader %d", id)
		}
		sh := s.shader
		r.liveShaderBytes -= int64(s.sourceBytes)
		r.curEpoch.shaders++
		r.curEpoch.shaderBytes += int64(s.sourceBytes)
		nPipes := len(s.pipelines)
		r.livePipelines -= nPipes
		r.curEpoch.pipelines += nPipes
		delete(r.shaders, id)
		sh.Dispose()
		return nil

	case opWritePixels:
		id := d.u32()
		n := d.count(20)
		if d.err != nil {
			return d.err
		}
		i, err := r.image(id)
		if err != nil {
			return err
		}
		if i.screen {
			return errors.New("remote: pixels written to the screen")
		}
		args := r.pixelsArgs[:0]
		if cap(args) < n {
			args = make([]graphicsdriver.PixelsArgs, 0, n)
		}
		var uploadBytes int64
		for range n {
			region := d.rect()
			pix := d.bytes()
			if d.err != nil {
				clear(args)
				if cap(args) <= 256 {
					r.pixelsArgs = args[:0]
				} else {
					r.pixelsArgs = nil
				}
				return d.err
			}
			if err := checkPixels(i, region, len(pix)); err != nil {
				clear(args)
				if cap(args) <= 256 {
					r.pixelsArgs = args[:0]
				} else {
					r.pixelsArgs = nil
				}
				return err
			}
			if !region.Empty() {
				uploadBytes += int64(len(pix))
				args = append(args, graphicsdriver.PixelsArgs{Pixels: pix, Region: region})
			}
		}
		var writeErr error
		if len(args) > 0 {
			if uploadBytes > r.limits.MaxUploadBytes-r.epochUploadBytes() {
				writeErr = fmt.Errorf("remote: upload byte limit (%d) exceeded", r.limits.MaxUploadBytes)
			} else {
				r.curEpoch.uploadBytes += uploadBytes
				writeErr = i.img.WritePixels(args)
			}
		}
		clear(args)
		if cap(args) <= 256 {
			r.pixelsArgs = args[:0]
		} else {
			r.pixelsArgs = nil
		}
		return writeErr

	case opDrawTriangles:
		return r.draw(g, d, target, res)

	case opPresent:
		id := d.u32()
		n := d.u32()
		if d.err != nil {
			return d.err
		}
		i, err := r.image(id)
		if err != nil {
			return err
		}
		if !i.screen {
			return fmt.Errorf("remote: image %d presented, which is not a screen", id)
		}
		if _, err := r.driverImage(id, i, target); err != nil {
			return err
		}
		res.Presented, res.Damage = true, nil
		if n == allDamaged {
			return nil
		}
		if uint64(n)*16 > uint64(len(d.b)-d.p) {
			return errShort
		}
		bounds := image.Rect(0, 0, i.width, i.height)
		res.Damage = make([]image.Rectangle, 0, n)
		for range n {
			if rect := d.rect().Intersect(bounds); !rect.Empty() {
				res.Damage = append(res.Damage, rect)
			}
		}
		return d.err
	}
	return fmt.Errorf("remote: unknown operation %d at %d", op, d.p-1)
}

func checkPixels(i *replayedImage, region image.Rectangle, n int) error {
	if region.Min.X > region.Max.X || region.Min.Y > region.Max.Y {
		return fmt.Errorf("remote: inverted pixel region %v", region)
	}
	bounds := i.bounds()
	if region.Empty() {
		if n != 0 {
			return fmt.Errorf("remote: %d bytes of pixels for empty region %v, not 0", n, region)
		}
		return nil
	}
	if !region.In(bounds) {
		return fmt.Errorf("remote: pixels at %v, out of an image of %v", region, bounds)
	}
	want := uint64(i.format.BytesPerPixel()) * uint64(region.Dx()) * uint64(region.Dy())
	if n < 0 || uint64(n) != want {
		return fmt.Errorf("remote: %d bytes of pixels for %v, not %d", n, region, want)
	}
	return nil
}

func (r *Replayer) draw(g graphicsdriver.Graphics, d *dec, target *Target, res *Result) error {
	dstID := d.u32()
	var srcIDs [graphics.ShaderSrcImageCount]uint32
	for k := range srcIDs {
		srcIDs[k] = d.u32()
	}
	shaderID := d.u32()
	n := d.count(20)
	if d.err != nil {
		return d.err
	}
	dst, err := r.image(dstID)
	if err != nil {
		return err
	}
	dstImg, err := r.driverImage(dstID, dst, target)
	if err != nil {
		return err
	}
	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
	for k, id := range srcIDs {
		if id == 0 {
			continue
		}
		if id == dstID {
			return errors.New("remote: an image drawn to itself")
		}
		src, err := r.image(id)
		if err != nil {
			return err
		}
		if src.screen {
			return errors.New("remote: the screen used as a source")
		}
		srcs[k] = src.img
	}
	sh := r.shaders[shaderID]
	if sh == nil {
		return fmt.Errorf("remote: no shader %d", shaderID)
	}
	// A shader may reference a source slot conditionally (for example, vector's cover shader
	// branches across src0..src3) while the caller binds only the slots its vertices index.
	// Bind a 1x1 transparent placeholder for any nil slot the shader references so an untrusted
	// stream can never make the driver sample a nil texture.
	for k := range graphics.ShaderSrcImageCount {
		if sh.usedTextures&(1<<k) != 0 && srcs[k] == nil {
			empty, err := r.ensureEmptyImage(g)
			if err != nil {
				return err
			}
			srcs[k] = empty
		}
	}

	bounds := dst.bounds()
	regions := r.regions
	if cap(regions) < n {
		regions = make([]graphicsdriver.DstRegion, n)
	} else {
		regions = regions[:n]
	}
	defer func() {
		if cap(regions) <= 256 {
			r.regions = regions[:0]
		} else {
			r.regions = nil
		}
	}()
	var indices uint64
	for k := range regions {
		reg := d.rect()
		c := d.u32()
		if d.err != nil {
			return d.err
		}
		if reg.Min.X < bounds.Min.X || reg.Min.Y < bounds.Min.Y || reg.Max.X > bounds.Max.X || reg.Max.Y > bounds.Max.Y || reg.Min.X > reg.Max.X || reg.Min.Y > reg.Max.Y {
			return fmt.Errorf("remote: a draw to %v, out of an image of %v", reg, bounds)
		}
		indices += uint64(c)
		if indices > uint64(r.indices) {
			return fmt.Errorf("remote: a draw of more indices than the %d set", r.indices)
		}
		regions[k] = graphicsdriver.DstRegion{
			Region:     reg,
			IndexCount: int(c),
		}
	}
	offset := uint64(d.u32())
	var b [6]uint8
	for k := range b {
		b[k] = d.u8()
	}
	internal, user := d.u64(), d.u64()
	uniforms := d.u32s()
	if d.err != nil {
		return d.err
	}
	if offset > uint64(r.indices)-indices {
		return fmt.Errorf("remote: a draw from index %d past the %d set", offset, r.indices)
	}
	for k := range 4 {
		if graphicsdriver.BlendFactor(b[k]) > graphicsdriver.BlendFactorSourceAlphaSaturated {
			return fmt.Errorf("remote: blend factor %d", b[k])
		}
	}
	for k := 4; k < 6; k++ {
		if graphicsdriver.BlendOperation(b[k]) > graphicsdriver.BlendOperationMax {
			return fmt.Errorf("remote: blend operation %d", b[k])
		}
	}
	blend := graphicsdriver.Blend{
		BlendFactorSourceRGB:        graphicsdriver.BlendFactor(b[0]),
		BlendFactorSourceAlpha:      graphicsdriver.BlendFactor(b[1]),
		BlendFactorDestinationRGB:   graphicsdriver.BlendFactor(b[2]),
		BlendFactorDestinationAlpha: graphicsdriver.BlendFactor(b[3]),
		BlendOperationRGB:           graphicsdriver.BlendOperation(b[4]),
		BlendOperationAlpha:         graphicsdriver.BlendOperation(b[5]),
	}
	if internal == 0 || user == 0 {
		return fmt.Errorf("remote: uniform versions %d, %d", internal, user)
	}
	if want := graphics.PreservedUniformDwordCount + sh.program.UniformDwordCount; len(uniforms) != want {
		return fmt.Errorf("remote: %d dwords of uniforms for a shader of %d", len(uniforms), want)
	}
	if indices == 0 {
		// Short-circuit a validated all-zero draw so drivers never receive a draw call without
		// vertex/index buffers set.
		return nil
	}
	dstFormat := dst.format
	if dst.screen {
		dstFormat = graphicsdriver.PixelFormatRGBA8
	}
	pk := pipelineKey{blend: blend, format: dstFormat}
	if _, ok := sh.pipelines[pk]; !ok {
		if r.livePipelines+r.pendingPipelines() >= r.limits.MaxPipelines {
			return fmt.Errorf("remote: pipeline limit (%d) exceeded", r.limits.MaxPipelines)
		}
		if sh.pipelines == nil {
			sh.pipelines = map[pipelineKey]struct{}{}
		}
		sh.pipelines[pk] = struct{}{}
		r.livePipelines++
	}
	// The uniforms are used in place unless they are rewritten for the screen.
	if dst.screen {
		uniforms = r.retarget(uniforms, target)
	}
	versions := graphicsdriver.UniformVersions{
		Internal: r.internalVersion(internal, sh.seq, dst.screen),
		User:     r.userVersion(user, sh.seq),
	}
	res.Draws++
	return g.DrawTriangles(dstImg, srcs, sh.shader, regions, int(offset), blend, uniforms, versions)
}

// retarget returns a copy of uniforms written for a screen of the target's size rewritten for the
// target, which draws at its internal size: the destination's texture size and the scale of the
// projection, whose terms graphicscommand writes as 2/width and ±2/height (see
// prependPreservedUniforms there). The rest, in pixels of the destination, holds as it is.
func (r *Replayer) retarget(uniforms []uint32, target *Target) []uint32 {
	if cap(r.retargetBuf) < len(uniforms) {
		r.retargetBuf = make([]uint32, len(uniforms))
	}
	u := r.retargetBuf[:len(uniforms)]
	copy(u, uniforms)
	if cap(r.retargetBuf) > 1024 && len(uniforms) <= 256 {
		// Drop oversized scratch on subsequent smaller draws.
		r.retargetBuf = nil
	}
	w, h := float32(target.InternalWidth), float32(target.InternalHeight)
	u[graphics.DstTextureSizeUniformDwordIndex] = math.Float32bits(w)
	u[graphics.DstTextureSizeUniformDwordIndex+1] = math.Float32bits(h)
	p := u[graphics.ProjectionMatrixUniformDwordIndex:]
	p[0] = math.Float32bits(2 / w)
	p[5] = math.Float32bits(2/h) | p[5]&(1<<31)
	return u
}

// ReadPixels reads regions of the app's image id into dsts. It is called after the batch that
// came with the request was replayed.
func (r *Replayer) ReadPixels(id uint32, regions []image.Rectangle, dsts [][]byte, target *Target) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, err := r.image(id)
	if err != nil {
		return err
	}
	img, err := r.driverImage(id, i, target)
	if err != nil {
		return err
	}
	format := i.format
	if i.screen {
		format = graphicsdriver.PixelFormatRGBA8
	}
	if len(regions) != len(dsts) {
		return errors.New("remote: a read of pixels with as many regions as buffers")
	}
	args := make([]graphicsdriver.PixelsArgs, 0, len(regions))
	for k, region := range regions {
		if err := checkPixels(&replayedImage{width: i.width, height: i.height, format: format, screen: i.screen}, region, len(dsts[k])); err != nil {
			return err
		}
		if !region.Empty() {
			args = append(args, graphicsdriver.PixelsArgs{Pixels: dsts[k], Region: region})
		}
	}
	if len(args) == 0 {
		return nil
	}
	return img.ReadPixels(args)
}

// zeroPixels is a read-only zero buffer shared by clearImage to zero-initialize backing textures
// in strips without per-image heap allocations.
var zeroPixels [256 << 10]byte

// clearImage zeroes the entire iw×ih backing texture of img (at bpp bytes per pixel).
func clearImage(img graphicsdriver.Image, iw, ih, bpp int) error {
	rowBytes := bpp * iw
	if rowBytes <= 0 || ih <= 0 {
		return nil
	}
	stripH := len(zeroPixels) / rowBytes
	if stripH < 1 {
		row := make([]byte, rowBytes)
		for y := range ih {
			if err := img.WritePixels([]graphicsdriver.PixelsArgs{{
				Pixels: row,
				Region: image.Rect(0, y, iw, y+1),
			}}); err != nil {
				return err
			}
		}
		return nil
	}
	args := make([]graphicsdriver.PixelsArgs, 0, (ih+stripH-1)/stripH)
	for y := 0; y < ih; y += stripH {
		h := min(stripH, ih-y)
		args = append(args, graphicsdriver.PixelsArgs{
			Pixels: zeroPixels[:rowBytes*h],
			Region: image.Rect(0, y, iw, y+h),
		})
	}
	return img.WritePixels(args)
}

// ensureEmptyImage returns a zero-initialized InternalImageSize(1)×InternalImageSize(1) RGBA8
// image owned by r, creating it on first use. Every driver backs NewImage with
// InternalImageSize×InternalImageSize texels, so sizing explicitly to InternalImageSize(1) and
// clearing the full square ensures no texel a shader can address is uninitialized.
func (r *Replayer) ensureEmptyImage(g graphicsdriver.Graphics) (graphicsdriver.Image, error) {
	if r.emptyImg != nil {
		return r.emptyImg, nil
	}
	side := graphics.InternalImageSize(1)
	img, err := g.NewImage(side, side, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		return nil, err
	}
	owned := false
	defer func() {
		if !owned {
			img.Dispose()
		}
	}()
	if err := clearImage(img, side, side, graphicsdriver.PixelFormatRGBA8.BytesPerPixel()); err != nil {
		return nil, err
	}
	owned = true
	r.emptyImg = img
	return img, nil
}

// Dispose frees every image and shader the app made. The screen is the viewer's and stays.
func (r *Replayer) Dispose() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, i := range r.images {
		if i.img != nil {
			i.img.Dispose()
		}
	}
	if r.emptyImg != nil {
		r.emptyImg.Dispose()
		r.emptyImg = nil
	}
	for _, s := range r.shaders {
		s.shader.Dispose()
	}
	clear(r.images)
	clear(r.shaders)
	r.activeScreenID = 0
	r.livePixels = 0
	r.screenPixels = 0
	r.liveShaderBytes = 0
	r.livePipelines = 0
	r.curEpoch = retiredCharges{}
	r.prevEpoch = retiredCharges{}
	r.lastInternalDriver, r.lastUserDriver = 0, 0
	r.vertices, r.indices = 0, 0
}

// programs caches the programs compiled from the sources apps sent, so that an app's restart
// skips the parse and the validation of its shaders. The compiled programs are immutable.
var (
	programsM sync.Mutex
	programs  = map[[32]byte]*shader.Program{}
)

// maxPrograms bounds the cache, which a viewer running app after app would otherwise grow.
const maxPrograms = 512

func compile(src []byte, textures int) (p *shader.Program, err error) {
	h := sha256.New()
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(textures))
	h.Write(n[:])
	h.Write(src)
	var k [32]byte
	h.Sum(k[:0])

	programsM.Lock()
	if p, ok := programs[k]; ok {
		programsM.Unlock()
		return p, nil
	}
	programsM.Unlock()

	defer func() {
		if e := recover(); e != nil {
			p, err = nil, fmt.Errorf("remote: compiling a shader: %v", e)
		}
	}()
	p, err = shader.CompileUntrusted(src, textures)
	if err != nil {
		return nil, err
	}

	programsM.Lock()
	if cached, ok := programs[k]; ok {
		programsM.Unlock()
		return cached, nil
	}
	if len(programs) >= maxPrograms {
		clear(programs)
	}
	programs[k] = p
	programsM.Unlock()
	return p, nil
}
