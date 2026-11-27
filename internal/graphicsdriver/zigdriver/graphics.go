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

//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// Options configures a native Zig graphics driver device. NewGraphics also
// reads the environment variables listed in the package documentation: their
// flags are added to Flags, and they supply Slots and DXFeatureLevel when
// those are left zero.
type Options struct {
	ColorSpace     color.ColorSpace
	Backend        uint32 // BackendAuto, BackendMetal, BackendDX12, BackendVulkan, BackendNull
	Flags          uint32 // Bitmask of DeviceFlag*
	Slots          int    // Command ring slots: 2 (default) or 3
	DXFeatureLevel uint32 // 0 uses default (0xb000 = D3D_FEATURE_LEVEL_11_0)
	LibPath        string // Optional explicit shared library path override
}

var deviceCreated atomic.Bool

// DeviceCreated reports whether this process has successfully created a Zig graphics device.
func DeviceCreated() bool {
	return deviceCreated.Load()
}

// Graphics implements graphicsdriver.Graphics and its optional capabilities
// by batching commands into a linear arena ring submitted to the Zig native library.
type Graphics struct {
	lib        *library
	devMu      sync.RWMutex
	dev        uintptr
	caps       DriverCaps
	colorSpace color.ColorSpace

	ring   ring
	shared sharedUploadArena
	closed atomic.Bool

	occluded atomic.Bool

	mainThreadMu    sync.Mutex
	runOnMainThread func(func())

	// Render-thread handle allocators and pending lifecycle disposals.
	imageIDs               idAllocator
	shaderIDs              idAllocator
	pendingDisposeImages   []uint32
	pendingDisposeShaders  []uint32
	pendingDisposeSurfaces []uint32

	// Main-thread surface handle allocator (never reused).
	nextSurfaceID atomic.Uint32

	// Active surfaces tracked for damage flushing at Begin().
	surfacesMu sync.Mutex
	surfaces   []*Surface

	// Reusable FFI descriptors kept on heap-resident Graphics.
	batch       Batch
	submitRes   SubmitResult
	uploadArena UploadArena
	readRegions []ReadRegion
	errBuf      [1024]byte

	mainErrMu  sync.Mutex
	mainErrBuf [1024]byte

	// presentOnMainThread bound once, so handing it to the main-thread runner
	// does not allocate a closure per frame. presentErr is its result.
	runPresent func()
	presentErr error
}

var (
	_ graphicsdriver.Graphics                  = (*Graphics)(nil)
	_ graphicsdriver.MainThreadRunner          = (*Graphics)(nil)
	_ graphicsdriver.DirectCompositionReporter = (*Graphics)(nil)
	_ graphicsdriver.OcclusionReporter         = (*Graphics)(nil)
	_ graphicsdriver.ForcedFrameFinisher       = (*Graphics)(nil)
	_ graphicsdriver.Closer                    = (*Graphics)(nil)
)

func defaultMainThreadMode() uint32 {
	if os.Getenv("GGFX_ZIG_METAL_DISPATCH") == "1" {
		return MainThreadDispatch
	}
	return MainThreadInline
}

// withEnv returns o with the driver's environment variables applied. This is
// the only place they are read, so every caller of NewGraphics sees the same
// settings whether or not it goes through internal/render.
func (o Options) withEnv() Options {
	if os.Getenv("GGFX_DRIVER_DEBUG") == "1" {
		o.Flags |= DeviceFlagDebug
	}
	if os.Getenv("GGFX_METAL_SWAPCHAIN") == "0" {
		o.Flags |= DeviceFlagMetalNoIOSurface
	}
	if o.Slots == 0 {
		if n, err := strconv.Atoi(os.Getenv("GGFX_DRIVER_SLOTS")); err == nil && (n == 2 || n == 3) {
			o.Slots = n
		}
	}
	if runtime.GOOS == "windows" && (o.Backend == BackendAuto || o.Backend == BackendDX12) {
		o.applyDirectXEnv()
	}
	return o
}

