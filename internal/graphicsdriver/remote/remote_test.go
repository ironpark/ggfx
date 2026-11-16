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
	"testing"

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

	builtin, err := graphics.CompileShader(builtinshader.ShaderSource(builtinshader.FilterNearest, builtinshader.AddressUnsafe))
	must(err)
	user, err := shader.Compile([]byte(userShader), graphics.ShaderSrcImageCount)
	must(err)
	s1, err := g.NewShader(builtin)
	must(err)
	s2, err := g.NewShader(user)
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
		graphicsdriver.BlendClear, uniformsFor(user, screenW, screenH, 9), graphicsdriver.UniformVersions{Internal: 3, User: 4}))

	vs, is = quads(1)
	must(g.SetVertices(vs, is))
	must(g.DrawTriangles(b, [graphics.ShaderSrcImageCount]graphicsdriver.Image{a}, s1,
		[]graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 17, 9), IndexCount: 6}}, 0,
		graphicsdriver.BlendSourceOver, uniformsFor(builtin, 32, 16, 5), graphicsdriver.UniformVersions{Internal: 5, User: 2}))

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
	s, err := g.NewSurface(SurfaceTarget{}, false)
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

func newTarget(g *fakedriver.Graphics) *Target {
	return &Target{Image: g.NewTarget(screenW, screenH), Width: screenW, Height: screenH, InternalWidth: screenW, InternalHeight: screenH}
}

// TestRecordAndReplay checks that a replay makes the very calls the app's driver was given.
func TestRecordAndReplay(t *testing.T) {
	native := fakedriver.New()
	scenario(t, native, native.NewTarget(screenW, screenH))

	batch := encode(t)
	replayed := fakedriver.New()
	target := newTarget(replayed)
	res, err := NewReplayer().Replay(replayed, batch, target)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(native.Log, replayed.Log) {
		t.Errorf("the replay's calls differ from the app's:\napp:\n%s\nreplay:\n%s", strings.Join(native.Log, "\n"), strings.Join(replayed.Log, "\n"))
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
	if _, err := NewReplayer().Replay(g, batch, target); err != nil {
		t.Fatal(err)
	}
	u := retarget(uniformsFor(&shader.Program{}, screenW, screenH, 9), target)
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
	r := NewReplayer()
	if r.version(3, true) == r.version(3, false) {
		t.Error("a rewritten block shares its version with the app's")
	}
	if NewReplayer().version(3, false) == r.version(3, false) {
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
	}
	for name, f := range tests {
		t.Run(name, func(t *testing.T) {
			if err := edit(t, f); err == nil {
				t.Error("the replay took it")
			}
		})
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
