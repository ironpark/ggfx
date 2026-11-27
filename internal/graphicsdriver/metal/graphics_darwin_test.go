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

package metal

import (
	"image"
	"math"
	"runtime"
	"slices"
	"testing"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/mtl"
	"github.com/ironpark/ggfx/internal/shader"
)

func newTestGraphics(t *testing.T) *Graphics {
	t.Helper()
	// NSAutoreleasePool created in Begin must be released on the same OS thread in End.
	// Cleanup runs LIFO, so register UnlockOSThread first and Metal teardown second.
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	drv, err := NewGraphics(color.ColorSpaceSRGB)
	if err != nil {
		t.Skipf("no Metal device: %v", err)
	}
	g := drv.(*Graphics)
	if err := g.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() {
		g.flushCommandBufferIfNeeded(false)
		for _, r := range g.inFlight {
			for _, cb := range r.cbs {
				if cb.Status() < mtl.CommandBufferStatusCompleted {
					cb.WaitUntilCompleted()
				}
				cb.Release()
			}
			for _, b := range r.buffers {
				b.Release()
			}
		}
		g.inFlight = nil
		g.unusedBuffers.evict(g.gen+maxIdleBufferFrames+1, mtl.Buffer.Release)
		for _, vs := range g.vertexStages {
			vs.fn.Release()
			vs.lib.Release()
		}
		g.vertexStages = nil
		if g.cq != (mtl.CommandQueue{}) {
			g.cq.Release()
			g.cq = mtl.CommandQueue{}
		}
	})
	return g
}

func makeInternalUniforms(p *shader.Program, w, h int) []uint32 {
	iw, ih := float32(graphics.InternalImageSize(w)), float32(graphics.InternalImageSize(h))
	u := make([]uint32, graphics.PreservedUniformDwordCount+p.UniformDwordCount)
	u[graphics.DstTextureSizeUniformDwordIndex] = math.Float32bits(iw)
	u[graphics.DstTextureSizeUniformDwordIndex+1] = math.Float32bits(ih)
	m := u[graphics.ProjectionMatrixUniformDwordIndex : graphics.ProjectionMatrixUniformDwordIndex+16]
	m[0] = math.Float32bits(2 / iw)
	m[5] = math.Float32bits(-2 / ih)
	m[10] = math.Float32bits(1)
	m[12] = math.Float32bits(-1)
	m[13] = math.Float32bits(1)
	m[15] = math.Float32bits(1)
	return u
}

func fullQuadVertices(w, h float32) ([]float32, []uint32) {
	vs := make([]float32, 4*graphics.VertexFloatCount)
	graphics.QuadVerticesFromDstAndSrc(vs, 0, 0, w, h, 0, 0, w, h, 1, 1, 1, 1)
	return vs, graphics.QuadIndices()
}