// applyDirectXEnv applies GGFX_DIRECTX and GGFX_DIRECTX_FEATURE_LEVEL, with
// the same tokens and fallback variable names as the Go DirectX driver. A
// "featurelevel=" token takes precedence over the standalone variable.
func (o *Options) applyDirectXEnv() {
	level := cmp.Or(os.Getenv("GGFX_DIRECTX_FEATURE_LEVEL"), os.Getenv("EBITENGINE_DIRECTX_FEATURE_LEVEL"))
	env := cmp.Or(os.Getenv("GGFX_DIRECTX"), os.Getenv("EBITENGINE_DIRECTX"), os.Getenv("EBITEN_DIRECTX"))
	if env != "" {
		for tok := range strings.SplitSeq(env, ",") {
			tok = strings.TrimSpace(tok)
			switch {
			case tok == "warp":
				o.Flags |= DeviceFlagWARP
			case tok == "debug":
				o.Flags |= DeviceFlagDebug
			case tok == "dred":
				o.Flags |= DeviceFlagDRED
			case strings.HasPrefix(tok, "featurelevel="):
				if _, ok := parseDXFeatureLevel(tok[len("featurelevel="):]); ok {
					level = tok[len("featurelevel="):]
				}
			}
		}
	}
	if o.DXFeatureLevel == 0 {
		if fl, ok := parseDXFeatureLevel(level); ok {
			o.DXFeatureLevel = fl
		}
	}
}

// parseDXFeatureLevel maps a feature level name to its D3D_FEATURE_LEVEL value.
func parseDXFeatureLevel(s string) (uint32, bool) {
	switch s {
	case "11_0":
		return 0xb000, true
	case "11_1":
		return 0xb100, true
	case "12_0":
		return 0xc000, true
	case "12_1":
		return 0xc100, true
	case "12_2":
		return 0xc200, true
	default:
		return 0, false
	}
}

// NewGraphics loads the native Zig driver library and creates a GPU device.
func NewGraphics(o Options) (*Graphics, error) {
	o = o.withEnv()
	lib, err := loadLibrary(o.LibPath)
	if err != nil {
		return nil, err
	}

	slots := o.Slots
	if slots != 3 {
		slots = 2
	}

	g := &Graphics{
		lib:        lib,
		colorSpace: o.ColorSpace,
	}
	g.runPresent = g.presentOnMainThread

	devOpts := DeviceOptions{
		StructSize:     uint32(unsafe.Sizeof(DeviceOptions{})),
		ABIVersion:     DriverABIVersion,
		Backend:        o.Backend,
		ColorSpace:     uint32(o.ColorSpace),
		Flags:          o.Flags,
		FramesInFlight: uint32(slots),
		DXFeatureLevel: o.DXFeatureLevel,
		MainThreadMode: defaultMainThreadMode(),
	}

	var dev uintptr
	g.errBuf[0] = 0
	st := lib.deviceCreate(&devOpts, &dev, &g.caps, &g.errBuf[0], uintptr(len(g.errBuf)))
	runtime.KeepAlive(&devOpts)
	if st != StatusOK {
		return nil, statusToError("device_create", st, g.errBuf[:])
	}
	if dev == 0 {
		return nil, fmt.Errorf("%w: device_create returned null device", ErrInternal)
	}

	g.dev = dev
	if g.colorSpace == 0 && g.caps.ColorSpace != 0 {
		g.colorSpace = color.ColorSpace(g.caps.ColorSpace)
	}
	g.ring.init(slots)
	g.ring.setShared(&g.shared)
	if g.caps.Flags&CapFlagSharedUploadArena != 0 {
		_ = g.acquireSharedArena(0)
	}
	deviceCreated.Store(true)
	return g, nil
}

