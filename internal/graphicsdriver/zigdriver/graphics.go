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
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// Options configures a native Zig graphics driver device.
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

type damageEntry struct {
	surfaceID uint32
	rects     []image.Rectangle
}

// Graphics implements graphicsdriver.Graphics and its optional capabilities
// by batching commands into a linear arena ring submitted to the Zig native library.
type Graphics struct {
	lib        *library
	devMu      sync.RWMutex
	dev        uintptr
	caps       DriverCaps
	colorSpace color.ColorSpace

	ring    ring
	shared  sharedUploadArena
	inBatch bool
	closed  atomic.Bool

	occluded atomic.Bool

	mainThreadMu    sync.Mutex
	runOnMainThread func(func())

	// Render-thread handle allocators (LIFO free-list reuse).
	// Disposed IDs are held in pendingFree* until the batch carrying OP_DISPOSE_*
	// has been submitted to Zig so synchronous calls (e.g. NewShader) never collide
	// with a queued disposal in the same batch.
	nextImageID         uint32
	freeImageIDs        []uint32
	pendingFreeImageIDs []uint32

	nextShaderID         uint32
	freeShaderIDs        []uint32
	pendingFreeShaderIDs []uint32

	// Main-thread surface handle allocator (never reused).
	nextSurfaceID atomic.Uint32

	// Active surfaces tracked for damage flushing at Begin().
	surfacesMu    sync.Mutex
	surfaces      []*Surface
	pendingDamage []damageEntry

	// Reusable FFI descriptors kept on heap-resident Graphics.
	batch       Batch
	submitRes   SubmitResult
	uploadArena UploadArena
	readRegions []ReadRegion
	errBuf      [1024]byte

	mainErrMu  sync.Mutex
	mainErrBuf [1024]byte
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

func applyDirectXEnvFlags(backend, flags uint32) uint32 {
	if runtime.GOOS != "windows" || (backend != BackendAuto && backend != BackendDX12) {
		return flags
	}
	env := cmp.Or(os.Getenv("GGFX_DIRECTX"), os.Getenv("EBITENGINE_DIRECTX"), os.Getenv("EBITEN_DIRECTX"))
	if env == "" {
		return flags
	}
	for tok := range strings.SplitSeq(env, ",") {
		switch strings.TrimSpace(tok) {
		case "warp":
			flags |= DeviceFlagWARP
		case "debug":
			flags |= DeviceFlagDebug
		case "dred":
			flags |= DeviceFlagDRED
		}
	}
	return flags
}

// NewGraphics loads the native Zig driver library and creates a GPU device.
func NewGraphics(o Options) (*Graphics, error) {
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

	flags := applyDirectXEnvFlags(o.Backend, o.Flags)

	devOpts := DeviceOptions{
		StructSize:     uint32(unsafe.Sizeof(DeviceOptions{})),
		ABIVersion:     DriverABIVersion,
		Backend:        o.Backend,
		ColorSpace:     uint32(o.ColorSpace),
		Flags:          flags,
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
		return false
	}
	g.errBuf[0] = 0
	g.uploadArena = UploadArena{}
	st := g.lib.acquireUploadArena(g.dev, minBytes, &g.uploadArena, &g.errBuf[0], uintptr(len(g.errBuf)))
	if st != StatusOK {
		return false
	}
	return g.shared.set(g.uploadArena)
}

func (g *Graphics) rotateSharedArena(minBytes uint64) bool {
	return g.acquireSharedArena(minBytes)
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
	g.inBatch = true

	// Snapshot dirty surface damage hints and emit OP_SET_DAMAGE at the head of the batch.
	g.pendingDamage = g.pendingDamage[:0]
	g.surfacesMu.Lock()
	for _, s := range g.surfaces {
		if s.damageDirty {
			g.pendingDamage = append(g.pendingDamage, damageEntry{
				surfaceID: s.id,
				rects:     s.damageRects,
			})
			s.damageDirty = false
			s.damageRects = nil
		}
	}
	g.surfacesMu.Unlock()

	if len(g.pendingDamage) > 0 {
		cur := g.ring.cur()
		for _, d := range g.pendingDamage {
			cur.encodeSetDamage(d.surfaceID, d.rects)
		}
		clear(g.pendingDamage)
		g.pendingDamage = g.pendingDamage[:0]
	}
	return nil
}

// End implements graphicsdriver.Graphics.
func (g *Graphics) End(mode graphicsdriver.FlushMode) error {
	if g.closed.Load() {
		return ErrClosed
	}
	g.inBatch = false

	cur := g.ring.cur()
	if mode == graphicsdriver.FlushModeIntermediate && cur.cmdCount == 0 {
		return nil
	}
	return g.submitCurrent(uint32(mode))
}

func (g *Graphics) submitCurrent(flushMode uint32) error {
	cur := g.ring.cur()
	g.ring.nextSeq++
	cur.seq = g.ring.nextSeq
	cur.fillBatch(&g.batch)

	usedCmd := cur.cmdLen
	usedShared := cur.usingShared
	usedPay := 0
	if !usedShared {
		usedPay = cur.payLen
	}

	g.errBuf[0] = 0
	st := g.lib.submit(g.dev, &g.batch, flushMode, &g.submitRes, &g.errBuf[0], uintptr(len(g.errBuf)))
	runtime.KeepAlive(cur)
	runtime.KeepAlive(&g.batch)
	runtime.KeepAlive(&g.submitRes)

	if usedShared {
		g.shared.advanceBatch()
	}
	cur.reset()
	if st == StatusOK {
		g.releaseDeferredIDs()
	} else {
		g.discardDeferredIDs()
	}
	if flushMode == FlushIntermediate {
		cur.frameMaxCmd = max(cur.frameMaxCmd, usedCmd)
		cur.frameMaxPay = max(cur.frameMaxPay, usedPay)
	} else {
		frameCmd := max(cur.frameMaxCmd, usedCmd)
		framePay := max(cur.frameMaxPay, usedPay)
		cur.frameMaxCmd = 0
		cur.frameMaxPay = 0
		cur.shrink(frameCmd, framePay)
		g.ring.advance()
	}

	if st != StatusOK {
		return statusToError("submit", st, g.errBuf[:])
	}

	g.occluded.Store(g.submitRes.Flags&ResultFlagOccluded != 0)

	if g.submitRes.Flags&ResultFlagNeedsMainThreadPresent != 0 {
		var presentErr error
		runPresent := func() {
			g.mainErrMu.Lock()
			defer g.mainErrMu.Unlock()
			g.mainErrBuf[0] = 0
			pst := g.lib.presentOnMainThread(g.dev, &g.mainErrBuf[0], uintptr(len(g.mainErrBuf)))
			presentErr = statusToError("present_on_main_thread", pst, g.mainErrBuf[:])
		}

		g.mainThreadMu.Lock()
		runner := g.runOnMainThread
		g.mainThreadMu.Unlock()

		if runner != nil {
			runner(runPresent)
		} else {
			runPresent()
		}
		if presentErr != nil {
			return presentErr
		}
	}

	return nil
}

// SetVertices implements graphicsdriver.Graphics.
func (g *Graphics) SetVertices(vertices []float32, indices []uint32) error {
	if g.closed.Load() {
		return ErrClosed
	}
	if len(vertices) == 0 && len(indices) == 0 {
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

	if g.ring.cur().encodeDrawTriangles(
		dstImg.id, sh.id, srcIDs, uint32(indexOffset), abiBlend,
		dstRegions, uniforms, versions, sh.userUniformDwords,
	) {
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
			if !g.rotateSharedArena(need) {
				cur.forceHeap = true
			}
		}
	}
	if !cur.encodeDrawTriangles(
		dstImg.id, sh.id, srcIDs, uint32(indexOffset), abiBlend,
		dstRegions, uniforms, versions, sh.userUniformDwords,
	) {
		if !cur.forceHeap && g.shared.enabled {
			cur.forceHeap = true
			if cur.encodeDrawTriangles(
				dstImg.id, sh.id, srcIDs, uint32(indexOffset), abiBlend,
				dstRegions, uniforms, versions, sh.userUniformDwords,
			) {
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

func (g *Graphics) allocImageID() uint32 {
	if n := len(g.freeImageIDs); n > 0 {
		id := g.freeImageIDs[n-1]
		g.freeImageIDs = g.freeImageIDs[:n-1]
		return id
	}
	g.nextImageID++
	return g.nextImageID
}

func (g *Graphics) deferFreeImageID(id uint32) {
	if id != 0 {
		g.pendingFreeImageIDs = append(g.pendingFreeImageIDs, id)
	}
}

func (g *Graphics) allocShaderID() uint32 {
	if n := len(g.freeShaderIDs); n > 0 {
		id := g.freeShaderIDs[n-1]
		g.freeShaderIDs = g.freeShaderIDs[:n-1]
		return id
	}
	g.nextShaderID++
	return g.nextShaderID
}

func (g *Graphics) freeShaderID(id uint32) {
	if id != 0 {
		g.freeShaderIDs = append(g.freeShaderIDs, id)
	}
}

func (g *Graphics) deferFreeShaderID(id uint32) {
	if id != 0 {
		g.pendingFreeShaderIDs = append(g.pendingFreeShaderIDs, id)
	}
}

func (g *Graphics) releaseDeferredIDs() {
	if len(g.pendingFreeImageIDs) > 0 {
		g.freeImageIDs = append(g.freeImageIDs, g.pendingFreeImageIDs...)
		g.pendingFreeImageIDs = g.pendingFreeImageIDs[:0]
	}
	if len(g.pendingFreeShaderIDs) > 0 {
		g.freeShaderIDs = append(g.freeShaderIDs, g.pendingFreeShaderIDs...)
		g.pendingFreeShaderIDs = g.pendingFreeShaderIDs[:0]
	}
}

func (g *Graphics) discardDeferredIDs() {
	g.pendingFreeImageIDs = g.pendingFreeImageIDs[:0]
	g.pendingFreeShaderIDs = g.pendingFreeShaderIDs[:0]
}