// TestUniformVersionCacheDifferentShadersAndSizes verifies that drawing two shaders with the same
// uniform version (different block sizes or same block size) still uploads each shader's own block.
func TestUniformVersionCacheDifferentShadersAndSizes(t *testing.T) {
	g := newTestGraphics(t)

	smallProg, err := shader.CompileUser([]byte(`
struct Uniforms {
	Color: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f { return u.Color; }
`))
	if err != nil {
		t.Fatal(err)
	}
	largeProg, err := shader.CompileUser([]byte(`
struct Uniforms {
	Pad: array<vec4f, 7>,
	Color: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f { return u.Color; }
`))
	if err != nil {
		t.Fatal(err)
	}
	sameSizeProg, err := shader.CompileUser([]byte(`
struct Uniforms {
	Color: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f { return u.Color; }
`))
	if err != nil {
		t.Fatal(err)
	}
	internalProgA, err := shader.CompileUser([]byte(`
fn fragment(v: Vertex) -> vec4f { return vec4f(dst_origin() / 16.0, 0.0, 1.0); }
`))
	if err != nil {
		t.Fatal(err)
	}
	internalProgB, err := shader.CompileUser([]byte(`
fn fragment(v: Vertex) -> vec4f { return vec4f(dst_size() / 16.0, 0.0, 1.0); }
`))
	if err != nil {
		t.Fatal(err)
	}

	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	defer g.End(graphicsdriver.FlushModeEndFrame)

	const w, h = 16, 16
	dst, err := g.NewImage(w, h, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Dispose()

	shSmall, err := g.NewShader(smallProg.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer shSmall.Dispose()
	shLarge, err := g.NewShader(largeProg.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer shLarge.Dispose()
	shSame, err := g.NewShader(sameSizeProg.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer shSame.Dispose()
	shIntA, err := g.NewShader(internalProgA.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer shIntA.Dispose()
	shIntB, err := g.NewShader(internalProgB.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer shIntB.Dispose()

	vs, is := fullQuadVertices(w, h)
	if err := g.SetVertices(vs, is); err != nil {
		t.Fatal(err)
	}

	// Draw 1: 16-byte user block, version 7, red -> [0,0)-(4,16).
	uSmall := makeInternalUniforms(smallProg, w, h)
	uSmall[graphics.PreservedUniformDwordCount+0] = math.Float32bits(1)
	uSmall[graphics.PreservedUniformDwordCount+3] = math.Float32bits(1)

	// Draw 2: 128-byte user block, same version 7, green at offset 112 -> [4,0)-(8,16).
	uLarge := makeInternalUniforms(largeProg, w, h)
	uLarge[graphics.PreservedUniformDwordCount+28+1] = math.Float32bits(1)
	uLarge[graphics.PreservedUniformDwordCount+28+3] = math.Float32bits(1)

	// Draw 3: 16-byte user block on a third shader, same version 7, blue -> [8,0)-(12,16).
	uSame := makeInternalUniforms(sameSizeProg, w, h)
	uSame[graphics.PreservedUniformDwordCount+2] = math.Float32bits(1)
	uSame[graphics.PreservedUniformDwordCount+3] = math.Float32bits(1)

	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
	sameVersions := graphicsdriver.UniformVersions{Internal: 100, User: 7}
	if err := g.DrawTriangles(dst, srcs, shSmall, []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 4, 16), IndexCount: len(is)}}, 0, graphicsdriver.BlendCopy, uSmall, sameVersions); err != nil {
		t.Fatal(err)
	}
	if err := g.DrawTriangles(dst, srcs, shLarge, []graphicsdriver.DstRegion{{Region: image.Rect(4, 0, 8, 16), IndexCount: len(is)}}, 0, graphicsdriver.BlendCopy, uLarge, sameVersions); err != nil {
		t.Fatal(err)
	}
	if err := g.DrawTriangles(dst, srcs, shSame, []graphicsdriver.DstRegion{{Region: image.Rect(8, 0, 12, 16), IndexCount: len(is)}}, 0, graphicsdriver.BlendCopy, uSame, sameVersions); err != nil {
		t.Fatal(err)
	}

	// Draw 4 & 5: two shaders reading different parts of internal uniforms with the same internal version.
	uIntA := makeInternalUniforms(internalProgA, w, h)
	uIntA[graphics.DstRegionOriginUniformDwordIndex] = math.Float32bits(16) // red
	uIntB := makeInternalUniforms(internalProgB, w, h)
	uIntB[graphics.DstRegionSizeUniformDwordIndex+1] = math.Float32bits(16) // green
	if err := g.DrawTriangles(dst, srcs, shIntA, []graphicsdriver.DstRegion{{Region: image.Rect(12, 0, 14, 16), IndexCount: len(is)}}, 0, graphicsdriver.BlendCopy, uIntA, graphicsdriver.UniformVersions{Internal: 200, User: 1}); err != nil {
		t.Fatal(err)
	}
	if err := g.DrawTriangles(dst, srcs, shIntB, []graphicsdriver.DstRegion{{Region: image.Rect(14, 0, 16, 16), IndexCount: len(is)}}, 0, graphicsdriver.BlendCopy, uIntB, graphicsdriver.UniformVersions{Internal: 200, User: 1}); err != nil {
		t.Fatal(err)
	}

	pix := make([]byte, 4*w*h)
	if err := dst.ReadPixels([]graphicsdriver.PixelsArgs{{Pixels: pix, Region: image.Rect(0, 0, w, h)}}); err != nil {
		t.Fatal(err)
	}
	checkPx := func(x, y int, want [4]byte) {
		t.Helper()
		off := 4 * (y*w + x)
		got := [4]byte(pix[off : off+4])
		if got != want {
			t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, want)
		}
	}
	checkPx(2, 8, [4]byte{255, 0, 0, 255})
	checkPx(6, 8, [4]byte{0, 255, 0, 255})
	checkPx(10, 8, [4]byte{0, 0, 255, 255})
	checkPx(13, 8, [4]byte{255, 0, 0, 255})
	checkPx(15, 8, [4]byte{0, 255, 0, 255})
}

// TestLargeUserUniformAlignment checks that user uniform buffers over 4096 bytes receive a
// 256-byte-aligned offset even after a 16-byte pixel upload in the same staging buffer.
func TestLargeUserUniformAlignment(t *testing.T) {
	g := newTestGraphics(t)
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	defer g.End(graphicsdriver.FlushModeEndFrame)

	buf1, off1 := g.uploadSpace(16, uploadAlignment)
	if buf1 == (mtl.Buffer{}) || off1 != 0 {
		t.Fatalf("first uploadSpace(16, 16) = (%v, %d), want non-zero buf at 0", buf1, off1)
	}
	buf2, off2 := g.uploadSpace(4112, uniformBufferAlignment)
	if buf2 == (mtl.Buffer{}) {
		t.Fatal("uploadSpace(4112, 256) returned zero buffer")
	}
	if off2%uniformBufferAlignment != 0 {
		t.Errorf("uniform upload offset = %d, want multiple of %d", off2, uniformBufferAlignment)
	}
	if off2 != 256 {
		t.Errorf("uniform upload offset = %d, want 256", off2)
	}

	// End-to-end draw with a >4096-byte user block after a 16-byte WritePixels upload.
	prog, err := shader.CompileUser([]byte(`
struct Uniforms {
	Pad: array<vec4f, 256>,
	Color: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f { return u.Color; }
`))
	if err != nil {
		t.Fatal(err)
	}
	sh, err := g.NewShader(prog.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Dispose()
	const w, h = 16, 16
	dst, err := g.NewImage(w, h, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Dispose()
	// 2x2 RGBA = 16 bytes advances uploadOffset to a non-256-aligned offset.
	if err := dst.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: make([]byte, 16), Region: image.Rect(0, 0, 2, 2)}}); err != nil {
		t.Fatal(err)
	}
	vs, is := fullQuadVertices(w, h)
	if err := g.SetVertices(vs, is); err != nil {
		t.Fatal(err)
	}
	u := makeInternalUniforms(prog, w, h)
	base := graphics.PreservedUniformDwordCount + 256*4
	u[base+0] = math.Float32bits(0)
	u[base+1] = math.Float32bits(1)
	u[base+2] = math.Float32bits(1)
	u[base+3] = math.Float32bits(1)
	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
	if err := g.DrawTriangles(dst, srcs, sh, []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, w, h), IndexCount: len(is)}}, 0, graphicsdriver.BlendCopy, u, graphicsdriver.UniformVersions{Internal: 1, User: 1}); err != nil {
		t.Fatal(err)
	}
	pix := make([]byte, 4*w*h)
	if err := dst.ReadPixels([]graphicsdriver.PixelsArgs{{Pixels: pix, Region: image.Rect(0, 0, w, h)}}); err != nil {
		t.Fatal(err)
	}
	got := [4]byte(pix[0:4])
	want := [4]byte{0, 255, 255, 255}
	if got != want {
		t.Errorf("large uniform draw pixel = %v, want %v", got, want)
	}
}