func (g *Graphics) acquireSharedArena(minBytes uint64) bool {
	if g.dev == 0 || g.caps.Flags&CapFlagSharedUploadArena == 0 {
		g.shared.clear()
		return false
	}
	g.errBuf[0] = 0
	g.uploadArena = UploadArena{}
	st := g.lib.acquireUploadArena(g.dev, minBytes, &g.uploadArena, &g.errBuf[0], uintptr(len(g.errBuf)))
	if st != StatusOK || !g.shared.set(g.uploadArena) {
		g.shared.clear()
		return false
	}
	return true
}

// SharedUploadArenaActive reports whether the device is currently writing batch
// payloads directly into a native GPU-mapped shared upload arena.
func (g *Graphics) SharedUploadArenaActive() bool {
	return g.shared.enabled && len(g.shared.buf) > 0
}

// Initialize implements graphicsdriver.Graphics. Device initialization happens in NewGraphics.
func (g *Graphics) Initialize() error {
	if g.closed.Load() {
		return ErrClosed
	}
	return nil
}

// ColorSpace implements graphicsdriver.Graphics.
func (g *Graphics) ColorSpace() color.ColorSpace {
	return g.colorSpace
}

// Caps returns the native driver capabilities reported at device creation.
func (g *Graphics) Caps() DriverCaps {
	return g.caps
}

// DeviceName returns the NUL-terminated adapter/device name reported by the native driver.
func (g *Graphics) DeviceName() string {
	return errBufToString(g.caps.DeviceName[:])
}

// SupportsPixelFormat implements graphicsdriver.Graphics.
func (g *Graphics) SupportsPixelFormat(format graphicsdriver.PixelFormat) bool {
	switch format {
	case graphicsdriver.PixelFormatRGBA8:
		return true
	case graphicsdriver.PixelFormatA8:
		return g.caps.Flags&CapFlagSupportsA8 != 0
	default:
		return false
	}
}

// NeedsClearingScreen implements graphicsdriver.Graphics.
func (g *Graphics) NeedsClearingScreen() bool {
	return g.caps.Flags&CapFlagNeedsClearingScreen != 0
}

// MaxImageSize implements graphicsdriver.Graphics.
func (g *Graphics) MaxImageSize() int {
	return int(g.caps.MaxImageSize)
}

// FlipsY implements graphicsdriver.Graphics.
func (g *Graphics) FlipsY() bool {
	return g.caps.Flags&CapFlagFlipsY != 0
}

// SupportsDirectComposition implements graphicsdriver.DirectCompositionReporter.
func (g *Graphics) SupportsDirectComposition() bool {
	return g.caps.Flags&CapFlagDirectComposition != 0
}

// IsOccluded implements graphicsdriver.OcclusionReporter.
func (g *Graphics) IsOccluded() bool {
	return g.occluded.Load()
}

// SetMainThreadRunner implements graphicsdriver.MainThreadRunner.
func (g *Graphics) SetMainThreadRunner(f func(func())) {
	g.mainThreadMu.Lock()
	g.runOnMainThread = f
	g.mainThreadMu.Unlock()

	g.devMu.RLock()
	defer g.devMu.RUnlock()
	if g.closed.Load() || g.dev == 0 {
		return
	}
	mode := defaultMainThreadMode()
	if f != nil {
		mode = MainThreadDeferred
	}
	g.lib.setMainThreadMode(g.dev, mode)
}

// FinishForcedFrame implements graphicsdriver.ForcedFrameFinisher.
func (g *Graphics) FinishForcedFrame() error {
	if g.closed.Load() {
		return ErrClosed
	}
	g.errBuf[0] = 0
	st := g.lib.finishForcedFrame(g.dev, &g.errBuf[0], uintptr(len(g.errBuf)))
	return statusToError("finish_forced_frame", st, g.errBuf[:])
}

// DebugStats queries live resource and batch execution statistics from the native driver.
func (g *Graphics) DebugStats() (DebugStats, error) {
	if g.closed.Load() {
		return DebugStats{}, ErrClosed
	}
	var out DebugStats
	st := g.lib.debugStats(g.dev, &out)
	if st != StatusOK {
		return DebugStats{}, statusToError("debug_stats", st, nil)
	}
	return out, nil
}

