// Copyright 2018 The Ebiten Authors
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

package metal

import (
	"cmp"
	"fmt"
	"image"
	"math"
	"slices"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/ironpark/ggfx/internal/cocoa"
	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/ca"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/mtl"
	"github.com/ironpark/ggfx/internal/shader"
)

var sel_supportsFamily = objc.RegisterName("supportsFamily:")

type Graphics struct {
	device mtl.Device

	// surfaces are the presentation targets, in creation order.
	surfaces []*Surface

	// runOnMainThread runs a function on the main thread synchronously. It is set at most once.
	runOnMainThread func(f func())

	vsync bool

	tmpUniforms []uint32

	colorSpace color.ColorSpace

	cq  mtl.CommandQueue
	cb  mtl.CommandBuffer
	rce mtl.RenderCommandEncoder

	// frame is the current frame number.
	// frame is incremented when the screen is presented.
	frame int64

	// frameToCB maps a frame number to command buffers used in the frame.
	// frameToCB keeps command buffers not to be released until the command buffers are completed.
	frameToCB map[int64][]mtl.CommandBuffer

	buffers       map[int64][]mtl.Buffer
	unusedBuffers map[mtl.Buffer]struct{}

	lastDst *Image

	vb mtl.Buffer
	ib mtl.Buffer

	images      map[graphicsdriver.ImageID]*Image
	nextImageID graphicsdriver.ImageID

	shaders      map[graphicsdriver.ShaderID]*Shader
	nextShaderID graphicsdriver.ShaderID

	maxImageSize int
	tmpTextures  []mtl.Texture

	pool cocoa.NSAutoreleasePool
}

type stencilMode int

const (
	noStencil stencilMode = iota
	incrementStencil
	invertStencil
	drawWithStencil
)

var (
	systemDefaultDevice    mtl.Device
	systemDefaultDeviceErr error
)

func init() {
	// mtl.CreateSystemDefaultDevice must be called on the main thread (#2147).
	d, err := mtl.CreateSystemDefaultDevice()
	if err != nil {
		systemDefaultDeviceErr = err
		return
	}
	systemDefaultDevice = d
}

// NewGraphics creates an implementation of graphicsdriver.Graphics for Metal.
// The returned graphics value is nil iff the error is not nil.
func NewGraphics(colorSpace color.ColorSpace) (graphicsdriver.Graphics, error) {
	// On old mac devices like iMac 2011, Metal is not supported (#779).
	// TODO: Is there a better way to check whether Metal is available or not?
	// It seems OK to call MTLCreateSystemDefaultDevice multiple times, so this should be fine.
	if systemDefaultDeviceErr != nil {
		return nil, fmt.Errorf("metal: mtl.CreateSystemDefaultDevice failed: %w", systemDefaultDeviceErr)
	}

	g := &Graphics{
		colorSpace: colorSpace,
		device:     systemDefaultDevice,
		vsync:      true,
	}
	return g, nil
}

// Surface is a CAMetalLayer on one window.
type Surface struct {
	graphics *Graphics
	view     view

	// drawable is the drawable acquired for the current frame, and zero when none is.
	drawable ca.MetalDrawable

	// cleared reports whether the drawable has been cleared this frame. The first render pass on
	// a drawable clears it; later passes load what earlier ones drew, as an app draws to the screen
	// many times per frame.
	cleared bool
}

// NewSurface creates a layer for the NSWindow given as a uintptr. NewSurface must be called on the
// main thread.
func (g *Graphics) NewSurface(target any, transparent bool) (graphicsdriver.Surface, error) {
	window, ok := target.(uintptr)
	if !ok {
		return nil, fmt.Errorf("metal: NewSurface needs an NSWindow as uintptr but got %T", target)
	}
	s := &Surface{graphics: g}
	s.view.runOnMainThread = g.runOnMainThread
	if err := s.view.initialize(g.device, g.colorSpace); err != nil {
		s.view.release()
		return nil, err
	}
	s.view.ml.SetOpaque(!transparent)
	s.view.setDisplaySyncEnabled(g.vsync)
	s.view.setWindow(window)
	g.surfaces = append(g.surfaces, s)
	return s, nil
}

func (s *Surface) NewScreenImage(width, height int) (graphicsdriver.Image, error) {
	g := s.graphics
	s.view.setDrawableSize(width, height)
	i := &Image{
		id:       g.genNextImageID(),
		graphics: g,
		surface:  s,
		width:    width,
		height:   height,
		screen:   true,
	}
	g.addImage(i)
	return i, nil
}