// TestAvailableBufferFailedAllocationNotPooled checks that a failed Metal buffer allocation returns
// zero without recording a zero buffer in inFlight or clobbering uploadBuffer.
func TestAvailableBufferFailedAllocationNotPooled(t *testing.T) {
	g := newTestGraphics(t)
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	defer g.End(graphicsdriver.FlushModeEndFrame)

	validBuf, _ := g.uploadSpace(64, uploadAlignment)
	if validBuf == (mtl.Buffer{}) {
		t.Fatal("uploadSpace(64) failed")
	}

	// Request larger than any Metal device's maxBufferLength (or NextPowerOf2 overflow).
	const huge = uintptr(1) << 62
	if b := g.availableBuffer(huge); b != (mtl.Buffer{}) {
		t.Fatalf("availableBuffer(%d) unexpectedly succeeded", huge)
	}
	if b, _ := g.uploadSpace(huge, uploadAlignment); b != (mtl.Buffer{}) {
		t.Fatalf("uploadSpace(%d) unexpectedly succeeded", huge)
	}
	if g.uploadBuffer != validBuf {
		t.Errorf("failed uploadSpace clobbered existing uploadBuffer")
	}
	for _, r := range g.inFlight {
		for _, b := range r.buffers {
			if b == (mtl.Buffer{}) {
				t.Errorf("zero buffer recorded in inFlight generation %d", r.gen)
			}
		}
	}
}