// Begin implements graphicsdriver.Graphics.
func (g *Graphics) Begin() error {
	if g.closed.Load() {
		return ErrClosed
	}
	// Emit OP_SET_DAMAGE for dirty surfaces at the head of the batch.
	g.surfacesMu.Lock()
	for _, s := range g.surfaces {
		if !s.damageDirty {
			continue
		}
		s.damageDirty = false
		// encodeSetDamage reads nil as "whole surface" and an empty slice as "nothing".
		rects := s.damageRects
		if s.damageFull {
			rects = nil
		} else if rects == nil {
			rects = []image.Rectangle{}
		}
		g.ring.cur().encodeSetDamage(s.id, rects)
	}
	g.surfacesMu.Unlock()
	return nil
}

// End implements graphicsdriver.Graphics.
func (g *Graphics) End(mode graphicsdriver.FlushMode) error {
	if g.closed.Load() {
		return ErrClosed
	}
	cur := g.ring.cur()
	if mode == graphicsdriver.FlushModeIntermediate && cur.cmdCount == 0 {
		return nil
	}
	return g.submitCurrent(uint32(mode))
}

// batchUse is how much of a slot one batch consumed.
type batchUse struct {
	cmd, pay int  // bytes of commands and of heap payload
	shared   bool // payload went to the shared upload arena instead
}

// stageBatch stamps cur with the next sequence number and describes it in
// g.batch, ready to hand to the native driver.
func (g *Graphics) stageBatch(cur *slot) batchUse {
	g.ring.nextSeq++
	cur.seq = g.ring.nextSeq
	cur.fillBatch(&g.batch)

	use := batchUse{cmd: cur.cmdLen, shared: cur.usingShared}
	if !use.shared {
		use.pay = cur.payLen
	}
	return use
}

func (g *Graphics) queueDisposeImage(id uint32) {
	g.pendingDisposeImages = append(g.pendingDisposeImages, id)
	g.ring.cur().encodeDisposeImage(id)
	g.imageIDs.deferRelease(id)
}

func (g *Graphics) queueDisposeShader(id uint32) {
	g.pendingDisposeShaders = append(g.pendingDisposeShaders, id)
	g.ring.cur().encodeDisposeShader(id)
	g.shaderIDs.deferRelease(id)
}

func (g *Graphics) queueDisposeSurface(id uint32) {
	g.pendingDisposeSurfaces = append(g.pendingDisposeSurfaces, id)
	g.ring.cur().encodeDisposeSurface(id)
}

// requeuePendingDisposals encodes any disposals from a rejected batch at the front
// of the next empty slot so native resources are never stranded after recoverable errors.
func (g *Graphics) requeuePendingDisposals() {
	cur := g.ring.cur()
	for _, id := range g.pendingDisposeImages {
		cur.encodeDisposeImage(id)
	}
	for _, id := range g.pendingDisposeShaders {
		cur.encodeDisposeShader(id)
	}
	for _, id := range g.pendingDisposeSurfaces {
		cur.encodeDisposeSurface(id)
	}
}

// retireBatch empties cur once the native driver has consumed or rejected the
// staged batch (st is its status), settles the IDs disposed in that batch, and
// folds the batch's size into the frame's peak usage.
func (g *Graphics) retireBatch(cur *slot, use batchUse, st int32) {
	if use.shared {
		g.shared.advanceBatch()
	}
	cur.reset()
	if st == StatusOK {
		g.imageIDs.commit()
		g.shaderIDs.commit()
		g.pendingDisposeImages = g.pendingDisposeImages[:0]
		g.pendingDisposeShaders = g.pendingDisposeShaders[:0]
		g.pendingDisposeSurfaces = g.pendingDisposeSurfaces[:0]
	}
	cur.frameMaxCmd = max(cur.frameMaxCmd, use.cmd)
	cur.frameMaxPay = max(cur.frameMaxPay, use.pay)
}

