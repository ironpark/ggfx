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
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gogpu/naga/ir"

	"github.com/ironpark/ggfx/internal/builtinshader"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/remote/fakedriver"
	"github.com/ironpark/ggfx/internal/shader"
)

const screenW, screenH = 203, 117

var testInfo = Info{FlipsY: false, A8: true, MaxImageSize: fakedriver.MaxImageSize}

// userShader has a user uniform block, which a draw's uniforms must be as long as.
const userShader = `
struct Uniforms {
	Tint: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;

fn fragment(v: Vertex) -> vec4f {
	return v.color * u.Tint;
}
`

// uniformsFor returns uniforms for a draw to a destination of w by h, laid out as
// graphicscommand writes them, with the rest filled with something to compare.
func uniformsFor(p *shader.Program, w, h int, seed uint32) []uint32 {
	u := make([]uint32, graphics.PreservedUniformDwordCount+p.UniformDwordCount)
	for i := range u {
		u[i] = seed + uint32(i)
	}
	u[graphics.DstTextureSizeUniformDwordIndex] = math.Float32bits(float32(w))
	u[graphics.DstTextureSizeUniformDwordIndex+1] = math.Float32bits(float32(h))
	m := u[graphics.ProjectionMatrixUniformDwordIndex : graphics.ProjectionMatrixUniformDwordIndex+16]
	clear(m)
	m[0] = math.Float32bits(2 / float32(w))
	m[5] = math.Float32bits(2/float32(h)) | 1<<31
	m[10] = math.Float32bits(1)
	m[12] = math.Float32bits(-1)
	m[13] = math.Float32bits(1)
	m[15] = math.Float32bits(1)
	return u
}

func quads(n int) ([]float32, []uint32) {
	var vs []float32
	var is []uint32
	for q := range n {
		base := uint32(len(vs) / graphics.VertexFloatCount)
		for k := range 4 {
			v := make([]float32, graphics.VertexFloatCount)
			v[0], v[1] = float32(q*10+k%2*8), float32(q*5+k/2*8)
			v[4], v[5], v[6], v[7] = 1, 0.5, 0.25, 1
			vs = append(vs, v...)
		}
		is = append(is, base, base+1, base+2, base+1, base+3, base+2)
	}
	return vs, is
}

// scenario makes on g the calls a frame of an app makes: images in both formats, pixels written
// to them, a built-in and a user shader, draws between images and to the screen, disposals.
func scenario(t testing.TB, g graphicsdriver.Graphics, screen graphicsdriver.Image) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	a, err := g.NewImage(30, 20, graphicsdriver.PixelFormatRGBA8)
	must(err)
	b, err := g.NewImage(17, 9, graphicsdriver.PixelFormatA8)
	must(err)
	pix := make([]byte, 4*30*20)
	for i := range pix {
		pix[i] = byte(i * 7)
	}
	must(a.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: pix, Region: image.Rect(0, 0, 30, 20)}}))
	must(b.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: pix[:3*4], Region: image.Rect(2, 3, 5, 7)}, {Pixels: pix[:1], Region: image.Rect(16, 8, 17, 9)}}))

	builtin, err := shader.CompileUser(builtinshader.ShaderSource(builtinshader.FilterNearest, builtinshader.AddressUnsafe))
	must(err)
	user, err := shader.Compile([]byte(userShader), graphics.ShaderSrcImageCount)
	must(err)
	s1, err := g.NewShader(builtin.Descriptor())
	must(err)
	s2, err := g.NewShader(user.Descriptor())
	must(err)

	vs, is := quads(3)
	must(g.SetVertices(vs, is))
	must(g.DrawTriangles(a, [graphics.ShaderSrcImageCount]graphicsdriver.Image{b}, s1,
		[]graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 30, 20), IndexCount: 6}}, 0,
		graphicsdriver.BlendSourceOver, uniformsFor(builtin, 32, 32, 1), graphicsdriver.UniformVersions{Internal: 1, User: 2}))
	must(g.DrawTriangles(screen, [graphics.ShaderSrcImageCount]graphicsdriver.Image{a}, s1,
		[]graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 100, 50), IndexCount: 6}, {Region: image.Rect(150, 60, screenW, screenH), IndexCount: 0}}, 6,
		graphicsdriver.BlendCopy, uniformsFor(builtin, screenW, screenH, 9), graphicsdriver.UniformVersions{Internal: 3, User: 2}))
	must(g.DrawTriangles(screen, [graphics.ShaderSrcImageCount]graphicsdriver.Image{}, s2,
		[]graphicsdriver.DstRegion{{Region: image.Rect(10, 10, 20, 20), IndexCount: 6}}, 12,
		graphicsdriver.BlendClear, uniformsFor(user, screenW, screenH, 9), graphicsdriver.UniformVersions{Internal: 4, User: 5}))

	vs, is = quads(1)
	must(g.SetVertices(vs, is))
	must(g.DrawTriangles(b, [graphics.ShaderSrcImageCount]graphicsdriver.Image{a}, s1,
		[]graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 17, 9), IndexCount: 6}}, 0,
		graphicsdriver.BlendSourceOver, uniformsFor(builtin, 32, 16, 5), graphicsdriver.UniformVersions{Internal: 6, User: 7}))

	b.Dispose()
	s2.Dispose()
	_ = s1
}

// recorder is a transport that keeps every batch, and answers a read of pixels by replaying what
// came so far on a fake driver.
type recorder struct {
	batches [][]byte
	present []bool

	g      *fakedriver.Graphics
	r      *Replayer
	target *Target
}

func (rec *recorder) Send(batch []byte, present bool) error {
	rec.batches = append(rec.batches, slices.Clone(batch))
	rec.present = append(rec.present, present)
	return nil
}

func (rec *recorder) ReadPixels(batch []byte, id uint32, regions []image.Rectangle, dsts [][]byte) error {
	if _, err := rec.r.Replay(rec.g, batch, rec.target); err != nil {
		return err
	}
	return rec.r.ReadPixels(id, regions, dsts, rec.target)
}

// encode runs the scenario through the remote driver and returns its batch.
func encode(t testing.TB) []byte {
	t.Helper()
	rec := &recorder{}
	g := NewGraphics(testInfo, rec)
	s, err := g.NewSurface(graphicsdriver.SurfaceDescriptor{Kind: graphicsdriver.SurfaceKindRemote})
	if err != nil {
		t.Fatal(err)
	}
	screen, err := s.NewScreenImage(screenW, screenH)
	if err != nil {
		t.Fatal(err)
	}
	s.(*Surface).SetDamage([]image.Rectangle{image.Rect(0, 0, 100, 50), image.Rect(10, 10, 20, 20)})
	scenario(t, g, screen)
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}
	if len(rec.batches) != 1 || !rec.present[0] {
		t.Fatalf("got %d batches, presents %v; want one present", len(rec.batches), rec.present)
	}
	return rec.batches[0]
}

// newTargetForSize builds a Target matching graphics.UnmanagedInternalImageSize unmanaged padding.
func newTargetForSize(g *fakedriver.Graphics, w, h int) *Target {
	maxSize := g.MaxImageSize()
	wp, hp := min(w+1, maxSize), min(h+1, maxSize)
	iw := graphics.UnmanagedInternalImageSize(w, maxSize)
	ih := graphics.UnmanagedInternalImageSize(h, maxSize)
	return &Target{
		Image:          g.NewTarget(wp, hp),
		Width:          w,
		Height:         h,
		InternalWidth:  iw,
		InternalHeight: ih,
	}
}

func newTarget(g *fakedriver.Graphics) *Target {
	return newTargetForSize(g, screenW, screenH)
}

// TestRecordAndReplay checks that a replay makes the very calls the app's driver was given,
// plus zero-initializing each newly created offscreen image's full internal backing texture.
func TestRecordAndReplay(t *testing.T) {
	native := fakedriver.New()
	scenario(t, native, native.NewTarget(screenW, screenH))

	batch := encode(t)
	replayed := fakedriver.New()
	// Use an unpadded target here so retarget() preserves the exact native projection/dstSize
	// uniforms logged by fakedriver; TestRetarget tests padded unmanaged targets.
	target := &Target{
		Image:          replayed.NewTarget(screenW, screenH),
		Width:          screenW,
		Height:         screenH,
		InternalWidth:  screenW,
		InternalHeight: screenH,
	}
	res, err := NewReplayer().Replay(replayed, batch, target)
	if err != nil {
		t.Fatal(err)
	}
	// Replayer clears every newly created image over its full InternalImageSize backing texture
	// right after NewImage so an untrusted app cannot read uninitialized GPU memory.
	var replayedWithoutClears []string
	var clears int
	for i := 0; i < len(replayed.Log); i++ {
		line := replayed.Log[i]
		replayedWithoutClears = append(replayedWithoutClears, line)
		// Skip the Replayer's synthetic full-backing clear right after each non-target NewImage.
		if i > 0 && strings.HasPrefix(line, "NewImage ") && i+1 < len(replayed.Log) && strings.HasPrefix(replayed.Log[i+1], "WritePixels ") {
			clears++
			i++
		}
	}
	if clears != 2 {
		t.Errorf("got %d NewImage clear writes, want 2:\n%s", clears, strings.Join(replayed.Log, "\n"))
	}
	if !slices.Equal(native.Log, replayedWithoutClears) {
		t.Errorf("the replay's calls differ from the app's:\napp:\n%s\nreplay:\n%s", strings.Join(native.Log, "\n"), strings.Join(replayedWithoutClears, "\n"))
	}
	if !res.Presented || res.Draws != 4 || !slices.Equal(res.Damage, []image.Rectangle{image.Rect(0, 0, 100, 50), image.Rect(10, 10, 20, 20)}) {
		t.Errorf("got %+v", res)
	}
	if h, err := ParseHeader(batch); err != nil || h.Width != screenW || h.Height != screenH {
		t.Errorf("ParseHeader: %+v, %v", h, err)
	}
}

// TestRetarget checks that a draw to the screen is rewritten for a target that draws at a larger
// internal size, and that its uniforms then have a version of their own.
func TestRetarget(t *testing.T) {
	batch := encode(t)
	g := fakedriver.New()
	target := newTarget(g)
	target.InternalWidth, target.InternalHeight = 208, 128
	r := NewReplayer()
	if _, err := r.Replay(g, batch, target); err != nil {
		t.Fatal(err)
	}
	u := r.retarget(uniformsFor(&shader.Program{}, screenW, screenH, 9), target)
	m := u[graphics.ProjectionMatrixUniformDwordIndex:]
	if got := math.Float32frombits(m[0]); got != 2.0/208 {
		t.Errorf("projection x: got %v", got)
	}
	if got := math.Float32frombits(m[5]); got != -2.0/128 {
		t.Errorf("projection y: got %v, want the sign kept", got)
	}
	if got := math.Float32frombits(u[graphics.DstTextureSizeUniformDwordIndex]); got != 208 {
		t.Errorf("destination width: got %v", got)
	}
	if r.internalVersion(3, 1, true) == r.internalVersion(3, 1, false) {
		t.Error("a rewritten block shares its version with an unrewritten one")
	}
	if NewReplayer().internalVersion(3, 1, false) == r.internalVersion(3, 1, false) {
		t.Error("two apps share a version")
	}
}