// TestSurfaceKeepLostNotRecordedAsLast checks that when Kept returned true but startFrom could not
// copy last (because the window hid or resized), presented does not record the incomplete texture
// as last, so the next frame sees Kept() == false and redraws whole.
func TestSurfaceKeepLostNotRecordedAsLast(t *testing.T) {
	g := newTestGraphics(t)
	mkTex := func(w, h int) mtl.Texture {
		t.Helper()
		tex := g.device.NewTextureWithDescriptor(mtl.TextureDescriptor{
			TextureType: mtl.TextureType2D,
			PixelFormat: mtl.PixelFormatBGRA8UNorm,
			Width:       w,
			Height:      h,
			StorageMode: textureStorageMode(g.device),
			Usage:       mtl.TextureUsageRenderTarget | mtl.TextureUsageShaderRead,
		})
		if tex == (mtl.Texture{}) {
			t.Fatalf("NewTextureWithDescriptor(%dx%d) failed", w, h)
		}
		return tex
	}
	tex1 := mkTex(16, 16)
	defer tex1.Release()
	tex2 := mkTex(16, 16)
	defer tex2.Release()
	tex3 := mkTex(32, 32)
	defer tex3.Release()

	s := &Surface{graphics: g}
	defer func() {
		s.mu.Lock()
		s.forgetLastLocked()
		s.mu.Unlock()
	}()

	// Frame 1: normal full draw presented -> becomes last.
	if s.Kept() {
		t.Fatal("initial Kept() = true, want false")
	}
	s.presented(tex1)
	if !s.Kept() {
		t.Fatal("Kept() after presented = false, want true")
	}

	// Window hides before startFrom runs: last is forgotten, startFrom returns false.
	s.mu.Lock()
	s.forgetLastLocked()
	s.mu.Unlock()
	if s.startFrom(tex2, false) {
		t.Fatal("startFrom after forgetLastLocked = true, want false")
	}
	// Presenting that partial frame must not record tex2 as last.
	s.presented(tex2)
	if s.Kept() {
		t.Fatal("Kept() after presenting frame whose keep was lost = true, want false")
	}

	// Next frame draws whole (Kept was false) and becomes last again.
	if s.startFrom(tex2, false) {
		t.Fatal("startFrom when keep is false = true, want false")
	}
	s.presented(tex2)
	if !s.Kept() {
		t.Fatal("Kept() after full redraw = false, want true")
	}

	// Size mismatch while keep is true also drops last on present.
	if s.startFrom(tex3, false) {
		t.Fatal("startFrom with mismatched size = true, want false")
	}
	s.presented(tex3)
	if s.Kept() {
		t.Fatal("Kept() after presenting size-mismatched kept frame = true, want false")
	}
}

// TestSurfaceRecoveryCopiesFullFrame verifies the 3-frame swap-chain recovery sequence:
// 1) kept-but-lost frame leaves tex1 blank,
// 2) recovery frame (Kept() == false) draws tex2 in full while passing a small damage hint,
// 3) next frame acquires tex1 with previous=true and must copy all of tex2 (not only the small hint).
func TestSurfaceRecoveryCopiesFullFrame(t *testing.T) {
	g := newTestGraphics(t)
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	defer g.End(graphicsdriver.FlushModeEndFrame)

	const w, h = 16, 16
	mkTex := func(fill [4]byte) mtl.Texture {
		t.Helper()
		tex := g.device.NewTextureWithDescriptor(mtl.TextureDescriptor{
			TextureType: mtl.TextureType2D,
			PixelFormat: mtl.PixelFormatBGRA8UNorm,
			Width:       w,
			Height:      h,
			StorageMode: textureStorageMode(g.device),
			Usage:       mtl.TextureUsageRenderTarget | mtl.TextureUsageShaderRead,
		})
		if tex == (mtl.Texture{}) {
			t.Fatalf("NewTextureWithDescriptor(%dx%d) failed", w, h)
		}
		pix := make([]byte, 4*w*h)
		for i := 0; i < len(pix); i += 4 {
			copy(pix[i:i+4], fill[:])
		}
		img := &Image{graphics: g, width: w, height: h, format: graphicsdriver.PixelFormatRGBA8, texture: tex}
		if err := img.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: pix, Region: image.Rect(0, 0, w, h)}}); err != nil {
			tex.Release()
			t.Fatal(err)
		}
		return tex
	}

	tex0 := mkTex([4]byte{10, 20, 30, 255})
	defer tex0.Release()
	tex1 := mkTex([4]byte{0, 0, 0, 0}) // represents the partial/cleared kept-but-lost buffer
	defer tex1.Release()
	redBGRA := [4]byte{0, 0, 255, 255}
	tex2 := mkTex(redBGRA) // represents the full recovery frame
	defer tex2.Release()

	s := &Surface{graphics: g}
	defer func() {
		s.mu.Lock()
		s.forgetLastLocked()
		s.mu.Unlock()
	}()

	// Establish initial last.
	s.presented(tex0)
	if !s.Kept() {
		t.Fatal("Kept() after initial frame = false, want true")
	}

	// Frame 1: window hides before startFrom -> keepLost, tex1 is partial/blank, presented drops last.
	s.mu.Lock()
	s.forgetLastLocked()
	s.mu.Unlock()
	if s.startFrom(tex1, false) {
		t.Fatal("frame 1 startFrom = true, want false")
	}
	s.presented(tex1)

	// Frame 2 (recovery): Kept() is false; app draws tex2 in full but supplies a small logical damage hint.
	if s.Kept() {
		t.Fatal("frame 2 Kept() = true, want false")
	}
	s.SetDamage([]image.Rectangle{image.Rect(0, 0, 4, 4)})
	if s.startFrom(tex2, false) {
		t.Fatal("frame 2 startFrom = true, want false")
	}
	s.presented(tex2)
	if s.lastDamage != nil {
		t.Fatalf("recovery frame recorded partial lastDamage = %v, want nil", s.lastDamage)
	}

	// Frame 3: Kept() is true; swap chain reuses tex1 with previous=true. startFrom must copy all of tex2.
	if !s.Kept() {
		t.Fatal("frame 3 Kept() = false, want true")
	}
	if !s.startFrom(tex1, true) {
		t.Fatal("frame 3 startFrom = false, want true")
	}

	// Sync GPU and read back a pixel outside the (0,0)-(4,4) damage hint.
	img1 := &Image{graphics: g, width: w, height: h, format: graphicsdriver.PixelFormatRGBA8, texture: tex1}
	got := make([]byte, 4*w*h)
	if err := img1.ReadPixels([]graphicsdriver.PixelsArgs{{Pixels: got, Region: image.Rect(0, 0, w, h)}}); err != nil {
		t.Fatal(err)
	}
	off := 4 * (10*w + 10)
	if px := [4]byte(got[off : off+4]); px != redBGRA {
		t.Errorf("tex1 pixel (10,10) after recovery copy = %v, want %v (full copy, not partial damage)", px, redBGRA)
	}
}