func (s *Surface) Dispose() {
	g := s.graphics
	if s.drawable != (ca.MetalDrawable{}) {
		s.drawable.Release()
		s.drawable = ca.MetalDrawable{}
	}
	s.view.release()
	g.surfaces = slices.DeleteFunc(g.surfaces, func(t *Surface) bool { return t == s })
}

// texture returns the drawable's texture for the current frame, acquiring a drawable if needed.
// It returns a zero texture when no drawable is available.
func (s *Surface) texture() mtl.Texture {
	if s.drawable == (ca.MetalDrawable{}) {
		drawable := s.view.nextDrawable()
		if drawable == (ca.MetalDrawable{}) {
			return mtl.Texture{}
		}
		// Keep the drawable alive across flushes that drain the autorelease pool without presenting (#3704).
		drawable.Retain()
		s.drawable = drawable
		s.cleared = false
		// After nextDrawable, it is expected some command buffers are completed.
		s.graphics.gcBuffers()
	}
	return s.drawable.Texture()
}

func (g *Graphics) ColorSpace() color.ColorSpace {
	return g.colorSpace
}

func (g *Graphics) Begin() error {
	// NSAutoreleasePool is required to release drawable correctly (#847).
	// https://developer.apple.com/library/archive/documentation/3DDrawing/Conceptual/MTLBestPracticesGuide/Drawables.html
	g.pool = cocoa.NSAutoreleasePool_new()
	for _, s := range g.surfaces {
		s.view.updatePresentationState()
	}
	return nil
}

func (g *Graphics) End(mode graphicsdriver.FlushMode) error {
	g.flushCommandBufferIfNeeded(mode == graphicsdriver.FlushModePresent)
	g.pool.Release()
	g.pool.ID = 0
	if mode != graphicsdriver.FlushModeIntermediate {
		g.frame++
	}
	// Reclaim the resources for the past frames here, as a drawable is not always obtained in a frame.
	g.gcBuffers()
	return nil
}

// SetMainThreadRunner sets a function that runs the given function on the main thread synchronously.
//
// The runner must be able to run a function even while the main thread is blocked until the
// current frame ends, like during window resizing.
func (g *Graphics) SetMainThreadRunner(f func(func())) {
	g.runOnMainThread = f
	for _, s := range g.surfaces {
		s.view.runOnMainThread = f
	}
}

func pow2(x uintptr) uintptr {
	if x > (math.MaxUint+1)/2 {
		return math.MaxUint
	}

	var p2 uintptr = 1
	for p2 < x {
		p2 *= 2
	}
	return p2
}

func (g *Graphics) gcBuffers() {
loop:
	for frame, cbs := range g.frameToCB {
		if frame == g.frame {
			continue
		}

		// Check if all command buffers for the frame are completed.
		for _, cb := range cbs {
			if cb.Status() != mtl.CommandBufferStatusCompleted {
				continue loop
			}
		}
		for _, cb := range cbs {
			cb.Release()
		}
		delete(g.frameToCB, frame)

		for _, b := range g.buffers[frame] {
			if g.unusedBuffers == nil {
				g.unusedBuffers = map[mtl.Buffer]struct{}{}
			}
			g.unusedBuffers[b] = struct{}{}
		}
		delete(g.buffers, frame)
	}

	const maxUnusedBuffers = 10
	if len(g.unusedBuffers) > maxUnusedBuffers {
		bufs := make([]mtl.Buffer, 0, len(g.unusedBuffers))
		for b := range g.unusedBuffers {
			bufs = append(bufs, b)
		}
		slices.SortFunc(bufs, func(a, b mtl.Buffer) int {
			return cmp.Compare(b.Length(), a.Length())
		})
		for _, b := range bufs[maxUnusedBuffers:] {
			delete(g.unusedBuffers, b)
			b.Release()
		}
	}
}

func (g *Graphics) ensureCommandBuffer() {
	if g.cb != (mtl.CommandBuffer{}) {
		return
	}
	g.cb = g.cq.CommandBuffer()
	if g.frameToCB == nil {
		g.frameToCB = map[int64][]mtl.CommandBuffer{}
	}
	g.frameToCB[g.frame] = append(g.frameToCB[g.frame], g.cb)
	g.cb.Retain()
}