func (g *Graphics) submitCurrent(flushMode uint32) error {
	cur := g.ring.cur()
	use := g.stageBatch(cur)

	g.errBuf[0] = 0
	st := g.lib.submit(g.dev, &g.batch, flushMode, &g.submitRes, &g.errBuf[0], uintptr(len(g.errBuf)))
	runtime.KeepAlive(cur)
	runtime.KeepAlive(&g.batch)
	runtime.KeepAlive(&g.submitRes)

	g.retireBatch(cur, use, st)
	if flushMode != FlushIntermediate {
		// End of frame: size the slot for the frame's peak, then move on.
		frameCmd, framePay := cur.frameMaxCmd, cur.frameMaxPay
		cur.frameMaxCmd, cur.frameMaxPay = 0, 0
		cur.shrink(frameCmd, framePay)
		g.ring.advance()
	}

	if st != StatusOK {
		if st != StatusErrDeviceLost && !g.closed.Load() {
			g.requeuePendingDisposals()
		}
		return statusToError("submit", st, g.errBuf[:])
	}

	g.occluded.Store(g.submitRes.Flags&ResultFlagOccluded != 0)

	if g.submitRes.Flags&ResultFlagNeedsMainThreadPresent != 0 {
		g.mainThreadMu.Lock()
		runner := g.runOnMainThread
		g.mainThreadMu.Unlock()

		g.presentErr = nil
		if runner != nil {
			runner(g.runPresent)
		} else {
			g.presentOnMainThread()
		}
		return g.presentErr
	}

	return nil
}

func (g *Graphics) presentOnMainThread() {
	g.mainErrMu.Lock()
	defer g.mainErrMu.Unlock()
	g.mainErrBuf[0] = 0
	st := g.lib.presentOnMainThread(g.dev, &g.mainErrBuf[0], uintptr(len(g.mainErrBuf)))
	g.presentErr = statusToError("present_on_main_thread", st, g.mainErrBuf[:])
}

// SetVertices implements graphicsdriver.Graphics.
func (g *Graphics) SetVertices(vertices []float32, indices []uint32) error {
	if g.closed.Load() {
		return ErrClosed
	}
	if len(vertices) == 0 || len(indices) == 0 {
		return nil
	}
	g.ring.cur().encodeSetVertices(vertices, indices)
	return nil
}