// TestReadPixelsRoundTrip reads back, through the viewer, pixels the app wrote.
func TestReadPixelsRoundTrip(t *testing.T) {
	fake := fakedriver.New()
	rec := &recorder{g: fake, r: NewReplayer()}
	rec.target = newTarget(fake)
	g := NewGraphics(testInfo, rec)

	img, err := g.NewImage(8, 6, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	pix := make([]byte, 4*8*6)
	for i := range pix {
		pix[i] = byte(255 - i)
	}
	if err := img.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: pix, Region: image.Rect(0, 0, 8, 6)}}); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4*3*2)
	if err := img.ReadPixels([]graphicsdriver.PixelsArgs{{Pixels: got, Region: image.Rect(4, 3, 7, 5)}}); err != nil {
		t.Fatal(err)
	}
	for y := range 2 {
		if want := pix[4*((3+y)*8+4) : 4*((3+y)*8+7)]; !bytes.Equal(got[12*y:12*(y+1)], want) {
			t.Errorf("row %d: got %v, want %v", y, got[12*y:12*(y+1)], want)
		}
	}
	// The read shipped the batch so far; the next one starts empty.
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}
	if len(rec.batches) != 0 {
		t.Errorf("an empty frame shipped %d batches", len(rec.batches))
	}
}

// TestDisposeLeavesNothing attaches and detaches many apps and checks that the viewer's driver
// is left with its own target alone.
func TestDisposeLeavesNothing(t *testing.T) {
	batch := encode(t)
	g := fakedriver.New()
	target := newTarget(g)
	for range 100 {
		r := NewReplayer()
		if _, err := r.Replay(g, batch, target); err != nil {
			t.Fatal(err)
		}
		if images, shaders := r.Live(); images != 1 || shaders != 1 {
			t.Fatalf("the app has %d images and %d shaders alive, want 1 and 1", images, shaders)
		}
		r.Dispose()
	}
	if images, shaders := g.Live(); images != 1 || shaders != 0 {
		t.Errorf("%d images and %d shaders alive after the apps went away, want the target alone", images, shaders)
	}
}

func TestVersions(t *testing.T) {
	b, err := testInfo.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var info Info
	if err := info.UnmarshalBinary(b); err != nil || info != testInfo {
		t.Fatalf("got %+v, %v", info, err)
	}
	binary.LittleEndian.PutUint16(b[4:], Version+1)
	var verr *VersionError
	if err := info.UnmarshalBinary(b); !errors.As(err, &verr) || verr.Got != Version+1 {
		t.Errorf("an Info of another version: got %v", err)
	}

	batch := encode(t)
	binary.LittleEndian.PutUint16(batch[4:], Version+1)
	g := fakedriver.New()
	if _, err := NewReplayer().Replay(g, batch, newTarget(g)); !errors.As(err, &verr) {
		t.Errorf("a batch of another version: got %v", err)
	}
}

// replayChecked replays b and fails the test if a call reached the driver that a check should
// have stopped: the fake driver panics on those, which the replay turns into an error of its own.
func replayChecked(t *testing.T, b []byte) error {
	t.Helper()
	g := fakedriver.New()
	_, err := NewReplayer().Replay(g, b, newTarget(g))
	if err != nil && strings.HasPrefix(err.Error(), "remote: replaying the batch:") {
		t.Fatalf("a call got past the checks: %v", err)
	}
	return err
}

func TestTruncatedBatches(t *testing.T) {
	batch := encode(t)
	for n := range len(batch) {
		_ = replayChecked(t, batch[:n])
	}
}

// edit encodes the scenario, changes it with f, and replays it.
func edit(t *testing.T, f func(e *enc)) error {
	t.Helper()
	var e enc
	e.header(Header{Screen: 1, Width: screenW, Height: screenH})
	f(&e)
	return replayChecked(t, e.b)
}

func TestReplayRejects(t *testing.T) {
	tests := map[string]func(e *enc){
		"unknown op": func(e *enc) { e.u8(200) },
		"image too large": func(e *enc) {
			e.u8(opNewImage)
			e.u32(2)
			e.u32(fakedriver.MaxImageSize + 1)
			e.u32(1)
			e.u8(0)
		},
		"unknown format": func(e *enc) {
			e.u8(opNewImage)
			e.u32(2)
			e.u32(1)
			e.u32(1)
			e.u8(9)
		},
		"index past the vertices": func(e *enc) {
			e.u8(opSetVertices)
			e.f32s(make([]float32, graphics.VertexFloatCount))
			e.u32s([]uint32{0, 1, 0})
		},
		"pixels out of the image": func(e *enc) {
			e.u8(opNewImage)
			e.u32(2)
			e.u32(4)
			e.u32(4)
			e.u8(0)
			e.u8(opWritePixels)
			e.u32(2)
			e.u32(1)
			e.rect(image.Rect(10, 10, 20, 20)) // past the texture, 16 by 16
			e.bytes(make([]byte, 4*100))
		},
		"draw to an unknown image": func(e *enc) {
			e.u8(opDrawTriangles)
			e.u32(77)
		},
		"dispose of an unknown shader": func(e *enc) {
			e.u8(opDisposeShader)
			e.u32(5)
		},
		"a huge count": func(e *enc) {
			e.u8(opNewImage)
			e.u32(2)
			e.u32(4)
			e.u32(4)
			e.u8(0)
			e.u8(opWritePixels)
			e.u32(2)
			e.u32(math.MaxUint32)
		},
		"inverted write pixels rect": func(e *enc) {
			e.u8(opNewImage)
			e.u32(2)
			e.u32(4)
			e.u32(4)
			e.u8(0)
			e.u8(opWritePixels)
			e.u32(2)
			e.u32(1)
			e.rect(image.Rectangle{Min: image.Pt(4, 4), Max: image.Pt(0, 0)})
			e.bytes(nil)
		},
		"out of bounds scissor": func(e *enc) {
			encodeTestShaderAndDraw(t, e, true, image.Rect(screenW+10, 0, screenW+10, 10), 0)
		},
		"inverted scissor": func(e *enc) {
			encodeTestShaderAndDraw(t, e, true, image.Rectangle{Min: image.Pt(20, 20), Max: image.Pt(10, 10)}, 6)
		},
		"shader with extra texture binding": func(e *enc) {
			const extraTexShader = `
@group(0) @binding(9) var extra_tex: texture_2d<f32>;
fn fragment(v: Vertex) -> vec4f {
	return textureLoad(extra_tex, vec2i(0, 0), 0);
}
`
			e.u8(opNewShader)
			e.u32(2)
			e.u32(graphics.ShaderSrcImageCount)
			e.bytes([]byte(extraTexShader))
		},
		"shader with extra texture in another group": func(e *enc) {
			const extraGroupTexShader = `
@group(2) @binding(0) var extra_tex: texture_2d<f32>;
fn fragment(v: Vertex) -> vec4f {
	return textureLoad(extra_tex, vec2i(0, 0), 0);
}
`
			e.u8(opNewShader)
			e.u32(2)
			e.u32(graphics.ShaderSrcImageCount)
			e.bytes([]byte(extraGroupTexShader))
		},
		"shader with extra sampler binding": func(e *enc) {
			const extraSamplerShader = `
@group(0) @binding(9) var extra_samp: sampler;
fn fragment(v: Vertex) -> vec4f {
	return textureSample(ggfx_src0, extra_samp, v.src_pos);
}
`
			e.u8(opNewShader)
			e.u32(2)
			e.u32(graphics.ShaderSrcImageCount)
			e.bytes([]byte(extraSamplerShader))
		},
		"shader with storage texture binding": func(e *enc) {
			const storageTexShader = `
@group(0) @binding(9) var extra_storage: texture_storage_2d<rgba8unorm, write>;
fn fragment(v: Vertex) -> vec4f {
	textureStore(extra_storage, vec2i(0, 0), v.color);
	return v.color;
}
`
			e.u8(opNewShader)
			e.u32(2)
			e.u32(graphics.ShaderSrcImageCount)
			e.bytes([]byte(storageTexShader))
		},
	}
	for name, f := range tests {
		t.Run(name, func(t *testing.T) {
			if err := edit(t, f); err == nil {
				t.Error("the replay took it")
			}
		})
	}
}

func encodeTestShaderAndDraw(t *testing.T, e *enc, withSetup bool, region image.Rectangle, indexCount uint32) {
	t.Helper()
	src, err := shader.Compile([]byte(userShader), graphics.ShaderSrcImageCount)
	if err != nil {
		t.Fatal(err)
	}
	if withSetup {
		e.u8(opNewScreenImage)
		e.u32(1)
		e.u32(screenW)
		e.u32(screenH)
		e.u8(opNewShader)
		e.u32(2)
		e.u32(uint32(src.TextureCount))
		e.bytes(src.Source)
		vs, is := quads(1)
		e.u8(opSetVertices)
		e.f32s(vs)
		e.u32s(is)
	}
	e.u8(opDrawTriangles)
	e.u32(1) // screen
	for range graphics.ShaderSrcImageCount {
		e.u32(0)
	}
	e.u32(2) // shader
	e.u32(1) // dstRegions count
	e.rect(region)
	e.u32(indexCount)
	e.u32(0) // indexOffset
	b := graphicsdriver.BlendSourceOver
	e.u8(uint8(b.BlendFactorSourceRGB))
	e.u8(uint8(b.BlendFactorSourceAlpha))
	e.u8(uint8(b.BlendFactorDestinationRGB))
	e.u8(uint8(b.BlendFactorDestinationAlpha))
	e.u8(uint8(b.BlendOperationRGB))
	e.u8(uint8(b.BlendOperationAlpha))
	e.u64(1)
	e.u64(1)
	e.u32s(uniformsFor(src, screenW, screenH, 1))
}

func TestReplayResetsVerticesBetweenBatches(t *testing.T) {
	src, err := shader.Compile([]byte(userShader), graphics.ShaderSrcImageCount)
	if err != nil {
		t.Fatal(err)
	}
	g := fakedriver.New()
	target := newTarget(g)
	r := NewReplayer()

	var b1 enc
	b1.header(Header{Screen: 1, Width: screenW, Height: screenH})
	b1.u8(opNewScreenImage)
	b1.u32(1)
	b1.u32(screenW)
	b1.u32(screenH)
	b1.u8(opNewShader)
	b1.u32(2)
	b1.u32(uint32(src.TextureCount))
	b1.bytes(src.Source)
	vs, is := quads(1)
	b1.u8(opSetVertices)
	b1.f32s(vs)
	b1.u32s(is)
	if _, err := r.Replay(g, b1.b, target); err != nil {
		t.Fatal(err)
	}

	// Second batch draws without setting vertices; must fail rather than reusing batch 1's counts.
	var b2 enc
	b2.header(Header{Screen: 1, Width: screenW, Height: screenH})
	encodeTestShaderAndDraw(t, &b2, false, image.Rect(0, 0, 10, 10), 6)
	if _, err := r.Replay(g, b2.b, target); err == nil {
		t.Error("Replay drew using stale vertices/indices from a previous batch")
	}
}