// TestSurfaceKeepLostSurvivesFlushFalseWithoutRedraw verifies that an incomplete pending drawable
// stays marked incomplete across Flush(false) and a subsequent Kept() query on a frame that does
// not redraw the surface, so a later present still drops last instead of caching the partial buffer.
func TestSurfaceKeepLostSurvivesFlushFalseWithoutRedraw(t *testing.T) {
	g := newTestGraphics(t)
	mkTex := func() mtl.Texture {
		t.Helper()
		tex := g.device.NewTextureWithDescriptor(mtl.TextureDescriptor{
			TextureType: mtl.TextureType2D,
			PixelFormat: mtl.PixelFormatBGRA8UNorm,
			Width:       16,
			Height:      16,
			StorageMode: textureStorageMode(g.device),
			Usage:       mtl.TextureUsageRenderTarget | mtl.TextureUsageShaderRead,
		})
		if tex == (mtl.Texture{}) {
			t.Fatal("NewTextureWithDescriptor failed")
		}
		return tex
	}
	tex0 := mkTex()
	defer tex0.Release()
	tex1 := mkTex()
	defer tex1.Release()

	s := &Surface{graphics: g}
	defer func() {
		s.mu.Lock()
		s.forgetLastLocked()
		s.mu.Unlock()
	}()

	// Frame 0: complete frame presented -> last = tex0.
	s.presented(tex0)
	if !s.Kept() {
		t.Fatal("Kept() after frame 0 = false, want true")
	}

	// Frame 1: window hides before startFrom -> tex1 is marked drawableIncomplete.
	s.mu.Lock()
	s.forgetLastLocked()
	s.mu.Unlock()
	if s.startFrom(tex1, false) {
		t.Fatal("frame 1 startFrom = true, want false")
	}
	// Frame 1 ends with Flush(false): tex1 remains pending (presented not called).

	// Frame 2: queries Kept() (returns false), does NOT redraw this surface, then presents pending tex1.
	if s.Kept() {
		t.Fatal("frame 2 Kept() = true, want false")
	}
	s.presented(tex1)

	// tex1 was incomplete and must NOT have been cached as last.
	if s.Kept() {
		t.Fatal("Kept() after delayed present of incomplete drawable = true, want false")
	}
}

// TestDrawZeroIndicesAndMissingBuffers checks that zero-count draws skip Metal's indexed draw call
// (even when no vertex/index buffer is set), and positive-count draws without SetVertices return
// an error instead of submitting a nil index buffer.
func TestDrawZeroIndicesAndMissingBuffers(t *testing.T) {
	g := newTestGraphics(t)
	prog, err := shader.CompileUser([]byte(`fn fragment(v: Vertex) -> vec4f { return v.color; }`))
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	dst, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Dispose()
	sh, err := g.NewShader(prog.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Dispose()

	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
	u := makeInternalUniforms(prog, 16, 16)
	vers := graphicsdriver.UniformVersions{Internal: 1, User: 1}

	// Zero-count draw with no SetVertices must succeed without calling Metal with nil indexBuffer.
	if err := g.DrawTriangles(dst, srcs, sh, []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 16, 16), IndexCount: 0}}, 0, graphicsdriver.BlendCopy, u, vers); err != nil {
		t.Fatalf("zero-count DrawTriangles without SetVertices failed: %v", err)
	}
	// Positive-count draw with no SetVertices must return an error.
	if err := g.DrawTriangles(dst, srcs, sh, []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 16, 16), IndexCount: 6}}, 0, graphicsdriver.BlendCopy, u, vers); err == nil {
		t.Fatal("positive-count DrawTriangles without SetVertices succeeded, want error")
	}

	// After SetVertices + End(FlushModeIntermediate), buffers are cleared; positive-count draw without
	// a new SetVertices must return an error, while zero-count still succeeds.
	vs, is := fullQuadVertices(16, 16)
	if err := g.SetVertices(vs, is); err != nil {
		t.Fatal(err)
	}
	if err := g.End(graphicsdriver.FlushModeIntermediate); err != nil {
		t.Fatal(err)
	}
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	defer g.End(graphicsdriver.FlushModeEndFrame)
	if err := g.DrawTriangles(dst, srcs, sh, []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 16, 16), IndexCount: 0}}, 0, graphicsdriver.BlendCopy, u, vers); err != nil {
		t.Fatalf("zero-count DrawTriangles after intermediate End failed: %v", err)
	}
	if err := g.DrawTriangles(dst, srcs, sh, []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 16, 16), IndexCount: 6}}, 0, graphicsdriver.BlendCopy, u, vers); err == nil {
		t.Fatal("positive-count DrawTriangles after intermediate End without SetVertices succeeded, want error")
	}
}