// DrawTriangles implements graphicsdriver.Graphics.
func (g *Graphics) DrawTriangles(
	dst graphicsdriver.Image,
	srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image,
	shader graphicsdriver.Shader,
	dstRegions []graphicsdriver.DstRegion,
	indexOffset int,
	blend graphicsdriver.Blend,
	uniforms []uint32,
	versions graphicsdriver.UniformVersions,
) error {
	if g.closed.Load() {
		return ErrClosed
	}

	dstImg, ok := dst.(*Image)
	if !ok || dstImg == nil || dstImg.g != g || dstImg.disposed {
		return fmt.Errorf("%w: invalid destination image", ErrInvalidArgument)
	}
	sh, ok := shader.(*Shader)
	if !ok || sh == nil || sh.g != g || sh.disposed {
		return fmt.Errorf("%w: invalid shader", ErrInvalidArgument)
	}

	wantUniforms := int(InternalUniformDwords) + int(sh.userUniformDwords)
	if len(uniforms) != wantUniforms {
		return fmt.Errorf("%w: uniforms length %d != expected %d", ErrInvalidArgument, len(uniforms), wantUniforms)
	}
	if versions.Internal == 0 {
		return fmt.Errorf("%w: internal uniform version must not be 0", ErrInvalidArgument)
	}
	if sh.userUniformDwords > 0 && versions.User == 0 {
		return fmt.Errorf("%w: user uniform version must not be 0", ErrInvalidArgument)
	}
	if indexOffset < 0 {
		return fmt.Errorf("%w: negative index offset %d", ErrInvalidArgument, indexOffset)
	}
	if len(dstRegions) == 0 {
		return nil
	}

	var srcIDs [MaxSrcImages]uint32
	for i, src := range srcs {
		if src == nil {
			continue
		}
		srcImg, ok := src.(*Image)
		if !ok || srcImg.g != g || srcImg.disposed {
			return fmt.Errorf("%w: invalid source image at slot %d", ErrInvalidArgument, i)
		}
		srcIDs[i] = srcImg.id
	}

	abiBlend := Blend{
		SrcRGB: uint8(blend.BlendFactorSourceRGB),
		SrcA:   uint8(blend.BlendFactorSourceAlpha),
		DstRGB: uint8(blend.BlendFactorDestinationRGB),
		DstA:   uint8(blend.BlendFactorDestinationAlpha),
		OpRGB:  uint8(blend.BlendOperationRGB),
		OpA:    uint8(blend.BlendOperationAlpha),
	}

	encode := func(s *slot) bool {
		return s.encodeDrawTriangles(
			dstImg.id, sh.id, srcIDs, uint32(indexOffset), abiBlend,
			dstRegions, uniforms, versions, sh.userUniformDwords,
		)
	}
	if encode(g.ring.cur()) {
		return nil
	}

	// Payload arena near limit: flush pending commands and retry in a fresh slot.
	if g.ring.cur().cmdCount > 0 {
		if err := g.submitCurrent(FlushIntermediate); err != nil {
			return err
		}
	}

	cur := g.ring.cur()
	if g.shared.enabled {
		need := g.shared.drawUniformsNeed(sh.userUniformDwords)
		if !g.shared.canFit(need) {
			if !g.acquireSharedArena(need) {
				cur.forceHeap = true
			}
		}
	}
	if !encode(cur) {
		if !cur.forceHeap && g.shared.enabled {
			cur.forceHeap = true
			if encode(cur) {
				return nil
			}
		}
		if cur.cmdCount == 0 && cur.payLen == 0 {
			cur.forceHeap = false
		}
		return fmt.Errorf("%w: uniform payload exceeds slot limit", ErrOutOfMemory)
	}
	return nil
}

// Close implements graphicsdriver.Closer. It flushes any pending disposal commands,
// destroys the native device, and reports any detected native memory leaks.
func (g *Graphics) Close() error {
	if !g.closed.CompareAndSwap(false, true) {
		return nil
	}

	var flushErr error
	if g.ring.cur().cmdCount > 0 {
		flushErr = g.submitCurrent(FlushIntermediate)
	}
	for i := range g.ring.n {
		g.ring.slots[i].pinner.Unpin()
	}

	g.devMu.Lock()
	defer g.devMu.Unlock()
	g.shared.clear()
	if g.dev == 0 {
		return flushErr
	}

	g.errBuf[0] = 0
	st := g.lib.deviceDestroy(g.dev, &g.errBuf[0], uintptr(len(g.errBuf)))
	g.dev = 0
	destroyErr := statusToError("device_destroy", st, g.errBuf[:])
	return errors.Join(flushErr, destroyErr)
}

// idAllocator hands out handle IDs starting at 1 and reuses released ones,
// most recently released first.
//
// An ID released with deferRelease stays unavailable until commit, which runs
// once the batch carrying its OP_DISPOSE_* has reached the native driver. A
// synchronous call such as NewShader therefore never reuses an ID whose
// disposal is still queued.
type idAllocator struct {
	next    uint32
	free    []uint32
	pending []uint32
}

func (a *idAllocator) alloc() uint32 {
	if n := len(a.free); n > 0 {
		id := a.free[n-1]
		a.free = a.free[:n-1]
		return id
	}
	a.next++
	return a.next
}

// release makes id available again at once. Use it only for an ID the native
// driver never learned about.
func (a *idAllocator) release(id uint32) {
	if id != 0 {
		a.free = append(a.free, id)
	}
}

func (a *idAllocator) deferRelease(id uint32) {
	if id != 0 {
		a.pending = append(a.pending, id)
	}
}

// commit makes every deferred ID available.
func (a *idAllocator) commit() {
	a.free = append(a.free, a.pending...)
	a.pending = a.pending[:0]
}