func (g *Graphics) availableBuffer(length uintptr) mtl.Buffer {
	g.ensureCommandBuffer()

	var newBuf mtl.Buffer
	for b := range g.unusedBuffers {
		if b.Length() >= length {
			newBuf = b
			delete(g.unusedBuffers, b)
			break
		}
	}

	if newBuf == (mtl.Buffer{}) {
		newBuf = g.device.NewBufferWithLength(pow2(length), resourceStorageMode)
	}

	if g.buffers == nil {
		g.buffers = map[int64][]mtl.Buffer{}
	}
	g.buffers[g.frame] = append(g.buffers[g.frame], newBuf)
	return newBuf
}

func (g *Graphics) SetVertices(vertices []float32, indices []uint32) error {
	vbSize := unsafe.Sizeof(vertices[0]) * uintptr(len(vertices))
	ibSize := unsafe.Sizeof(indices[0]) * uintptr(len(indices))

	g.vb = g.availableBuffer(vbSize)
	g.vb.CopyToContents(unsafe.Pointer(&vertices[0]), vbSize)

	g.ib = g.availableBuffer(ibSize)
	g.ib.CopyToContents(unsafe.Pointer(&indices[0]), ibSize)

	return nil
}

func (g *Graphics) flushCommandBufferIfNeeded(present bool) {
	if g.cb == (mtl.CommandBuffer{}) {
		if g.rce != (mtl.RenderCommandEncoder{}) {
			panic("metal: render command encoder must be empty if command buffer is empty")
		}
		return
	}

	g.flushRenderCommandEncoderIfNeeded()

	var presented []*Surface
	var withTransaction []*Surface
	if present {
		for _, s := range g.surfaces {
			if s.drawable == (ca.MetalDrawable{}) {
				continue
			}
			if s.view.shouldPresentWithTransaction() {
				// The drawable must be presented after the command buffer is committed and scheduled.
				withTransaction = append(withTransaction, s)
			} else {
				s.view.presentDrawable(g.cb, s.drawable)
			}
			presented = append(presented, s)
		}
	}

	g.cb.Commit()

	for _, s := range withTransaction {
		s.view.presentDrawableWithTransaction(g.cb, s.drawable)
	}

	for _, t := range g.tmpTextures {
		t.Release()
	}
	g.tmpTextures = g.tmpTextures[:0]

	g.cb = mtl.CommandBuffer{}

	for _, s := range presented {
		s.drawable.Release()
		s.drawable = ca.MetalDrawable{}
		s.view.finishDrawableUsage()
	}
}

func (g *Graphics) checkSize(width, height int) {
	if width < 1 {
		panic(fmt.Sprintf("metal: width (%d) must be equal or more than %d", width, 1))
	}
	if height < 1 {
		panic(fmt.Sprintf("metal: height (%d) must be equal or more than %d", height, 1))
	}
	m := g.MaxImageSize()
	if width > m {
		panic(fmt.Sprintf("metal: width (%d) must be less than or equal to %d", width, m))
	}
	if height > m {
		panic(fmt.Sprintf("metal: height (%d) must be less than or equal to %d", height, m))
	}
}

func (g *Graphics) genNextImageID() graphicsdriver.ImageID {
	g.nextImageID++
	return g.nextImageID
}

func (g *Graphics) genNextShaderID() graphicsdriver.ShaderID {
	g.nextShaderID++
	return g.nextShaderID
}

func (g *Graphics) NewImage(width, height int) (graphicsdriver.Image, error) {
	g.checkSize(width, height)
	td := mtl.TextureDescriptor{
		TextureType: mtl.TextureType2D,
		PixelFormat: mtl.PixelFormatRGBA8UNorm,
		Width:       graphics.InternalImageSize(width),
		Height:      graphics.InternalImageSize(height),
		StorageMode: storageMode,
		Usage:       mtl.TextureUsageShaderRead | mtl.TextureUsageRenderTarget,
	}
	t := g.device.NewTextureWithDescriptor(td)
	i := &Image{
		id:       g.genNextImageID(),
		graphics: g,
		width:    width,
		height:   height,
		texture:  t,
	}
	g.addImage(i)
	return i, nil
}

func (g *Graphics) addImage(img *Image) {
	if g.images == nil {
		g.images = map[graphicsdriver.ImageID]*Image{}
	}
	if _, ok := g.images[img.id]; ok {
		panic(fmt.Sprintf("metal: image ID %d was already registered", img.id))
	}
	g.images[img.id] = img
}