// TestDrawSkipsZeroCountRegionInNonZeroDraw verifies that a zero-count region inside a non-zero
// draw is skipped without touching its pixels or shifting indexOffset for subsequent regions.
func TestDrawSkipsZeroCountRegionInNonZeroDraw(t *testing.T) {
	g := newTestGraphics(t)
	prog, err := shader.CompileUser([]byte(`fn fragment(v: Vertex) -> vec4f { return vec4f(0.0, 1.0, 0.0, 1.0); }`))
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	defer g.End(graphicsdriver.FlushModeEndFrame)

	const w, h = 16, 16
	dst, err := g.NewImage(w, h, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Dispose()
	if err := dst.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: make([]byte, 4*w*h), Region: image.Rect(0, 0, w, h)}}); err != nil {
		t.Fatal(err)
	}
	sh, err := g.NewShader(prog.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Dispose()

	vs, is := fullQuadVertices(w, h)
	if err := g.SetVertices(vs, is); err != nil {
		t.Fatal(err)
	}
	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
	u := makeInternalUniforms(prog, w, h)
	regions := []graphicsdriver.DstRegion{
		{Region: image.Rect(0, 0, 8, h), IndexCount: 0},
		{Region: image.Rect(8, 0, w, h), IndexCount: len(is)},
	}
	if err := g.DrawTriangles(dst, srcs, sh, regions, 0, graphicsdriver.BlendCopy, u, graphicsdriver.UniformVersions{Internal: 1, User: 1}); err != nil {
		t.Fatal(err)
	}
	pix := make([]byte, 4*w*h)
	if err := dst.ReadPixels([]graphicsdriver.PixelsArgs{{Pixels: pix, Region: image.Rect(0, 0, w, h)}}); err != nil {
		t.Fatal(err)
	}
	leftOff := 4 * (8*w + 2)
	if got := [4]byte(pix[leftOff : leftOff+4]); got != ([4]byte{0, 0, 0, 0}) {
		t.Errorf("zero-count region pixel = %v, want untouched [0 0 0 0]", got)
	}
	rightOff := 4 * (8*w + 12)
	if got := [4]byte(pix[rightOff : rightOff+4]); got != ([4]byte{0, 255, 0, 255}) {
		t.Errorf("non-zero region pixel = %v, want [0 255 0 255]", got)
	}
}

// TestSurfaceResizeDiscardsPendingDrawableAndInvalidatesKept verifies (a) resizing a Surface while
// a swap-chain drawable is pending after Flush(false) discards the old drawable so texture()
// acquires one at the new size, and (b) disposing a screen image on resize invalidates last so a
// subsequent Kept() returns false even if the first resized frame never drew.
func TestSurfaceResizeDiscardsPendingDrawableAndInvalidatesKept(t *testing.T) {
	g := newTestGraphics(t)
	c, err := newSwapChain(g.device, color.ColorSpaceSRGB)
	if err != nil {
		t.Skipf("IOSurface swap chain unavailable: %v", err)
	}
	s := &Surface{graphics: g}
	s.view.device = g.device
	s.view.pixelFormat = mtl.PixelFormatBGRA8UNorm
	s.view.chain = c
	g.surfacesMu.Lock()
	g.surfaces = append(g.surfaces, s)
	g.surfacesMu.Unlock()
	defer s.Dispose()

	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	scr16, err := s.NewScreenImage(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	tex16 := s.texture()
	if tex16 == (mtl.Texture{}) || tex16.Width() != 16 || tex16.Height() != 16 {
		t.Fatalf("initial texture = %v (%dx%d), want 16x16", tex16, tex16.Width(), tex16.Height())
	}
	// End without presenting: 16x16 swap-chain drawable remains pending.
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}

	// Resize to 32x32 while 16x16 drawable is pending; next texture() must be 32x32.
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	scr32, err := s.NewScreenImage(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	tex32 := s.texture()
	if tex32 == (mtl.Texture{}) || tex32.Width() != 32 || tex32.Height() != 32 {
		t.Fatalf("resized texture = %v (%dx%d), want 32x32", tex32, tex32.Width(), tex32.Height())
	}
	// Present the 32x32 frame so s.last holds 32x32.
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}
	if !s.Kept() {
		t.Fatal("Kept() after presenting 32x32 frame = false, want true")
	}

	// Simulate resize where the old screen image is disposed and the next frame never draws
	// (NewScreenImage is lazy and does not run): Kept() must report false.
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	scr32.Dispose()
	scr16.Dispose()
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}
	if s.Kept() {
		t.Fatal("Kept() after screen image disposed on resize (no draw) = true, want false")
	}
}