func TestSingleSurfaceUntilDispose(t *testing.T) {
	rec := &recorder{}
	g := NewGraphics(testInfo, rec)
	desc := graphicsdriver.SurfaceDescriptor{Kind: graphicsdriver.SurfaceKindRemote}
	s1, err := g.NewSurface(desc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.NewSurface(desc); err == nil {
		t.Error("second concurrent NewSurface must fail")
	}
	s1.Dispose()
	s2, err := g.NewSurface(desc)
	if err != nil {
		t.Fatalf("NewSurface after Dispose failed: %v", err)
	}
	s2.Dispose()
}

func TestReadPixelsScreenPaddingRejected(t *testing.T) {
	g := fakedriver.New()
	target := newTarget(g)
	target.InternalWidth, target.InternalHeight = 256, 128
	r := NewReplayer()
	var e enc
	e.header(Header{Screen: 1, Width: screenW, Height: screenH})
	e.u8(opNewScreenImage)
	e.u32(1)
	e.u32(screenW)
	e.u32(screenH)
	if _, err := r.Replay(g, e.b, target); err != nil {
		t.Fatal(err)
	}
	dst := make([]byte, 4)
	if err := r.ReadPixels(1, []image.Rectangle{image.Rect(screenW, 0, screenW+1, 1)}, [][]byte{dst}, target); err == nil {
		t.Error("ReadPixels on screen past logical width must fail")
	}
}

// versionCaptureDriver wraps fakedriver to record UniformVersions and source bindings passed to DrawTriangles.
type versionCaptureDriver struct {
	*fakedriver.Graphics
	seen     []graphicsdriver.UniformVersions
	seenSrcs [][graphics.ShaderSrcImageCount]graphicsdriver.Image
}

func (d *versionCaptureDriver) DrawTriangles(dst graphicsdriver.Image, srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image, sh graphicsdriver.Shader, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32, versions graphicsdriver.UniformVersions) error {
	d.seen = append(d.seen, versions)
	d.seenSrcs = append(d.seenSrcs, srcs)
	return d.Graphics.DrawTriangles(dst, srcs, sh, dstRegions, indexOffset, blend, uniforms, versions)
}

func TestReplayBindsEmptyTextureForUnboundShaderSlot(t *testing.T) {
	const src2Shader = `fn fragment(v: Vertex) -> vec4f { return src2_at(v.src_pos); }`
	prog, err := shader.CompileUser([]byte(src2Shader))
	if err != nil {
		t.Fatal(err)
	}
	var e enc
	e.header(Header{Screen: 1, Width: screenW, Height: screenH})
	e.u8(opNewScreenImage)
	e.u32(1)
	e.u32(screenW)
	e.u32(screenH)
	e.u8(opNewShader)
	e.u32(2)
	e.u32(uint32(prog.TextureCount))
	e.bytes(prog.Source)
	vs, is := quads(1)
	e.u8(opSetVertices)
	e.f32s(vs)
	e.u32s(is)
	e.u8(opDrawTriangles)
	e.u32(1)
	for range graphics.ShaderSrcImageCount {
		e.u32(0) // slot 2 left unbound though shader reads src2
	}
	e.u32(2)
	e.u32(1)
	e.rect(image.Rect(0, 0, 10, 10))
	e.u32(6)
	e.u32(0)
	b := graphicsdriver.BlendSourceOver
	e.u8(uint8(b.BlendFactorSourceRGB))
	e.u8(uint8(b.BlendFactorSourceAlpha))
	e.u8(uint8(b.BlendFactorDestinationRGB))
	e.u8(uint8(b.BlendFactorDestinationAlpha))
	e.u8(uint8(b.BlendOperationRGB))
	e.u8(uint8(b.BlendOperationAlpha))
	e.u64(1)
	e.u64(1)
	e.u32s(uniformsFor(prog, screenW, screenH, 1))

	drv := &versionCaptureDriver{Graphics: fakedriver.New()}
	r := NewReplayer()
	if _, err := r.Replay(drv, e.b, newTarget(drv.Graphics)); err != nil {
		t.Fatal(err)
	}
	if len(drv.seenSrcs) != 1 {
		t.Fatalf("got %d draws, want 1", len(drv.seenSrcs))
	}
	if drv.seenSrcs[0][2] == nil {
		t.Fatal("slot 2 read by shader reached driver as nil")
	}
	if drv.seenSrcs[0][0] != nil || drv.seenSrcs[0][1] != nil || drv.seenSrcs[0][3] != nil {
		t.Errorf("unread slots were unexpectedly populated: %v", drv.seenSrcs[0])
	}
	// Verify the placeholder was sized to InternalImageSize(1)^2 and zero-initialized across its
	// entire backing texture (not only (0,0)).
	side := graphics.InternalImageSize(1)
	wantNew := "NewImage #3 16x16 RGBA8"
	wantWritePrefix := "WritePixels #3 (0,0)-(16,16) "
	var sawNew, sawFullWrite bool
	for _, line := range drv.Log {
		if line == wantNew {
			sawNew = true
		}
		if strings.HasPrefix(line, wantWritePrefix) {
			sawFullWrite = true
		}
	}
	if !sawNew || !sawFullWrite {
		t.Errorf("expected full %dx%d placeholder init (%q and %q), got log:\n%s", side, side, wantNew, wantWritePrefix, strings.Join(drv.Log, "\n"))
	}
	// Read a non-origin texel (side-1, side-1) from the placeholder to verify it is zeroed.
	px := make([]byte, 4)
	if err := drv.seenSrcs[0][2].ReadPixels([]graphicsdriver.PixelsArgs{{Pixels: px, Region: image.Rect(side-1, side-1, side, side)}}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(px, []byte{0, 0, 0, 0}) {
		t.Errorf("placeholder texel (%d,%d) = %v, want [0 0 0 0]", side-1, side-1, px)
	}
	r.Dispose()
	if images, shaders := drv.Live(); images != 1 || shaders != 0 {
		t.Errorf("after Dispose: %d images and %d shaders alive, want target alone (1, 0)", images, shaders)
	}
}

func TestReplayClearsNewImageFullBacking(t *testing.T) {
	// 17x9 has internal size 32x16; verify opNewImage zeroes the full 32x16 backing texture.
	var e enc
	e.header(Header{})
	e.u8(opNewImage)
	e.u32(1)
	e.u32(17)
	e.u32(9)
	e.u8(uint8(graphicsdriver.PixelFormatRGBA8))
	e.u8(opNewImage)
	e.u32(2)
	e.u32(17)
	e.u32(9)
	e.u8(uint8(graphicsdriver.PixelFormatA8))

	g := fakedriver.New()
	r := NewReplayer()
	if _, err := r.Replay(g, e.b, nil); err != nil {
		t.Fatal(err)
	}
	var sawRGBA, sawA8 bool
	for _, line := range g.Log {
		if strings.HasPrefix(line, "WritePixels #1 (0,0)-(32,16) ") {
			sawRGBA = true
		}
		if strings.HasPrefix(line, "WritePixels #2 (0,0)-(32,16) ") {
			sawA8 = true
		}
	}
	if !sawRGBA || !sawA8 {
		t.Errorf("expected full 32x16 clear writes for RGBA8 and A8 images, got log:\n%s", strings.Join(g.Log, "\n"))
	}
	r.Dispose()
}

func TestReplayUniformVersionsIsolatedAcrossShaders(t *testing.T) {
	const bigUserShader = `
struct Uniforms {
	Pad: array<vec4f, 7>,
	Tint: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f { return v.color * u.Tint; }
`
	pSmall, err := shader.CompileUser([]byte(userShader))
	if err != nil {
		t.Fatal(err)
	}
	pBig, err := shader.CompileUser([]byte(bigUserShader))
	if err != nil {
		t.Fatal(err)
	}

	var e enc
	e.header(Header{Screen: 1, Width: screenW, Height: screenH})
	e.u8(opNewScreenImage)
	e.u32(1)
	e.u32(screenW)
	e.u32(screenH)
	e.u8(opNewShader)
	e.u32(2)
	e.u32(uint32(pSmall.TextureCount))
	e.bytes(pSmall.Source)
	e.u8(opNewShader)
	e.u32(3)
	e.u32(uint32(pBig.TextureCount))
	e.bytes(pBig.Source)
	vs, is := quads(1)
	e.u8(opSetVertices)
	e.f32s(vs)
	e.u32s(is)

	emitDraw := func(shID uint32, p *shader.Program, intVer, userVer uint64) {
		e.u8(opDrawTriangles)
		e.u32(1)
		for range graphics.ShaderSrcImageCount {
			e.u32(0)
		}
		e.u32(shID)
		e.u32(1)
		e.rect(image.Rect(0, 0, 10, 10))
		e.u32(6)
		e.u32(0)
		b := graphicsdriver.BlendSourceOver
		e.u8(uint8(b.BlendFactorSourceRGB))
		e.u8(uint8(b.BlendFactorSourceAlpha))
		e.u8(uint8(b.BlendFactorDestinationRGB))
		e.u8(uint8(b.BlendFactorDestinationAlpha))
		e.u8(uint8(b.BlendOperationRGB))
		e.u8(uint8(b.BlendOperationAlpha))
		e.u64(intVer)
		e.u64(userVer)
		e.u32s(uniformsFor(p, screenW, screenH, 1))
	}
	// Draw 0 & 1: same shader (2) and same app versions (3, 7) -> may reuse driver version.
	emitDraw(2, pSmall, 3, 7)
	emitDraw(2, pSmall, 3, 7)
	// Draw 2: different shader (3, 128-byte block) with the SAME app versions (3, 7) -> must get fresh versions.
	emitDraw(3, pBig, 3, 7)
	// Draw 3: back to shader (2) with old app versions (3, 7) -> must get fresh versions (not Draw 0's).
	emitDraw(2, pSmall, 3, 7)

	drv := &versionCaptureDriver{Graphics: fakedriver.New()}
	r := NewReplayer()
	if _, err := r.Replay(drv, e.b, newTarget(drv.Graphics)); err != nil {
		t.Fatal(err)
	}
	if len(drv.seen) != 4 {
		t.Fatalf("got %d draws, want 4", len(drv.seen))
	}
	if drv.seen[0] != drv.seen[1] {
		t.Errorf("consecutive identical (shader, version) draws got different versions: %+v vs %+v", drv.seen[0], drv.seen[1])
	}
	if drv.seen[2].User == drv.seen[1].User {
		t.Errorf("different shaders with same app user version got identical driver User version %d", drv.seen[2].User)
	}
	if drv.seen[2].Internal == drv.seen[1].Internal {
		t.Errorf("different shaders with same app internal version got identical driver Internal version %d", drv.seen[2].Internal)
	}
	if drv.seen[3].User == drv.seen[0].User || drv.seen[3].User == drv.seen[2].User {
		t.Errorf("returning to previous shader with old app user version reused driver User version: %+v", drv.seen)
	}
}

func TestReplayResourceLimits(t *testing.T) {
	t.Run("MaxImages", func(t *testing.T) {
		g := fakedriver.New()
		r := NewReplayer()
		r.SetLimits(Limits{MaxImages: 2})

		var okBatch enc
		okBatch.header(Header{})
		for _, id := range []uint32{1, 2} {
			okBatch.u8(opNewImage)
			okBatch.u32(id)
			okBatch.u32(16)
			okBatch.u32(16)
			okBatch.u8(0)
		}
		if _, err := r.Replay(g, okBatch.b, nil); err != nil {
			t.Fatalf("within MaxImages failed: %v", err)
		}

		var over enc
		over.header(Header{})
		over.u8(opNewImage)
		over.u32(3)
		over.u32(16)
		over.u32(16)
		over.u8(0)
		if _, err := r.Replay(g, over.b, nil); err == nil {
			t.Fatal("exceeding MaxImages succeeded, want error")
		}

		// Disposing and creating in the SAME Replay while already at cap must fail because the
		// disposed GPU texture remains pinned until the GPU retires the frame.
		var sameBatch enc
		sameBatch.header(Header{})
		sameBatch.u8(opDisposeImage)
		sameBatch.u32(1)
		sameBatch.u8(opNewImage)
		sameBatch.u32(3)
		sameBatch.u32(16)
		sameBatch.u32(16)
		sameBatch.u8(0)
		if _, err := r.Replay(g, sameBatch.b, nil); err == nil {
			t.Fatal("DisposeImage + NewImage in the same Replay at MaxImages cap succeeded, want error")
		}

		var nextBatch enc
		nextBatch.header(Header{})
		nextBatch.u8(opNewImage)
		nextBatch.u32(3)
		nextBatch.u32(16)
		nextBatch.u32(16)
		nextBatch.u8(0)
		// Without Retire, the disposed image's charge still holds across Replay calls.
		if _, err := r.Replay(g, nextBatch.b, nil); err == nil {
			t.Fatal("NewImage in next Replay without Retire succeeded, want error")
		}
		// One Retire moves the charge to the previous epoch (still possibly in flight on the GPU).
		r.Retire()
		if _, err := r.Replay(g, nextBatch.b, nil); err == nil {
			t.Fatal("NewImage after only 1 Retire succeeded, want error")
		}
		// Second Retire completes the two-epoch ring and frees the slot.
		r.Retire()
		if _, err := r.Replay(g, nextBatch.b, nil); err != nil {
			t.Fatalf("NewImage after 2 Retire calls failed: %v", err)
		}
		r.Dispose()
	})

	t.Run("MaxPixels", func(t *testing.T) {
		g := fakedriver.New()
		r := NewReplayer()
		// Internal size of 32x32 is 32x32 = 1024 pixels.
		r.SetLimits(Limits{MaxPixels: 2048})

		var okBatch enc
		okBatch.header(Header{})
		for _, id := range []uint32{1, 2} {
			okBatch.u8(opNewImage)
			okBatch.u32(id)
			okBatch.u32(32)
			okBatch.u32(32)
			okBatch.u8(0)
		}
		if _, err := r.Replay(g, okBatch.b, nil); err != nil {
			t.Fatalf("within MaxPixels failed: %v", err)
		}

		var over enc
		over.header(Header{})
		over.u8(opNewImage)
		over.u32(3)
		over.u32(16)
		over.u32(16)
		over.u8(0)
		if _, err := r.Replay(g, over.b, nil); err == nil {
			t.Fatal("exceeding MaxPixels succeeded, want error")
		}

		var disposeBatch enc
		disposeBatch.header(Header{})
		disposeBatch.u8(opDisposeImage)
		disposeBatch.u32(1)
		if _, err := r.Replay(g, disposeBatch.b, nil); err != nil {
			t.Fatalf("DisposeImage failed: %v", err)
		}
		var reuse enc
		reuse.header(Header{})
		reuse.u8(opNewImage)
		reuse.u32(3)
		reuse.u32(32)
		reuse.u32(32)
		reuse.u8(0)
		if _, err := r.Replay(g, reuse.b, nil); err == nil {
			t.Fatal("NewImage without Retire succeeded, want error")
		}
		r.Retire()
		if _, err := r.Replay(g, reuse.b, nil); err == nil {
			t.Fatal("NewImage after 1 Retire succeeded, want error")
		}
		r.Retire()
		if _, err := r.Replay(g, reuse.b, nil); err != nil {
			t.Fatalf("NewImage after 2 Retire calls failed: %v", err)
		}
		r.Dispose()
	})

	t.Run("MaxShaders", func(t *testing.T) {
		g := fakedriver.New()
		r := NewReplayer()
		r.SetLimits(Limits{MaxShaders: 2})

		var okBatch enc
		okBatch.header(Header{})
		for _, id := range []uint32{1, 2} {
			okBatch.u8(opNewShader)
			okBatch.u32(id)
			okBatch.u32(graphics.ShaderSrcImageCount)
			okBatch.bytes([]byte(userShader))
		}
		if _, err := r.Replay(g, okBatch.b, nil); err != nil {
			t.Fatalf("within MaxShaders failed: %v", err)
		}

		var over enc
		over.header(Header{})
		over.u8(opNewShader)
		over.u32(3)
		over.u32(graphics.ShaderSrcImageCount)
		over.bytes([]byte(userShader))
		if _, err := r.Replay(g, over.b, nil); err == nil {
			t.Fatal("exceeding MaxShaders succeeded, want error")
		}

		var disposeBatch enc
		disposeBatch.header(Header{})
		disposeBatch.u8(opDisposeShader)
		disposeBatch.u32(1)
		if _, err := r.Replay(g, disposeBatch.b, nil); err != nil {
			t.Fatalf("DisposeShader failed: %v", err)
		}
		var reuse enc
		reuse.header(Header{})
		reuse.u8(opNewShader)
		reuse.u32(3)
		reuse.u32(graphics.ShaderSrcImageCount)
		reuse.bytes([]byte(userShader))
		if _, err := r.Replay(g, reuse.b, nil); err == nil {
			t.Fatal("NewShader without Retire succeeded, want error")
		}
		r.Retire()
		if _, err := r.Replay(g, reuse.b, nil); err == nil {
			t.Fatal("NewShader after 1 Retire succeeded, want error")
		}
		r.Retire()
		if _, err := r.Replay(g, reuse.b, nil); err != nil {
			t.Fatalf("NewShader after 2 Retire calls failed: %v", err)
		}
		r.Dispose()
	})

	t.Run("MaxShaderSourceBytes", func(t *testing.T) {
		g := fakedriver.New()
		r := NewReplayer()
		srcLen := len(userShader)
		r.SetLimits(Limits{MaxShaderSourceBytes: srcLen + 10})

		var okBatch enc
		okBatch.header(Header{})
		okBatch.u8(opNewShader)
		okBatch.u32(1)
		okBatch.u32(graphics.ShaderSrcImageCount)
		okBatch.bytes([]byte(userShader))
		if _, err := r.Replay(g, okBatch.b, nil); err != nil {
			t.Fatalf("within MaxShaderSourceBytes failed: %v", err)
		}

		var over enc
		over.header(Header{})
		over.u8(opNewShader)
		over.u32(2)
		over.u32(graphics.ShaderSrcImageCount)
		over.bytes([]byte(userShader))
		if _, err := r.Replay(g, over.b, nil); err == nil {
			t.Fatal("exceeding MaxShaderSourceBytes succeeded, want error")
		}

		var disposeBatch enc
		disposeBatch.header(Header{})
		disposeBatch.u8(opDisposeShader)
		disposeBatch.u32(1)
		if _, err := r.Replay(g, disposeBatch.b, nil); err != nil {
			t.Fatalf("DisposeShader failed: %v", err)
		}
		var reuse enc
		reuse.header(Header{})
		reuse.u8(opNewShader)
		reuse.u32(2)
		reuse.u32(graphics.ShaderSrcImageCount)
		reuse.bytes([]byte(userShader))
		if _, err := r.Replay(g, reuse.b, nil); err == nil {
			t.Fatal("NewShader without Retire succeeded, want error")
		}
		r.Retire()
		if _, err := r.Replay(g, reuse.b, nil); err == nil {
			t.Fatal("NewShader after 1 Retire succeeded, want error")
		}
		r.Retire()
		if _, err := r.Replay(g, reuse.b, nil); err != nil {
			t.Fatalf("NewShader after 2 Retire calls failed: %v", err)
		}
		r.Dispose()
	})

	t.Run("MaxUploadBytes", func(t *testing.T) {
		g := fakedriver.New()
		r := NewReplayer()
		// 16x16 synthetic clear = 1024 bytes; one 16x16 WritePixels = 1024 bytes -> 2048 total.
		r.SetLimits(Limits{MaxUploadBytes: 2048})

		var create enc
		create.header(Header{})
		create.u8(opNewImage)
		create.u32(1)
		create.u32(16)
		create.u32(16)
		create.u8(0)
		if _, err := r.Replay(g, create.b, nil); err != nil {
			t.Fatalf("NewImage within MaxUploadBytes failed: %v", err)
		}

		var write enc
		write.header(Header{})
		write.u8(opWritePixels)
		write.u32(1)
		write.u32(1) // 1 region
		write.u32(0)
		write.u32(0)
		write.u32(16)
		write.u32(16)
		write.bytes(make([]byte, 16*16*4))
		if _, err := r.Replay(g, write.b, nil); err != nil {
			t.Fatalf("WritePixels hitting exact MaxUploadBytes failed: %v", err)
		}

		// Any further upload (even 1 pixel) must fail until two Retire calls pass.
		var smallWrite enc
		smallWrite.header(Header{})
		smallWrite.u8(opWritePixels)
		smallWrite.u32(1)
		smallWrite.u32(1)
		smallWrite.u32(0)
		smallWrite.u32(0)
		smallWrite.u32(1)
		smallWrite.u32(1)
		smallWrite.bytes(make([]byte, 4))
		if _, err := r.Replay(g, smallWrite.b, nil); err == nil {
			t.Fatal("WritePixels exceeding MaxUploadBytes succeeded, want error")
		}
		// Another opNewImage (synthetic clear = 1024 bytes) must also fail.
		var secondImg enc
		secondImg.header(Header{})
		secondImg.u8(opNewImage)
		secondImg.u32(2)
		secondImg.u32(16)
		secondImg.u32(16)
		secondImg.u8(0)
		if _, err := r.Replay(g, secondImg.b, nil); err == nil {
			t.Fatal("NewImage synthetic clear exceeding MaxUploadBytes succeeded, want error")
		}
		r.Retire()
		if _, err := r.Replay(g, smallWrite.b, nil); err == nil {
			t.Fatal("WritePixels after 1 Retire succeeded, want error")
		}
		r.Retire()
		if _, err := r.Replay(g, smallWrite.b, nil); err != nil {
			t.Fatalf("WritePixels after 2 Retire calls failed: %v", err)
		}
		r.Dispose()
	})

	t.Run("MaxPipelines", func(t *testing.T) {
		g := fakedriver.New()
		r := NewReplayer()
		r.SetLimits(Limits{MaxPipelines: 2})

		pUser, err := shader.CompileUser([]byte(userShader))
		if err != nil {
			t.Fatal(err)
		}

		var setup enc
		setup.header(Header{})
		setup.u8(opNewImage)
		setup.u32(1)
		setup.u32(16)
		setup.u32(16)
		setup.u8(0)
		setup.u8(opNewShader)
		setup.u32(10)
		setup.u32(uint32(pUser.TextureCount))
		setup.bytes(pUser.Source)
		if _, err := r.Replay(g, setup.b, nil); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		vs, is := quads(1)
		drawWithBlend := func(shaderID uint32, blend graphicsdriver.Blend) []byte {
			var b enc
			b.header(Header{})
			b.u8(opSetVertices)
			b.f32s(vs)
			b.u32s(is)
			b.u8(opDrawTriangles)
			b.u32(1) // dst
			for range graphics.ShaderSrcImageCount {
				b.u32(0) // src
			}
			b.u32(shaderID)
			b.u32(1) // 1 region
			b.rect(image.Rect(0, 0, 16, 16))
			b.u32(uint32(len(is)))
			b.u32(0)
			b.u8(uint8(blend.BlendFactorSourceRGB))
			b.u8(uint8(blend.BlendFactorSourceAlpha))
			b.u8(uint8(blend.BlendFactorDestinationRGB))
			b.u8(uint8(blend.BlendFactorDestinationAlpha))
			b.u8(uint8(blend.BlendOperationRGB))
			b.u8(uint8(blend.BlendOperationAlpha))
			b.u64(1) // internal version
			b.u64(1) // user version
			b.u32s(uniformsFor(pUser, 16, 16, 1))
			return b.b
		}

		// First pipeline (BlendCopy).
		if _, err := r.Replay(g, drawWithBlend(10, graphicsdriver.BlendCopy), nil); err != nil {
			t.Fatalf("pipeline 1 failed: %v", err)
		}
		// Repeating the same (shader, blend, format) must not consume another pipeline slot.
		if _, err := r.Replay(g, drawWithBlend(10, graphicsdriver.BlendCopy), nil); err != nil {
			t.Fatalf("repeat of pipeline 1 failed: %v", err)
		}
		// Second distinct pipeline (BlendSourceOver).
		if _, err := r.Replay(g, drawWithBlend(10, graphicsdriver.BlendSourceOver), nil); err != nil {
			t.Fatalf("pipeline 2 failed: %v", err)
		}
		// Third distinct pipeline (BlendLighter) exceeds MaxPipelines = 2.
		if _, err := r.Replay(g, drawWithBlend(10, graphicsdriver.BlendLighter), nil); err == nil {
			t.Fatal("pipeline 3 exceeding MaxPipelines succeeded, want error")
		}

		// Dispose shader 10 and create shader 11; pipeline charges release only after 2 Retire calls.
		var swap enc
		swap.header(Header{})
		swap.u8(opDisposeShader)
		swap.u32(10)
		swap.u8(opNewShader)
		swap.u32(11)
		swap.u32(uint32(pUser.TextureCount))
		swap.bytes(pUser.Source)
		if _, err := r.Replay(g, swap.b, nil); err != nil {
			t.Fatalf("swap shader failed: %v", err)
		}
		if _, err := r.Replay(g, drawWithBlend(11, graphicsdriver.BlendCopy), nil); err == nil {
			t.Fatal("new pipeline without Retire succeeded, want error")
		}
		r.Retire()
		if _, err := r.Replay(g, drawWithBlend(11, graphicsdriver.BlendCopy), nil); err == nil {
			t.Fatal("new pipeline after 1 Retire succeeded, want error")
		}
		r.Retire()
		if _, err := r.Replay(g, drawWithBlend(11, graphicsdriver.BlendCopy), nil); err != nil {
			t.Fatalf("new pipeline after 2 Retire calls failed: %v", err)
		}
		r.Dispose()
	})

	t.Run("DeferredReleaseLoopBoundedAcrossReplays", func(t *testing.T) {
		g := fakedriver.New()
		g.DeferReleases = true
		r := NewReplayer()
		// Allow at most 3 images and 3*32*32 = 3072 pixels pinned across a frame.
		r.SetLimits(Limits{MaxImages: 3, MaxPixels: 3 * 32 * 32})

		// Subcase A: single Replay create->dispose loop fails at cap.
		var loopBatch enc
		loopBatch.header(Header{})
		for range 10 {
			loopBatch.u8(opNewImage)
			loopBatch.u32(1)
			loopBatch.u32(32)
			loopBatch.u32(32)
			loopBatch.u8(0)
			loopBatch.u8(opDisposeImage)
			loopBatch.u32(1)
		}
		if _, err := r.Replay(g, loopBatch.b, nil); err == nil {
			t.Fatal("single Replay create->dispose loop exceeding limits succeeded, want error")
		}
		peakImgs, _, peakPx := g.PeakPinned()
		if peakImgs > 3 || peakPx > 3*32*32 {
			t.Errorf("single-Replay deferred peak (%d images, %d px), want <= (3, %d)", peakImgs, peakPx, 3*32*32)
		}

		// Intermediate flush does NOT retire deferred releases on the driver.
		_ = g.End(graphicsdriver.FlushModeIntermediate)
		if imgs, _, px := g.Pinned(); imgs == 0 || px == 0 {
			t.Fatalf("FlushModeIntermediate unexpectedly retired deferred releases: pinned = (%d, %d)", imgs, px)
		}

		// Retire GPU frame and two host epochs; fresh Replays within limits now succeed.
		_ = g.End(graphicsdriver.FlushModeEndFrame)
		if imgs, _, px := g.Pinned(); imgs != 0 || px != 0 {
			t.Fatalf("after final End: pinned = (%d, %d), want (0, 0)", imgs, px)
		}
		r.Retire()
		r.Retire()

		// Subcase B: many small Replay calls in one host frame (with intermediate flushes, no Retire)
		// must still hit the cap and keep pinned GPU memory bounded.
		var oneChurn enc
		oneChurn.header(Header{})
		oneChurn.u8(opNewImage)
		oneChurn.u32(1)
		oneChurn.u32(32)
		oneChurn.u32(32)
		oneChurn.u8(0)
		oneChurn.u8(opDisposeImage)
		oneChurn.u32(1)

		hitErr := false
		for i := range 10 {
			_, err := r.Replay(g, oneChurn.b, nil)
			_ = g.End(graphicsdriver.FlushModeIntermediate)
			if err != nil {
				hitErr = true
				if i != 3 {
					t.Errorf("multi-Replay churn failed on iteration %d, want 3", i)
				}
				break
			}
		}
		if !hitErr {
			t.Fatal("10 small create->dispose Replay calls without Retire all succeeded, want error")
		}
		peakImgs, _, peakPx = g.PeakPinned()
		if peakImgs > 3 || peakPx > 3*32*32 {
			t.Errorf("multi-Replay deferred peak (%d images, %d px), want <= (3, %d)", peakImgs, peakPx, 3*32*32)
		}

		// Final End + 2 Retire calls frees everything.
		_ = g.End(graphicsdriver.FlushModeEndFrame)
		r.Retire()
		r.Retire()

		// Subcase C: shader + source churn in one Replay and across multiple Replays.
		r.SetLimits(Limits{
			MaxShaders:           2,
			MaxShaderSourceBytes: 2 * len(userShader),
		})
		var shaderChurn enc
		shaderChurn.header(Header{})
		shaderChurn.u8(opNewShader)
		shaderChurn.u32(1)
		shaderChurn.u32(graphics.ShaderSrcImageCount)
		shaderChurn.bytes([]byte(userShader))
		shaderChurn.u8(opDisposeShader)
		shaderChurn.u32(1)

		var shaderLoop enc
		shaderLoop.header(Header{})
		for range 5 {
			shaderLoop.u8(opNewShader)
			shaderLoop.u32(1)
			shaderLoop.u32(graphics.ShaderSrcImageCount)
			shaderLoop.bytes([]byte(userShader))
			shaderLoop.u8(opDisposeShader)
			shaderLoop.u32(1)
		}
		if _, err := r.Replay(g, shaderLoop.b, nil); err == nil {
			t.Fatal("single-Replay shader churn exceeding MaxShaders succeeded, want error")
		}
		_, peakShaders, _ := g.PeakPinned()
		if peakShaders > 2 {
			t.Errorf("deferred peak shaders = %d, want <= 2", peakShaders)
		}
		_ = g.End(graphicsdriver.FlushModeEndFrame)
		r.Retire()
		r.Retire()

		hitShaderErr := false
		for i := range 5 {
			_, err := r.Replay(g, shaderChurn.b, nil)
			_ = g.End(graphicsdriver.FlushModeIntermediate)
			if err != nil {
				hitShaderErr = true
				if i != 2 {
					t.Errorf("multi-Replay shader churn failed on iteration %d, want 2", i)
				}
				break
			}
		}
		if !hitShaderErr {
			t.Fatal("multi-Replay shader churn without Retire all succeeded, want error")
		}
		_, peakShaders, _ = g.PeakPinned()
		if peakShaders > 2 {
			t.Errorf("multi-Replay deferred peak shaders = %d, want <= 2", peakShaders)
		}
		r.Dispose()
		_ = g.End(graphicsdriver.FlushModeEndFrame)
	})

	t.Run("ScreenSizePreflightAndBudget", func(t *testing.T) {
		g := fakedriver.New()
		r := NewReplayer()

		// 16x16 logical unmanaged screen has 16+1 -> 32x32 = 1024 backing pixels;
		// a 256-pixel cap must reject it (cannot pass based on logical 16x16 = 256).
		rTiny := NewReplayer()
		rTiny.SetLimits(Limits{MaxPixels: 256})
		var s16 enc
		s16.header(Header{Screen: 1, Width: 16, Height: 16})
		if _, _, err := rTiny.ScreenSize(s16.b, g.MaxImageSize()); err == nil {
			t.Fatal("ScreenSize accepted 16x16 screen (32x32=1024 backing px) under MaxPixels=256")
		}

		// Overflow safety: MaxInt32 x MaxInt32 with maxImageSize=0 must return error cleanly.
		var overflowScreen enc
		overflowScreen.header(Header{Screen: 1, Width: math.MaxInt32, Height: math.MaxInt32})
		if _, _, err := rTiny.ScreenSize(overflowScreen.b, 0); err == nil {
			t.Fatal("ScreenSize accepted MaxInt32xMaxInt32 with maxImageSize=0")
		}

		// Budget fits a 32x32 unmanaged screen (48x48 = 2304 backing px) + 32x32 offscreen (1024 px) = 3328 px.
		r.SetLimits(Limits{MaxPixels: 48*48 + 32*32})

		// Non-multiple-of-16 maxImageSize: logical 96x16 <= 100, but rounded backing 112x32 > 100.
		var nonMult16 enc
		nonMult16.header(Header{Screen: 1, Width: 96, Height: 16})
		if _, _, err := r.ScreenSize(nonMult16.b, 100); err == nil {
			t.Fatal("ScreenSize accepted 96x16 screen whose rounded backing width 112 exceeds maxImageSize 100")
		}

		// Oversize screen exceeding MaxPixels rejected by preflight and by Replay.
		var bigScreen enc
		bigScreen.header(Header{Screen: 1, Width: 64, Height: 64})
		bigScreen.u8(opNewScreenImage)
		bigScreen.u32(1)
		bigScreen.u32(64)
		bigScreen.u32(64)
		if _, _, err := r.ScreenSize(bigScreen.b, g.MaxImageSize()); err == nil {
			t.Fatal("ScreenSize accepted screen exceeding MaxPixels")
		}
		bigTarget := newTargetForSize(g, 64, 64)
		if _, err := r.Replay(g, bigScreen.b, bigTarget); err == nil {
			t.Fatal("Replay accepted screen exceeding MaxPixels when preflight was skipped")
		}
		bigTarget.Image.Dispose()

		// Oversize screen exceeding device maxImageSize rejected by preflight and by Replay.
		var hugeScreen enc
		hugeScreen.header(Header{Screen: 1, Width: fakedriver.MaxImageSize + 1, Height: 1})
		if _, _, err := r.ScreenSize(hugeScreen.b, g.MaxImageSize()); err == nil {
			t.Fatal("ScreenSize accepted screen exceeding maxImageSize")
		}
		rHuge := NewReplayer()
		rHuge.SetLimits(Limits{MaxPixels: 1 << 30})
		fakeBigTarget := &Target{Image: g.NewTarget(16, 16), Width: fakedriver.MaxImageSize + 1, Height: 1, InternalWidth: fakedriver.MaxImageSize + 16, InternalHeight: 16}
		if _, err := rHuge.Replay(g, hugeScreen.b, fakeBigTarget); err == nil {
			t.Fatal("Replay accepted screen exceeding g.MaxImageSize()")
		}
		fakeBigTarget.Image.Dispose()

		// Invalid Target (InternalWidth < Width) rejected by Replay.
		badTarget := &Target{Image: g.NewTarget(16, 16), Width: 32, Height: 32, InternalWidth: 16, InternalHeight: 32}
		var okBatch enc
		okBatch.header(Header{Screen: 1, Width: 32, Height: 32})
		okBatch.u8(opNewScreenImage)
		okBatch.u32(1)
		okBatch.u32(32)
		okBatch.u32(32)
		okBatch.u8(opNewImage)
		okBatch.u32(2)
		okBatch.u32(32)
		okBatch.u32(32)
		okBatch.u8(0)
		if _, err := r.Replay(g, okBatch.b, badTarget); err == nil {
			t.Fatal("Replay accepted Target with InternalWidth < Width")
		}
		badTarget.Image.Dispose()

		// Screen (32x32 -> 48x48 = 2304 px) + offscreen image (32x32 = 1024 px) fits exactly.
		w, h, err := r.ScreenSize(okBatch.b, g.MaxImageSize())
		if err != nil || w != 32 || h != 32 {
			t.Fatalf("ScreenSize(32x32) = (%d, %d, %v), want (32, 32, nil)", w, h, err)
		}
		t32 := newTargetForSize(g, 32, 32)
		if _, err := r.Replay(g, okBatch.b, t32); err != nil {
			t.Fatalf("Replay(32x32 screen + 32x32 image) failed: %v", err)
		}

		// End-of-batch header claiming an unknown screen ID must be rejected.
		var badHeader enc
		badHeader.header(Header{Screen: 99, Width: 32, Height: 32})
		if _, err := r.Replay(g, badHeader.b, t32); err == nil {
			t.Fatal("Replay accepted header naming an inactive screen ID")
		}

		// opPresent targeting an offscreen image ID must be rejected.
		var badPresent enc
		badPresent.header(Header{Screen: 1, Width: 32, Height: 32})
		badPresent.u8(opPresent)
		badPresent.u32(2) // offscreen image, not active screen
		badPresent.u32(allDamaged)
		if _, err := r.Replay(g, badPresent.b, t32); err == nil {
			t.Fatal("Replay accepted opPresent on offscreen image ID")
		}

		// A malformed no-screen batch while screen 1 is still active must NOT drop screen 1's pixel
		// charge during prefix opNewImage allocations before the end-of-batch header check runs.
		var bogusNoScreen enc
		bogusNoScreen.header(Header{})
		bogusNoScreen.u8(opNewImage)
		bogusNoScreen.u32(3)
		bogusNoScreen.u32(16)
		bogusNoScreen.u32(16)
		bogusNoScreen.u8(0)
		liveBefore, _ := g.Live()
		if _, err := r.Replay(g, bogusNoScreen.b, nil); err == nil {
			t.Fatal("Replay accepted no-screen batch allocating past MaxPixels while screen 1 was active")
		}
		if liveAfter, _ := g.Live(); liveAfter != liveBefore {
			t.Errorf("driver live images changed from %d to %d during rejected Screen=0 prefix", liveBefore, liveAfter)
		}

		// Adding another 16x16 offscreen image pushes screen + offscreen over MaxPixels.
		var overImg enc
		overImg.header(Header{Screen: 1, Width: 32, Height: 32})
		overImg.u8(opNewImage)
		overImg.u32(3)
		overImg.u32(16)
		overImg.u32(16)
		overImg.u8(0)
		if _, err := r.Replay(g, overImg.b, t32); err == nil {
			t.Fatal("Replay accepted offscreen image pushing screen + images over MaxPixels")
		}
		t32.Image.Dispose()

		// Resizing to a smaller screen (16x16 -> 32x32 = 1024 px) frees 1280 px, allowing a 32x32 (1024 px) image.
		var shrink enc
		shrink.header(Header{Screen: 4, Width: 16, Height: 16})
		shrink.u8(opDisposeImage)
		shrink.u32(1)
		shrink.u8(opNewScreenImage)
		shrink.u32(4)
		shrink.u32(16)
		shrink.u32(16)
		shrink.u8(opNewImage)
		shrink.u32(5)
		shrink.u32(32)
		shrink.u32(32)
		shrink.u8(0)
		w, h, err = r.ScreenSize(shrink.b, g.MaxImageSize())
		if err != nil || w != 16 || h != 16 {
			t.Fatalf("ScreenSize(16x16) = (%d, %d, %v), want (16, 16, nil)", w, h, err)
		}
		t16 := newTargetForSize(g, 16, 16)
		if _, err := r.Replay(g, shrink.b, t16); err != nil {
			t.Fatalf("Replay after shrinking screen failed: %v", err)
		}

		// Create a second screen ID 6 of the same size (16x16) WITHOUT disposing screen 4: screen 4
		// remains live and same-sized, yet inactive, so opPresent on screen 4 must be rejected.
		var replaceLive enc
		replaceLive.header(Header{Screen: 6, Width: 16, Height: 16})
		replaceLive.u8(opNewScreenImage)
		replaceLive.u32(6)
		replaceLive.u32(16)
		replaceLive.u32(16)
		if _, err := r.Replay(g, replaceLive.b, t16); err != nil {
			t.Fatalf("creating replacement screen 6 failed: %v", err)
		}
		var inactivePresent enc
		inactivePresent.header(Header{Screen: 6, Width: 16, Height: 16})
		inactivePresent.u8(opPresent)
		inactivePresent.u32(4) // still live and 16x16, but no longer activeScreenID
		inactivePresent.u32(allDamaged)
		if _, err := r.Replay(g, inactivePresent.b, t16); err == nil {
			t.Fatal("Replay accepted opPresent on still-live inactive screen ID of matching size")
		}
		t16.Image.Dispose()
		r.Dispose()
	})
}

func TestSetDamageEmptySlice(t *testing.T) {
	rec := &recorder{}
	g := NewGraphics(testInfo, rec)
	s, err := g.NewSurface(graphicsdriver.SurfaceDescriptor{Kind: graphicsdriver.SurfaceKindRemote})
	if err != nil {
		t.Fatal(err)
	}
	screen, err := s.NewScreenImage(screenW, screenH)
	if err != nil {
		t.Fatal(err)
	}
	user, err := shader.Compile([]byte(userShader), graphics.ShaderSrcImageCount)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := g.NewShader(user.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	vs, is := quads(1)

	drawOne := func() {
		if err := g.SetVertices(vs, is); err != nil {
			t.Fatal(err)
		}
		if err := g.DrawTriangles(screen, [graphics.ShaderSrcImageCount]graphicsdriver.Image{}, sh,
			[]graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 10, 10), IndexCount: 6}}, 0,
			graphicsdriver.BlendCopy, uniformsFor(user, screenW, screenH, 1), graphicsdriver.UniformVersions{Internal: 1, User: 1}); err != nil {
			t.Fatal(err)
		}
	}

	// Frame 1: SetDamage with an empty non-nil slice on a fresh Graphics (where g.damage is nil).
	s.(*Surface).SetDamage([]image.Rectangle{})
	drawOne()
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}

	// Frame 2: SetDamage(nil) means all damaged.
	s.(*Surface).SetDamage(nil)
	drawOne()
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}

	// Frame 3 (idle): SetDamage(empty) with no screen draw, ending in FlushModePresent; must
	// retire the empty damage so Frame 4 (which does not call SetDamage) sends allDamaged.
	s.(*Surface).SetDamage([]image.Rectangle{})
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}
	// Frame 4: draws to screen without calling SetDamage -> must default to all damaged (nil).
	drawOne()
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}

	// Frame 5: intermediate flush within a frame must preserve SetDamage(empty) for the final present.
	s.(*Surface).SetDamage([]image.Rectangle{})
	if err := g.End(graphicsdriver.FlushModeIntermediate); err != nil {
		t.Fatal(err)
	}
	drawOne()
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}

	if len(rec.batches) != 4 {
		t.Fatalf("got %d batches, want 4", len(rec.batches))
	}
	fake := fakedriver.New()
	target := newTarget(fake)
	r := NewReplayer()
	res1, err := r.Replay(fake, rec.batches[0], target)
	if err != nil {
		t.Fatal(err)
	}
	if !res1.Presented || res1.Damage == nil || len(res1.Damage) != 0 {
		t.Errorf("empty non-nil SetDamage: got Presented=%v Damage=%#v, want Presented=true non-nil empty Damage", res1.Presented, res1.Damage)
	}
	res2, err := r.Replay(fake, rec.batches[1], target)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Presented || res2.Damage != nil {
		t.Errorf("nil SetDamage: got Presented=%v Damage=%#v, want Presented=true nil Damage", res2.Presented, res2.Damage)
	}
	res4, err := r.Replay(fake, rec.batches[2], target)
	if err != nil {
		t.Fatal(err)
	}
	if !res4.Presented || res4.Damage != nil {
		t.Errorf("frame after idle SetDamage(empty): got Presented=%v Damage=%#v, want Presented=true nil Damage", res4.Presented, res4.Damage)
	}
	res5, err := r.Replay(fake, rec.batches[3], target)
	if err != nil {
		t.Fatal(err)
	}
	if !res5.Presented || res5.Damage == nil || len(res5.Damage) != 0 {
		t.Errorf("SetDamage(empty) across intermediate flush: got Presented=%v Damage=%#v, want Presented=true non-nil empty Damage", res5.Presented, res5.Damage)
	}
}