func (g *Graphics) removeImage(img *Image) {
	delete(g.images, img.id)
}

func blendFactorToMetalBlendFactor(c graphicsdriver.BlendFactor) mtl.BlendFactor {
	switch c {
	case graphicsdriver.BlendFactorZero:
		return mtl.BlendFactorZero
	case graphicsdriver.BlendFactorOne:
		return mtl.BlendFactorOne
	case graphicsdriver.BlendFactorSourceColor:
		return mtl.BlendFactorSourceColor
	case graphicsdriver.BlendFactorOneMinusSourceColor:
		return mtl.BlendFactorOneMinusSourceColor
	case graphicsdriver.BlendFactorSourceAlpha:
		return mtl.BlendFactorSourceAlpha
	case graphicsdriver.BlendFactorOneMinusSourceAlpha:
		return mtl.BlendFactorOneMinusSourceAlpha
	case graphicsdriver.BlendFactorDestinationColor:
		return mtl.BlendFactorDestinationColor
	case graphicsdriver.BlendFactorOneMinusDestinationColor:
		return mtl.BlendFactorOneMinusDestinationColor
	case graphicsdriver.BlendFactorDestinationAlpha:
		return mtl.BlendFactorDestinationAlpha
	case graphicsdriver.BlendFactorOneMinusDestinationAlpha:
		return mtl.BlendFactorOneMinusDestinationAlpha
	case graphicsdriver.BlendFactorSourceAlphaSaturated:
		return mtl.BlendFactorSourceAlphaSaturated
	default:
		panic(fmt.Sprintf("metal: invalid blend factor: %d", c))
	}
}

func blendOperationToMetalBlendOperation(o graphicsdriver.BlendOperation) mtl.BlendOperation {
	switch o {
	case graphicsdriver.BlendOperationAdd:
		return mtl.BlendOperationAdd
	case graphicsdriver.BlendOperationSubtract:
		return mtl.BlendOperationSubtract
	case graphicsdriver.BlendOperationReverseSubtract:
		return mtl.BlendOperationReverseSubtract
	case graphicsdriver.BlendOperationMin:
		return mtl.BlendOperationMin
	case graphicsdriver.BlendOperationMax:
		return mtl.BlendOperationMax
	default:
		panic(fmt.Sprintf("metal: invalid blend operation: %d", o))
	}
}

func (g *Graphics) Initialize() error {
	g.cq = g.device.NewCommandQueue()
	return nil
}

func (g *Graphics) flushRenderCommandEncoderIfNeeded() {
	if g.rce == (mtl.RenderCommandEncoder{}) {
		return
	}
	g.rce.EndEncoding()
	g.rce = mtl.RenderCommandEncoder{}
	g.lastDst = nil
}