// TestSurfaceFromLastSpanningTwoLogicalFramesForcesFullCopy verifies that when a valid fromLast
// pending drawable spans a non-presenting End(FlushModeEndFrame) into a second logical frame,
// presented leaves lastDamage == nil so the alternate swap buffer copies the entire scene and
// does not lose draws from either frame.
func TestSurfaceFromLastSpanningTwoLogicalFramesForcesFullCopy(t *testing.T) {
	g := newTestGraphics(t)
	const w, h = 16, 16
	mkTex := func(fill [4]byte) mtl.Texture {
		t.Helper()
		tex := g.device.NewTextureWithDescriptor(mtl.TextureDescriptor{
			TextureType: mtl.TextureType2D,
			PixelFormat: mtl.PixelFormatBGRA8UNorm,
			Width:       w,
			Height:      h,
			StorageMode: textureStorageMode(g.device),
			Usage:       mtl.TextureUsageRenderTarget | mtl.TextureUsageShaderRead,
		})
		if tex == (mtl.Texture{}) {
			t.Fatal("NewTextureWithDescriptor failed")
		}
		pix := make([]byte, 4*w*h)
		for i := 0; i < len(pix); i += 4 {
			copy(pix[i:i+4], fill[:])
		}
		img := &Image{graphics: g, width: w, height: h, format: graphicsdriver.PixelFormatRGBA8, texture: tex}
		if err := img.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: pix, Region: image.Rect(0, 0, w, h)}}); err != nil {
			tex.Release()
			t.Fatal(err)
		}
		return tex
	}

	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	texA := mkTex([4]byte{10, 10, 10, 255})
	defer texA.Release()
	texB := mkTex([4]byte{0, 0, 0, 0})
	defer texB.Release()

	s := &Surface{graphics: g}
	g.surfacesMu.Lock()
	g.surfaces = append(g.surfaces, s)
	g.surfacesMu.Unlock()
	defer func() {
		s.mu.Lock()
		s.forgetLastLocked()
		s.mu.Unlock()
		g.surfacesMu.Lock()
		g.surfaces = slices.DeleteFunc(g.surfaces, func(t *Surface) bool { return t == s })
		g.surfacesMu.Unlock()
	}()

	// Establish complete last = texA.
	s.presented(texA)
	if !s.Kept() {
		t.Fatal("Kept() = false, want true")
	}

	// Frame 1: acquires texB from last, writes red at (1,1) with small damage rect (0,0)-(4,4),
	// and ends WITHOUT presenting.
	s.chained, s.drawableTex = true, texB
	if !s.startFrom(texB, false) {
		t.Fatal("frame 1 startFrom = false, want true")
	}
	redBGRA := [4]byte{0, 0, 255, 255}
	imgB := &Image{graphics: g, width: w, height: h, format: graphicsdriver.PixelFormatRGBA8, texture: texB}
	if err := imgB.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: redBGRA[:], Region: image.Rect(1, 1, 2, 2)}}); err != nil {
		t.Fatal(err)
	}
	s.SetDamage([]image.Rectangle{image.Rect(0, 0, 4, 4)})
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}

	// Frame 2: draws green at (10,10) with a DISJOINT damage hint (8,8)-(12,12) on the same
	// pending texB, then presents. Without the drawableFromLast->drawableFresh downgrade,
	// presented would record lastDamage = [(8,8)-(12,12)] and Frame 3 would lose Frame 1's (1,1).
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	defer g.End(graphicsdriver.FlushModeEndFrame)
	greenBGRA := [4]byte{0, 255, 0, 255}
	if err := imgB.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: greenBGRA[:], Region: image.Rect(10, 10, 11, 11)}}); err != nil {
		t.Fatal(err)
	}
	s.SetDamage([]image.Rectangle{image.Rect(8, 8, 12, 12)})
	s.presented(texB)
	s.chained, s.drawableTex = false, mtl.Texture{}
	if s.lastDamage != nil {
		t.Fatalf("multi-frame pending drawable recorded partial lastDamage = %v, want nil", s.lastDamage)
	}

	// Frame 3: acquires alternate buffer texA with previous=true; must copy all of texB including
	// both Frame 1's (1,1) and Frame 2's (10,10).
	if !s.Kept() {
		t.Fatal("frame 3 Kept() = false, want true")
	}
	if !s.startFrom(texA, true) {
		t.Fatal("frame 3 startFrom = false, want true")
	}
	imgA := &Image{graphics: g, width: w, height: h, format: graphicsdriver.PixelFormatRGBA8, texture: texA}
	got1 := make([]byte, 4)
	if err := imgA.ReadPixels([]graphicsdriver.PixelsArgs{{Pixels: got1, Region: image.Rect(1, 1, 2, 2)}}); err != nil {
		t.Fatal(err)
	}
	if [4]byte(got1) != redBGRA {
		t.Errorf("texA pixel (1,1) after multi-frame present = %v, want %v", got1, redBGRA)
	}
	got2 := make([]byte, 4)
	if err := imgA.ReadPixels([]graphicsdriver.PixelsArgs{{Pixels: got2, Region: image.Rect(10, 10, 11, 11)}}); err != nil {
		t.Fatal(err)
	}
	if [4]byte(got2) != greenBGRA {
		t.Errorf("texA pixel (10,10) after multi-frame present = %v, want %v", got2, greenBGRA)
	}
}