func TestReplayZeroCountDrawWithoutSetVertices(t *testing.T) {
	prog, err := shader.Compile([]byte(userShader), graphics.ShaderSrcImageCount)
	if err != nil {
		t.Fatal(err)
	}
	drv := &versionCaptureDriver{Graphics: fakedriver.New()}
	target := newTarget(drv.Graphics)
	r := NewReplayer()
	defer r.Dispose()

	// Setup screen and shader, but emit NO SetVertices.
	var setup enc
	setup.header(Header{Screen: 1, Width: screenW, Height: screenH})
	setup.u8(opNewScreenImage)
	setup.u32(1)
	setup.u32(screenW)
	setup.u32(screenH)
	setup.u8(opNewShader)
	setup.u32(2)
	setup.u32(uint32(prog.TextureCount))
	setup.bytes(prog.Source)
	if _, err := r.Replay(drv, setup.b, target); err != nil {
		t.Fatal(err)
	}

	// Valid all-zero-count draw without SetVertices must short-circuit without calling DrawTriangles.
	var zeroDraw enc
	zeroDraw.header(Header{Screen: 1, Width: screenW, Height: screenH})
	encodeTestShaderAndDraw(t, &zeroDraw, false, image.Rect(0, 0, 10, 10), 0)
	res, err := r.Replay(drv, zeroDraw.b, target)
	if err != nil {
		t.Fatalf("valid zero-count draw failed: %v", err)
	}
	if res.Draws != 0 || len(drv.seen) != 0 {
		t.Errorf("zero-count draw was not short-circuited: res.Draws=%d, driver draws=%d", res.Draws, len(drv.seen))
	}

	// Invalid all-zero-count draw (out-of-bounds scissor) must still be rejected.
	var badZeroDraw enc
	badZeroDraw.header(Header{Screen: 1, Width: screenW, Height: screenH})
	encodeTestShaderAndDraw(t, &badZeroDraw, false, image.Rect(screenW+1, 0, screenW+5, 5), 0)
	if _, err := r.Replay(drv, badZeroDraw.b, target); err == nil {
		t.Fatal("invalid zero-count draw with out-of-bounds scissor was accepted")
	}

	// Non-zero-count draw without SetVertices must be rejected.
	var posDraw enc
	posDraw.header(Header{Screen: 1, Width: screenW, Height: screenH})
	encodeTestShaderAndDraw(t, &posDraw, false, image.Rect(0, 0, 10, 10), 6)
	if _, err := r.Replay(drv, posDraw.b, target); err == nil {
		t.Fatal("non-zero-count draw without SetVertices was accepted")
	}
}