func (g *Graphics) draw(dst *Image, dstRegions []graphicsdriver.DstRegion, srcs [graphics.ShaderSrcImageCount]*Image, indexOffset int, shader *Shader, uniforms []uint32, blend graphicsdriver.Blend) error {
	// In order to create a separate command buffer for the screen, flush the current command buffer.
	// It's because a drawable will not be released as long as the CommandBuffer referencing it is alive,
	// it is more efficient to separate CommandBuffers that use the drawable from those that do not.
	var lastSurface *Surface
	if g.lastDst != nil {
		lastSurface = g.lastDst.surface
	}
	if lastSurface != dst.surface {
		g.flushCommandBufferIfNeeded(false)
	}

	// When preparing a stencil buffer, flush the current render command encoder
	// to make sure the stencil buffer is cleared when loading.
	// TODO: What about clearing the stencil buffer by vertices?
	if g.lastDst != dst {
		g.flushRenderCommandEncoderIfNeeded()
	}
	g.lastDst = dst

	if g.rce == (mtl.RenderCommandEncoder{}) {
		var rpd mtl.RenderPassDescriptor
		t := dst.mtlTexture()
		if t == (mtl.Texture{}) {
			return nil
		}

		// Even though the destination pixels are not used, mtl.LoadActionDontCare might cause glitches
		// (#1019). Always using mtl.LoadActionLoad is safe. A drawable is cleared by its first pass
		// of the frame only; mtlTexture acquired it above, so the flag is current.
		rpd.ColorAttachments[0].LoadAction = mtl.LoadActionLoad
		if dst.screen && !dst.surface.cleared {
			rpd.ColorAttachments[0].LoadAction = mtl.LoadActionClear
			dst.surface.cleared = true
		}

		// The store action should always be 'store' even for the screen (#1700).
		rpd.ColorAttachments[0].StoreAction = mtl.StoreActionStore
		rpd.ColorAttachments[0].Texture = t
		rpd.ColorAttachments[0].ClearColor = mtl.ClearColor{}

		g.ensureCommandBuffer()
		g.rce = g.cb.RenderCommandEncoderWithDescriptor(rpd)
	}

	w, h := dst.internalSize()
	g.rce.SetViewport(mtl.Viewport{
		OriginX: 0,
		OriginY: 0,
		Width:   float64(w),
		Height:  float64(h),
		ZNear:   -1,
		ZFar:    1,
	})
	g.rce.SetVertexBuffer(g.vb, 0, 0)

	// The internal uniform block comes first, then the user's block. Both are already laid out as
	// the shader expects. In Metal, the NDC's Y direction (upward) and the framebuffer's Y
	// direction (downward) don't match, so the projection matrix's Y is inverted on a copy.
	g.tmpUniforms = append(g.tmpUniforms[:0], uniforms[:graphics.PreservedUniformDwordCount]...)
	const p = graphics.ProjectionMatrixUniformDwordIndex
	g.tmpUniforms[p+1] ^= 1 << 31
	g.tmpUniforms[p+5] ^= 1 << 31
	g.tmpUniforms[p+9] ^= 1 << 31
	g.tmpUniforms[p+13] ^= 1 << 31
	head := unsafe.SliceData(g.tmpUniforms)
	g.rce.SetVertexBytes(unsafe.Pointer(head), unsafe.Sizeof(uniforms[0])*uintptr(len(g.tmpUniforms)), internalUniformBufferIndex)
	g.rce.SetFragmentBytes(unsafe.Pointer(head), unsafe.Sizeof(uniforms[0])*uintptr(len(g.tmpUniforms)), internalUniformBufferIndex)
	if user := uniforms[graphics.PreservedUniformDwordCount:]; len(user) > 0 {
		head := unsafe.SliceData(user)
		g.rce.SetVertexBytes(unsafe.Pointer(head), unsafe.Sizeof(user[0])*uintptr(len(user)), userUniformBufferIndex)
		g.rce.SetFragmentBytes(unsafe.Pointer(head), unsafe.Sizeof(user[0])*uintptr(len(user)), userUniformBufferIndex)
	}

	for i, src := range srcs {
		if src != nil {
			g.rce.SetFragmentTexture(src.texture, i)
		} else {
			g.rce.SetFragmentTexture(mtl.Texture{}, i)
		}
	}

	s, err := shader.RenderPipelineState(dst.colorPixelFormat(), blend, dst.screen)
	if err != nil {
		return err
	}
	rps := s

	for _, dstRegion := range dstRegions {
		g.rce.SetScissorRect(mtl.ScissorRect{
			X:      dstRegion.Region.Min.X,
			Y:      dstRegion.Region.Min.Y,
			Width:  dstRegion.Region.Dx(),
			Height: dstRegion.Region.Dy(),
		})

		g.rce.SetRenderPipelineState(rps)
		g.rce.DrawIndexedPrimitives(mtl.PrimitiveTypeTriangle, dstRegion.IndexCount, mtl.IndexTypeUInt32, g.ib, indexOffset*int(unsafe.Sizeof(uint32(0))))

		indexOffset += dstRegion.IndexCount
	}

	return nil
}

func (g *Graphics) DrawTriangles(dstID graphicsdriver.ImageID, srcIDs [graphics.ShaderSrcImageCount]graphicsdriver.ImageID, shaderID graphicsdriver.ShaderID, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32) error {
	if shaderID == graphicsdriver.InvalidShaderID {
		return fmt.Errorf("metal: shader ID is invalid")
	}

	dst := g.images[dstID]

	if dst.screen {
		dst.surface.view.update()
	}

	var srcs [graphics.ShaderSrcImageCount]*Image
	for i, srcID := range srcIDs {
		srcs[i] = g.images[srcID]
	}

	if err := g.draw(dst, dstRegions, srcs, indexOffset, g.shaders[shaderID], uniforms, blend); err != nil {
		return err
	}

	return nil
}