// TestSurfaceDamageRetiredOnPresentWithoutAcquire checks K2: a surface that supplied SetDamage
// but had no pending drawable at FlushModePresent (both on the no-pending early return and when
// a command buffer exists) has its damage retired instead of leaking into a later frame.
func TestSurfaceDamageRetiredOnPresentWithoutAcquire(t *testing.T) {
	g := newTestGraphics(t)
	s := &Surface{graphics: g}
	g.surfacesMu.Lock()
	g.surfaces = append(g.surfaces, s)
	g.surfacesMu.Unlock()
	defer func() {
		g.surfacesMu.Lock()
		g.surfaces = slices.DeleteFunc(g.surfaces, func(t *Surface) bool { return t == s })
		g.surfacesMu.Unlock()
	}()

	// Case 1: empty command buffer + no pending drawable -> FlushModePresent early return.
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	s.SetDamage([]image.Rectangle{image.Rect(0, 0, 4, 4)})
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	damaged1, n1 := s.damaged, len(s.damage)
	s.mu.Unlock()
	if damaged1 || n1 != 0 {
		t.Errorf("after no-pending FlushModePresent: damaged=%v len=%d, want false, 0", damaged1, n1)
	}

	// Case 2: active command buffer (non-empty work), surface did not acquire -> FlushModePresent.
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	g.ensureCommandBuffer()
	s.SetDamage([]image.Rectangle{image.Rect(4, 4, 8, 8)})
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	damaged2, n2 := s.damaged, len(s.damage)
	s.mu.Unlock()
	if damaged2 || n2 != 0 {
		t.Errorf("after unacquired FlushModePresent with CB: damaged=%v len=%d, want false, 0", damaged2, n2)
	}
}

// TestIntermediateFlushesBoundLiveBuffers checks that many Begin/SetVertices/End(Intermediate)
// cycles reclaim completed command buffers and reuse MTLBuffers instead of accumulating them.
func TestIntermediateFlushesBoundLiveBuffers(t *testing.T) {
	g := newTestGraphics(t)
	vs, is := fullQuadVertices(16, 16)

	const cycles = 2000
	var maxInFlight, maxPooled int
	for range cycles {
		if err := g.Begin(); err != nil {
			t.Fatal(err)
		}
		if err := g.SetVertices(vs, is); err != nil {
			t.Fatal(err)
		}
		if err := g.End(graphicsdriver.FlushModeIntermediate); err != nil {
			t.Fatal(err)
		}
		if n := len(g.inFlight); n > maxInFlight {
			maxInFlight = n
		}
		if n := g.unusedBuffers.len(); n > maxPooled {
			maxPooled = n
		}
	}

	// Wait for any remaining in-flight command buffers and reclaim them.
	for _, r := range g.inFlight {
		for _, cb := range r.cbs {
			if cb.Status() < mtl.CommandBufferStatusCompleted {
				cb.WaitUntilCompleted()
			}
		}
	}
	g.gcBuffers()

	if len(g.inFlight) != 0 {
		t.Errorf("inFlight after completion = %d, want 0", len(g.inFlight))
	}
	if maxInFlight >= cycles/4 {
		t.Errorf("peak inFlight = %d over %d intermediate flushes; command buffers were not reclaimed", maxInFlight, cycles)
	}
	if maxPooled > maxPooledBuffers {
		t.Errorf("peak pooled buffers = %d, want <= %d", maxPooled, maxPooledBuffers)
	}
	if g.unusedBuffers.len() > maxPooledBuffers {
		t.Errorf("final pooled buffers = %d, want <= %d", g.unusedBuffers.len(), maxPooledBuffers)
	}
}