// TestDrawTrianglesAllSourcesAndAsymmetricBlend round-trips a DrawTriangles call with four
// distinct source images, distinct RGB vs alpha blend factors and operations, multiple
// DstRegions, and a non-zero indexOffset to catch any field swap in the direct-write encoder.
func TestDrawTrianglesAllSourcesAndAsymmetricBlend(t *testing.T) {
	const fourSrcShader = `
fn fragment(v: Vertex) -> vec4f {
	return src0_at(v.src_pos) + src1_at(v.src_pos) + src2_at(v.src_pos) + src3_at(v.src_pos);
}
`
	prog, err := shader.CompileUser([]byte(fourSrcShader))
	if err != nil {
		t.Fatal(err)
	}
	blend := graphicsdriver.Blend{
		BlendFactorSourceRGB:        graphicsdriver.BlendFactorSourceColor,
		BlendFactorSourceAlpha:      graphicsdriver.BlendFactorOneMinusDestinationAlpha,
		BlendFactorDestinationRGB:   graphicsdriver.BlendFactorDestinationColor,
		BlendFactorDestinationAlpha: graphicsdriver.BlendFactorSourceAlphaSaturated,
		BlendOperationRGB:           graphicsdriver.BlendOperationSubtract,
		BlendOperationAlpha:         graphicsdriver.BlendOperationReverseSubtract,
	}
	run := func(g graphicsdriver.Graphics) {
		t.Helper()
		dst, err := g.NewImage(32, 32, graphicsdriver.PixelFormatRGBA8)
		if err != nil {
			t.Fatal(err)
		}
		var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
		for i := range srcs {
			img, err := g.NewImage(16+i, 16+i, graphicsdriver.PixelFormatRGBA8)
			if err != nil {
				t.Fatal(err)
			}
			srcs[i] = img
		}
		sh, err := g.NewShader(prog.Descriptor())
		if err != nil {
			t.Fatal(err)
		}
		vs, is := quads(4)
		if err := g.SetVertices(vs, is); err != nil {
			t.Fatal(err)
		}
		regions := []graphicsdriver.DstRegion{
			{Region: image.Rect(1, 2, 10, 12), IndexCount: 6},
			{Region: image.Rect(13, 14, 25, 28), IndexCount: 12},
		}
		if err := g.DrawTriangles(dst, srcs, sh, regions, 6, blend, uniformsFor(prog, 32, 32, 7), graphicsdriver.UniformVersions{Internal: 11, User: 13}); err != nil {
			t.Fatal(err)
		}
	}

	native := fakedriver.New()
	run(native)

	rec := &recorder{}
	rg := NewGraphics(testInfo, rec)
	run(rg)
	if err := rg.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}
	if len(rec.batches) != 1 {
		t.Fatalf("got %d batches, want 1", len(rec.batches))
	}

	replayed := fakedriver.New()
	r := NewReplayer()
	if _, err := r.Replay(replayed, rec.batches[0], nil); err != nil {
		t.Fatal(err)
	}
	r.Dispose()

	var got []string
	for i := 0; i < len(replayed.Log); i++ {
		line := replayed.Log[i]
		if strings.HasPrefix(line, "Dispose") {
			continue
		}
		got = append(got, line)
		if strings.HasPrefix(line, "NewImage ") && i+1 < len(replayed.Log) && strings.HasPrefix(replayed.Log[i+1], "WritePixels ") {
			i++
		}
	}
	if !slices.Equal(native.Log, got) {
		t.Errorf("four-source asymmetric-blend replay differs:\nnative:\n%s\nreplayed:\n%s", strings.Join(native.Log, "\n"), strings.Join(got, "\n"))
	}
}