func (g *Graphics) SetVsyncEnabled(enabled bool) {
	g.vsync = enabled
	for _, s := range g.surfaces {
		s.view.setDisplaySyncEnabled(enabled)
	}
}

func (g *Graphics) NeedsClearingScreen() bool {
	return false
}

func (g *Graphics) MaxImageSize() int {
	if g.maxImageSize != 0 {
		return g.maxImageSize
	}

	d := g.device

	// supportsFamily is available as of macOS 10.15+ and iOS 13.0+.
	// https://developer.apple.com/documentation/metal/mtldevice/3143473-supportsfamily
	if d.RespondsToSelector(sel_supportsFamily) {
		// https://developer.apple.com/metal/Metal-Feature-Set-Tables.pdf
		g.maxImageSize = 8192
		switch {
		case d.SupportsFamily(mtl.GPUFamilyApple3):
			g.maxImageSize = 16384
		case d.SupportsFamily(mtl.GPUFamilyMac2):
			g.maxImageSize = 16384
		}
		return g.maxImageSize
	}

	// supportsFeatureSet is deprecated but some old macOS/iOS versions support only this (#2553).
	switch {
	case d.SupportsFeatureSet(mtl.FeatureSet_iOS_GPUFamily5_v1):
		g.maxImageSize = 16384
	case d.SupportsFeatureSet(mtl.FeatureSet_iOS_GPUFamily4_v1):
		g.maxImageSize = 16384
	case d.SupportsFeatureSet(mtl.FeatureSet_iOS_GPUFamily3_v1):
		g.maxImageSize = 16384
	case d.SupportsFeatureSet(mtl.FeatureSet_iOS_GPUFamily2_v2):
		g.maxImageSize = 8192
	case d.SupportsFeatureSet(mtl.FeatureSet_iOS_GPUFamily2_v1):
		g.maxImageSize = 4096
	case d.SupportsFeatureSet(mtl.FeatureSet_iOS_GPUFamily1_v2):
		g.maxImageSize = 8192
	case d.SupportsFeatureSet(mtl.FeatureSet_iOS_GPUFamily1_v1):
		g.maxImageSize = 4096
	case d.SupportsFeatureSet(mtl.FeatureSet_tvOS_GPUFamily2_v1):
		g.maxImageSize = 16384
	case d.SupportsFeatureSet(mtl.FeatureSet_tvOS_GPUFamily1_v1):
		g.maxImageSize = 8192
	case d.SupportsFeatureSet(mtl.FeatureSet_macOS_GPUFamily1_v1):
		g.maxImageSize = 16384
	default:
		panic("metal: there is no supported feature set")
	}
	return g.maxImageSize
}

func (g *Graphics) NewShader(program *shader.Program) (graphicsdriver.Shader, error) {
	s, err := newShader(g.genNextShaderID(), g, g.device, program)
	if err != nil {
		return nil, err
	}
	g.addShader(s)
	return s, nil
}

func (g *Graphics) addShader(shader *Shader) {
	if g.shaders == nil {
		g.shaders = map[graphicsdriver.ShaderID]*Shader{}
	}
	if _, ok := g.shaders[shader.id]; ok {
		panic(fmt.Sprintf("metal: shader ID %d was already registered", shader.id))
	}
	g.shaders[shader.id] = shader
}

func (g *Graphics) removeShader(shader *Shader) {
	delete(g.shaders, shader.id)
}

type Image struct {
	id       graphicsdriver.ImageID
	graphics *Graphics
	width    int
	height   int
	screen   bool
	surface  *Surface // non-nil iff screen
	texture  mtl.Texture
	stencil  mtl.Texture
}

func (i *Image) colorPixelFormat() mtl.PixelFormat {
	if i.screen {
		return i.surface.view.colorPixelFormat()
	}
	return mtl.PixelFormatRGBA8UNorm
}

func (i *Image) ID() graphicsdriver.ImageID {
	return i.id
}

func (i *Image) internalSize() (int, int) {
	if i.screen {
		return i.width, i.height
	}
	return graphics.InternalImageSize(i.width), graphics.InternalImageSize(i.height)
}

func (i *Image) Dispose() {
	if i.stencil != (mtl.Texture{}) {
		i.stencil.Release()
		i.stencil = mtl.Texture{}
	}
	if i.texture != (mtl.Texture{}) {
		i.texture.Release()
		i.texture = mtl.Texture{}
	}
	i.graphics.removeImage(i)
}