// TestSetDamageCoalescedAndOffscreenSchedules verifies conservative damage accumulation when
// multiple logical frames coalesce into fewer final flushes (six screen frames, three final
// flushes) and when an offscreen-only frame runs between two screen frames, ensuring emitted
// damage always covers every coalesced frame and no driver state grows.
func TestSetDamageCoalescedAndOffscreenSchedules(t *testing.T) {
	rec := &recorder{}
	g := NewGraphics(testInfo, rec)
	s, err := g.NewSurface(graphicsdriver.SurfaceDescriptor{Kind: graphicsdriver.SurfaceKindRemote})
	if err != nil {
		t.Fatal(err)
	}
	surf := s.(*Surface)
	screen, err := s.NewScreenImage(screenW, screenH)
	if err != nil {
		t.Fatal(err)
	}
	offscreen, err := g.NewImage(32, 32, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	user, err := shader.Compile([]byte(userShader), graphics.ShaderSrcImageCount)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := g.NewShader(user.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	vs, is := quads(1)
	drawTo := func(dst graphicsdriver.Image, w, h int) {
		if err := g.SetVertices(vs, is); err != nil {
			t.Fatal(err)
		}
		if err := g.DrawTriangles(dst, [graphics.ShaderSrcImageCount]graphicsdriver.Image{}, sh,
			[]graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 10, 10), IndexCount: 6}}, 0,
			graphicsdriver.BlendCopy, uniformsFor(user, w, h, 1), graphicsdriver.UniformVersions{Internal: 1, User: 1}); err != nil {
			t.Fatal(err)
		}
	}

	// Schedule A: six screen frames coalesced into three final flushes (2 frames per flush).
	r1 := image.Rect(0, 0, 10, 10)
	r2 := image.Rect(20, 20, 30, 30)
	r3 := image.Rect(40, 40, 50, 50)
	r6 := image.Rect(60, 60, 70, 70)

	// Flush 1 covers Frame 1 (r1) + Frame 2 (r2): emitted damage must cover both r1 and r2.
	_ = surf.Kept()
	surf.SetDamage([]image.Rectangle{r1})
	drawTo(screen, screenW, screenH)
	_ = surf.Kept()
	surf.SetDamage([]image.Rectangle{r2})
	drawTo(screen, screenW, screenH)
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}

	// Flush 2 covers Frame 3 (r3) + Frame 4 (no SetDamage => whole screen): must emit allDamaged (nil).
	_ = surf.Kept()
	surf.SetDamage([]image.Rectangle{r3})
	drawTo(screen, screenW, screenH)
	_ = surf.Kept()
	drawTo(screen, screenW, screenH)
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}

	// Flush 3 covers Frame 5 (no SetDamage => whole screen) + Frame 6 (r6): must emit allDamaged (nil).
	_ = surf.Kept()
	drawTo(screen, screenW, screenH)
	_ = surf.Kept()
	surf.SetDamage([]image.Rectangle{r6})
	drawTo(screen, screenW, screenH)
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}

	// Schedule B: offscreen-only frame between two screen frames.
	r7 := image.Rect(5, 5, 15, 15)
	r8 := image.Rect(75, 10, 95, 30)
	_ = surf.Kept()
	surf.SetDamage([]image.Rectangle{r7})
	drawTo(screen, screenW, screenH)
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}
	// Offscreen-only frame: no Kept, no SetDamage, final FlushModeEndFrame.
	drawTo(offscreen, 32, 32)
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}
	// Next screen frame: must emit [r8], unaffected by the offscreen frame.
	_ = surf.Kept()
	surf.SetDamage([]image.Rectangle{r8})
	drawTo(screen, screenW, screenH)
	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}

	// No damage state may be left behind or growing.
	g.damageM.Lock()
	leftLen, leftCap := len(g.damage), cap(g.damage)
	leftFlags := g.damaged || g.damageAll || g.pendingKept
	g.damageM.Unlock()
	if leftLen != 0 || leftCap > 64 || leftFlags {
		t.Errorf("residual damage state after final flushes: len=%d cap=%d flags=%v", leftLen, leftCap, leftFlags)
	}

	fake := fakedriver.New()
	target := newTarget(fake)
	r := NewReplayer()
	defer r.Dispose()
	var presents []Result
	for _, b := range rec.batches {
		res, err := r.Replay(fake, b, target)
		if err != nil {
			t.Fatal(err)
		}
		if res.Presented {
			presents = append(presents, res)
		}
	}
	if len(presents) != 5 {
		t.Fatalf("got %d presents, want 5", len(presents))
	}
	if !slices.Equal(presents[0].Damage, []image.Rectangle{r1, r2}) {
		t.Errorf("Flush 1 (frames 1+2) damage = %v, want [%v %v]", presents[0].Damage, r1, r2)
	}
	if presents[1].Damage != nil {
		t.Errorf("Flush 2 (frames 3+4, frame 4 default) damage = %v, want nil (allDamaged)", presents[1].Damage)
	}
	if presents[2].Damage != nil {
		t.Errorf("Flush 3 (frames 5+6, frame 5 default) damage = %v, want nil (allDamaged)", presents[2].Damage)
	}
	if !slices.Equal(presents[3].Damage, []image.Rectangle{r7}) {
		t.Errorf("Screen frame before offscreen damage = %v, want [%v]", presents[3].Damage, r7)
	}
	if !slices.Equal(presents[4].Damage, []image.Rectangle{r8}) {
		t.Errorf("Screen frame after offscreen damage = %v, want [%v]", presents[4].Damage, r8)
	}
}

type failingWriteImage struct {
	graphicsdriver.Image
	err      error
	panicMsg string
	writes   []image.Rectangle
}

func (f *failingWriteImage) WritePixels(args []graphicsdriver.PixelsArgs) error {
	for _, a := range args {
		f.writes = append(f.writes, a.Region)
	}
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	if f.err != nil {
		return f.err
	}
	return f.Image.WritePixels(args)
}

type failingWriteDriver struct {
	*fakedriver.Graphics
	err      error
	panicMsg string
	lastImg  *failingWriteImage
}

func (d *failingWriteDriver) NewImage(w, h int, format graphicsdriver.PixelFormat) (graphicsdriver.Image, error) {
	img, err := d.Graphics.NewImage(w, h, format)
	if err != nil {
		return nil, err
	}
	fi := &failingWriteImage{Image: img, err: d.err, panicMsg: d.panicMsg}
	d.lastImg = fi
	return fi, nil
}

// TestClearImageMultiStripAndFailureSafety covers multi-strip clearImage, clear error disposal,
// and panic mid-clear disposal with consistent Replayer accounting.
func TestClearImageMultiStripAndFailureSafety(t *testing.T) {
	t.Run("MultiStrip", func(t *testing.T) {
		g := fakedriver.New()
		// 1024x128 RGBA8 = 512 KiB = 2 strips of 64 rows over the 256 KiB zeroPixels buffer.
		img, err := g.NewImage(1024, 128, graphicsdriver.PixelFormatRGBA8)
		if err != nil {
			t.Fatal(err)
		}
		fi := &failingWriteImage{Image: img}
		if err := clearImage(fi, 1024, 128, 4); err != nil {
			t.Fatal(err)
		}
		want := []image.Rectangle{image.Rect(0, 0, 1024, 64), image.Rect(0, 64, 1024, 128)}
		if !slices.Equal(fi.writes, want) {
			t.Errorf("multi-strip clearImage regions = %v, want %v", fi.writes, want)
		}
		img.Dispose()
	})

	t.Run("ClearErrorDisposesNativeImage", func(t *testing.T) {
		drv := &failingWriteDriver{Graphics: fakedriver.New(), err: errors.New("upload failed")}
		r := NewReplayer()
		var e enc
		e.header(Header{})
		e.u8(opNewImage)
		e.u32(1)
		e.u32(16)
		e.u32(16)
		e.u8(0)
		if _, err := r.Replay(drv, e.b, nil); err == nil {
			t.Fatal("opNewImage with failing clear succeeded, want error")
		}
		if imgs, shaders := drv.Live(); imgs != 0 || shaders != 0 {
			t.Errorf("after clear error: driver Live() = (%d, %d), want (0, 0)", imgs, shaders)
		}
		if imgs, shaders := r.Live(); imgs != 0 || shaders != 0 || r.livePixels != 0 {
			t.Errorf("after clear error: Replayer live = (%d, %d, %d px), want all 0", imgs, shaders, r.livePixels)
		}
	})

	t.Run("ClearPanicDisposesNativeImageAndKeepsAccountingConsistent", func(t *testing.T) {
		drv := &failingWriteDriver{Graphics: fakedriver.New(), panicMsg: "driver boom"}
		r := NewReplayer()
		var e enc
		e.header(Header{})
		e.u8(opNewImage)
		e.u32(1)
		e.u32(16)
		e.u32(16)
		e.u8(0)
		if _, err := r.Replay(drv, e.b, nil); err == nil {
			t.Fatal("opNewImage with panicking clear succeeded, want recovered error")
		}
		if imgs, shaders := drv.Live(); imgs != 0 || shaders != 0 {
			t.Errorf("after clear panic: driver Live() = (%d, %d), want (0, 0)", imgs, shaders)
		}
		if imgs, shaders := r.Live(); imgs != 0 || shaders != 0 || r.livePixels != 0 {
			t.Errorf("after clear panic: Replayer live = (%d, %d, %d px), want all 0", imgs, shaders, r.livePixels)
		}
		if r.pendingImages() != 1 || r.pendingPixels() != 16*16 || r.epochUploadBytes() != 16*16*4 {
			t.Errorf("after clear panic: retained epoch = (%d imgs, %d px, %d upload bytes), want (1, %d, %d)",
				r.pendingImages(), r.pendingPixels(), r.epochUploadBytes(), 16*16, 16*16*4)
		}
		r.Retire()
		r.Retire()
		if r.pendingImages() != 0 || r.pendingPixels() != 0 || r.epochUploadBytes() != 0 {
			t.Errorf("after 2 Retire calls: retained epoch = (%d imgs, %d px, %d upload bytes), want all 0",
				r.pendingImages(), r.pendingPixels(), r.epochUploadBytes())
		}
	})
}