func (i *Image) syncTexture() {
	i.graphics.flushCommandBufferIfNeeded(false)

	// Calling SynchronizeTexture is ignored on iOS (see mtl.m), but it looks like committing BlitCommandEncoder
	// is necessary (#1337).
	if i.graphics.cb != (mtl.CommandBuffer{}) {
		panic("metal: command buffer must be empty at syncTexture")
	}

	cb := i.graphics.cq.CommandBuffer()
	bce := cb.BlitCommandEncoder()
	bce.SynchronizeTexture(i.texture, 0, 0)
	bce.EndEncoding()

	cb.Commit()
	// TODO: Are fences available here?
	cb.WaitUntilCompleted()
}

func (i *Image) ReadPixels(args []graphicsdriver.PixelsArgs) error {
	i.syncTexture()

	for _, arg := range args {
		if got, want := len(arg.Pixels), 4*arg.Region.Dx()*arg.Region.Dy(); got != want {
			return fmt.Errorf("metal: len(buf) must be %d but %d at ReadPixels", want, got)
		}
		if err := i.texture.GetBytes(arg.Pixels, 4*arg.Region.Dx(), mtl.Region{
			Origin: mtl.Origin{X: arg.Region.Min.X, Y: arg.Region.Min.Y},
			Size:   mtl.Size{Width: arg.Region.Dx(), Height: arg.Region.Dy(), Depth: 1},
		}, 0); err != nil {
			return err
		}
	}
	return nil
}

func (i *Image) WritePixels(args []graphicsdriver.PixelsArgs) error {
	g := i.graphics

	g.flushRenderCommandEncoderIfNeeded()

	// Calculate the smallest texture size to include all the values in args.
	var region image.Rectangle
	for _, a := range args {
		region = region.Union(a.Region)
	}

	// Use a temporary texture to send pixels asynchronously, whichever the memory is shared (e.g., iOS) or
	// managed (e.g., macOS). A temporary texture is needed since ReplaceRegion tries to sync the pixel
	// data between CPU and GPU, and doing it on the existing texture is inefficient (#1418).
	// The texture cannot be reused until sending the pixels finishes, then create new ones for each call.
	td := mtl.TextureDescriptor{
		TextureType: mtl.TextureType2D,
		PixelFormat: mtl.PixelFormatRGBA8UNorm,
		Width:       region.Dx(),
		Height:      region.Dy(),
		StorageMode: storageMode,
		Usage:       mtl.TextureUsageShaderRead | mtl.TextureUsageRenderTarget,
	}
	t := g.device.NewTextureWithDescriptor(td)
	g.tmpTextures = append(g.tmpTextures, t)

	for _, a := range args {
		if err := t.ReplaceRegion(mtl.Region{
			Origin: mtl.Origin{X: a.Region.Min.X - region.Min.X, Y: a.Region.Min.Y - region.Min.Y, Z: 0},
			Size:   mtl.Size{Width: a.Region.Dx(), Height: a.Region.Dy(), Depth: 1},
		}, 0, a.Pixels, 4*a.Region.Dx()); err != nil {
			return err
		}
	}

	g.ensureCommandBuffer()
	bce := g.cb.BlitCommandEncoder()
	for _, a := range args {
		so := mtl.Origin{X: a.Region.Min.X - region.Min.X, Y: a.Region.Min.Y - region.Min.Y, Z: 0}
		ss := mtl.Size{Width: a.Region.Dx(), Height: a.Region.Dy(), Depth: 1}
		do := mtl.Origin{X: a.Region.Min.X, Y: a.Region.Min.Y, Z: 0}
		bce.CopyFromTexture(t, 0, 0, so, ss, i.texture, 0, 0, do)
	}
	bce.EndEncoding()

	return nil
}

func (i *Image) mtlTexture() mtl.Texture {
	if i.screen {
		return i.surface.texture()
	}
	return i.texture
}

func (i *Image) ensureStencil() {
	if i.stencil != (mtl.Texture{}) {
		return
	}

	td := mtl.TextureDescriptor{
		TextureType: mtl.TextureType2D,
		PixelFormat: mtl.PixelFormatStencil8,
		Width:       graphics.InternalImageSize(i.width),
		Height:      graphics.InternalImageSize(i.height),
		StorageMode: mtl.StorageModePrivate,
		Usage:       mtl.TextureUsageRenderTarget,
	}
	i.stencil = i.graphics.device.NewTextureWithDescriptor(td)
}