// TestValidateShaderGlobalsDirectIR exercises validateShaderGlobals directly on hand-constructed
// naga IR modules, independent of shader.Compile.
func TestValidateShaderGlobalsDirectIR(t *testing.T) {
	tex2DFloat := ir.Type{Inner: ir.ImageType{Dim: ir.Dim2D, Class: ir.ImageClassSampled, SampledKind: ir.ScalarFloat}}
	tex3DFloat := ir.Type{Inner: ir.ImageType{Dim: ir.Dim3D, Class: ir.ImageClassSampled, SampledKind: ir.ScalarFloat}}
	tex2DUint := ir.Type{Inner: ir.ImageType{Dim: ir.Dim2D, Class: ir.ImageClassSampled, SampledKind: ir.ScalarUint}}
	texStorage := ir.Type{Inner: ir.ImageType{Dim: ir.Dim2D, Class: ir.ImageClassStorage}}
	samplerType := ir.Type{Inner: ir.SamplerType{}}
	scalarType := ir.Type{Inner: ir.ScalarType{Kind: ir.ScalarFloat, Width: 4}}

	b := func(g, slot uint32) *ir.ResourceBinding { return &ir.ResourceBinding{Group: g, Binding: slot} }
	intBind := shader.InternalUniformBinding
	userBind := shader.UserUniformBinding
	src0Bind := shader.TextureBinding(0)
	src2Bind := shader.TextureBinding(2)

	validProg := &shader.Program{
		TextureCount: graphics.ShaderSrcImageCount,
		Module: &ir.Module{
			Types: []ir.Type{scalarType, tex2DFloat},
			GlobalVariables: []ir.GlobalVariable{
				{Name: "ggfx_internal", Space: ir.SpaceUniform, Binding: &intBind, Type: 0},
				{Name: "u", Space: ir.SpaceUniform, Binding: &userBind, Type: 0},
				{Name: "ggfx_src0", Space: ir.SpaceHandle, Binding: &src0Bind, Type: 1},
				{Name: "ggfx_src2", Space: ir.SpaceHandle, Binding: &src2Bind, Type: 1},
				{Name: "ggfx_front_facing", Space: ir.SpacePrivate, Type: 0},
			},
		},
	}
	mask, err := validateShaderGlobals(validProg)
	if err != nil {
		t.Fatalf("valid IR rejected: %v", err)
	}
	if want := uint8((1 << 0) | (1 << 2)); mask != want {
		t.Errorf("valid IR mask = %#x, want %#x", mask, want)
	}

	rejects := map[string]*shader.Program{
		"nil program": nil,
		"nil module":  {TextureCount: 4},
		"extra handle binding": {
			TextureCount: 4,
			Module: &ir.Module{
				Types:           []ir.Type{tex2DFloat},
				GlobalVariables: []ir.GlobalVariable{{Name: "extra", Space: ir.SpaceHandle, Binding: b(0, 9), Type: 0}},
			},
		},
		"sampler at texture slot": {
			TextureCount: 4,
			Module: &ir.Module{
				Types:           []ir.Type{samplerType},
				GlobalVariables: []ir.GlobalVariable{{Name: "samp", Space: ir.SpaceHandle, Binding: &src0Bind, Type: 0}},
			},
		},
		"3D texture at texture slot": {
			TextureCount: 4,
			Module: &ir.Module{
				Types:           []ir.Type{tex3DFloat},
				GlobalVariables: []ir.GlobalVariable{{Name: "t3d", Space: ir.SpaceHandle, Binding: &src0Bind, Type: 0}},
			},
		},
		"uint texture at texture slot": {
			TextureCount: 4,
			Module: &ir.Module{
				Types:           []ir.Type{tex2DUint},
				GlobalVariables: []ir.GlobalVariable{{Name: "tu32", Space: ir.SpaceHandle, Binding: &src0Bind, Type: 0}},
			},
		},
		"storage texture at texture slot": {
			TextureCount: 4,
			Module: &ir.Module{
				Types:           []ir.Type{texStorage},
				GlobalVariables: []ir.GlobalVariable{{Name: "st", Space: ir.SpaceHandle, Binding: &src0Bind, Type: 0}},
			},
		},
		"duplicate handle slot": {
			TextureCount: 4,
			Module: &ir.Module{
				Types: []ir.Type{tex2DFloat},
				GlobalVariables: []ir.GlobalVariable{
					{Name: "a", Space: ir.SpaceHandle, Binding: &src0Bind, Type: 0},
					{Name: "b", Space: ir.SpaceHandle, Binding: &src0Bind, Type: 0},
				},
			},
		},
		"extra uniform binding": {
			TextureCount: 4,
			Module: &ir.Module{
				Types:           []ir.Type{scalarType},
				GlobalVariables: []ir.GlobalVariable{{Name: "u2", Space: ir.SpaceUniform, Binding: b(2, 0), Type: 0}},
			},
		},
		"duplicate internal uniform": {
			TextureCount: 4,
			Module: &ir.Module{
				Types: []ir.Type{scalarType},
				GlobalVariables: []ir.GlobalVariable{
					{Name: "a", Space: ir.SpaceUniform, Binding: &intBind, Type: 0},
					{Name: "b", Space: ir.SpaceUniform, Binding: &intBind, Type: 0},
				},
			},
		},
		"private global with binding": {
			TextureCount: 4,
			Module: &ir.Module{
				Types:           []ir.Type{scalarType},
				GlobalVariables: []ir.GlobalVariable{{Name: "p", Space: ir.SpacePrivate, Binding: b(0, 9), Type: 0}},
			},
		},
		"storage address space": {
			TextureCount: 4,
			Module: &ir.Module{
				Types:           []ir.Type{scalarType},
				GlobalVariables: []ir.GlobalVariable{{Name: "buf", Space: ir.SpaceStorage, Binding: b(1, 0), Type: 0}},
			},
		},
		"immediate address space": {
			TextureCount: 4,
			Module: &ir.Module{
				Types:           []ir.Type{scalarType},
				GlobalVariables: []ir.GlobalVariable{{Name: "imm", Space: ir.SpaceImmediate, Type: 0}},
			},
		},
	}
	for name, prog := range rejects {
		t.Run(name, func(t *testing.T) {
			if _, err := validateShaderGlobals(prog); err == nil {
				t.Errorf("validateShaderGlobals(%s) succeeded, want error", name)
			}
		})
	}
}

func TestReplayerRetireConcurrent(t *testing.T) {
	g := fakedriver.New()
	r := NewReplayer()
	var batch enc
	batch.header(Header{})
	batch.u8(opNewImage)
	batch.u32(1)
	batch.u32(16)
	batch.u32(16)
	batch.u8(0)
	batch.u8(opDisposeImage)
	batch.u32(1)

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				r.Retire()
				_ = r.Limits()
				_, _ = r.Live()
			}
		}
	}()
	for range 50 {
		if _, err := r.Replay(g, batch.b, nil); err != nil {
			t.Fatalf("concurrent Replay failed: %v", err)
		}
	}
	r.Dispose()
	close(done)
	wg.Wait()
}

// TestReplayRejectsOversizedUntrustedUniformBlock verifies that Replay compiles remote shader
// sources via shader.CompileUntrusted: a 16,400-byte user uniform block compiles locally through
// shader.Compile but is rejected when replayed.
func TestReplayRejectsOversizedUntrustedUniformBlock(t *testing.T) {
	// 1025 * 16 = 16,400 bytes (> 16 KiB untrusted user-uniform limit).
	const bigUniformShader = `
struct Uniforms {
	Data: array<vec4f, 1025>,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f { return v.color * u.Data[0]; }
`
	if _, err := shader.Compile([]byte(bigUniformShader), graphics.ShaderSrcImageCount); err != nil {
		t.Fatalf("local shader.Compile unexpectedly rejected 16,400-byte uniform block: %v", err)
	}

	var e enc
	e.header(Header{})
	e.u8(opNewShader)
	e.u32(1)
	e.u32(graphics.ShaderSrcImageCount)
	e.bytes([]byte(bigUniformShader))

	g := fakedriver.New()
	r := NewReplayer()
	defer r.Dispose()
	if _, err := r.Replay(g, e.b, nil); err == nil {
		t.Fatal("Replay accepted shader with 16,400-byte user uniform block, want error")
	}
	if _, shaders := g.Live(); shaders != 0 {
		t.Errorf("driver live shaders = %d after rejected shader, want 0", shaders)
	}
}

func FuzzReplay(f *testing.F) {
	f.Add(encode(f))
	var e enc
	e.header(Header{})
	f.Add(e.b)
	f.Fuzz(func(t *testing.T, b []byte) {
		_ = replayChecked(t, b)
	})
}
