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

//go:build darwin

package remote

import (
	"bytes"
	"image"
	"math"
	"runtime"
	"testing"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal"
	"github.com/ironpark/ggfx/internal/shader"
)

// TestReplayEmptyPlaceholderNonOriginTexelMetal verifies on a real Metal GPU that sampling a
// non-origin texel (15, 15) from an unbound source slot reads transparent black from the fully
// zero-initialized placeholder, and that a freshly created unwritten image also reads transparent
// black at a non-origin padding texel.
func TestReplayEmptyPlaceholderNonOriginTexelMetal(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	g, err := metal.NewGraphics(color.ColorSpaceSRGB)
	if err != nil {
		t.Skipf("no Metal device: %v", err)
	}
	if err := g.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	defer g.End(graphicsdriver.FlushModeEndFrame)

	const side = 16
	internalSide := graphics.UnmanagedInternalImageSize(side, g.MaxImageSize())
	targetImg, err := g.NewImage(side+1, side+1, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	defer targetImg.Dispose()
	// Pre-fill target with opaque red so BlendCopy must visibly overwrite it with (0,0,0,0).
	red := make([]byte, 4*side*side)
	for i := 0; i < len(red); i += 4 {
		red[i], red[i+3] = 255, 255
	}
	if err := targetImg.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: red, Region: image.Rect(0, 0, side, side)}}); err != nil {
		t.Fatal(err)
	}

	// Shader reads texel (15, 15) of src2 (unbound -> placeholder) plus texel (15, 15) of src0
	// (freshly created 1x1 unwritten image whose backing texture is 16x16).
	const probeShader = `
fn fragment(v: Vertex) -> vec4f {
	return src2_unsafe_at(vec2f(15.0, 15.0)) + src0_unsafe_at(vec2f(15.0, 15.0));
}
`
	prog, err := shader.CompileUser([]byte(probeShader))
	if err != nil {
		t.Fatal(err)
	}

	var e enc
	e.header(Header{Screen: 1, Width: side, Height: side})
	e.u8(opNewScreenImage)
	e.u32(1)
	e.u32(side)
	e.u32(side)
	e.u8(opNewImage)
	e.u32(2)
	e.u32(1)
	e.u32(1)
	e.u8(uint8(graphicsdriver.PixelFormatRGBA8))
	e.u8(opNewShader)
	e.u32(3)
	e.u32(uint32(prog.TextureCount))
	e.bytes(prog.Source)

	vs := make([]float32, 4*graphics.VertexFloatCount)
	graphics.QuadVerticesFromDstAndSrc(vs, 0, 0, side, side, 0, 0, side, side, 1, 1, 1, 1)
	is := graphics.QuadIndices()
	e.u8(opSetVertices)
	e.f32s(vs)
	e.u32s(is)

	u := uniformsFor(prog, side, side, 1)
	// FlipsY on Metal matches uniformsFor's -2/h sign, ensure projection is valid for side x side.
	m := u[graphics.ProjectionMatrixUniformDwordIndex:]
	m[0] = math.Float32bits(2 / float32(side))
	m[5] = math.Float32bits(-2 / float32(side))

	e.u8(opDrawTriangles)
	e.u32(1) // screen
	e.u32(2) // src0 = unwritten 1x1 image (16x16 backing)
	e.u32(0)
	e.u32(0) // src2 = unbound -> placeholder
	e.u32(0)
	e.u32(3) // probeShader
	e.u32(1)
	e.rect(image.Rect(0, 0, side, side))
	e.u32(uint32(len(is)))
	e.u32(0)
	b := graphicsdriver.BlendCopy
	e.u8(uint8(b.BlendFactorSourceRGB))
	e.u8(uint8(b.BlendFactorSourceAlpha))
	e.u8(uint8(b.BlendFactorDestinationRGB))
	e.u8(uint8(b.BlendFactorDestinationAlpha))
	e.u8(uint8(b.BlendOperationRGB))
	e.u8(uint8(b.BlendOperationAlpha))
	e.u64(1)
	e.u64(1)
	e.u32s(u)

	r := NewReplayer()
	// Set MaxImages to exact count of images the stream creates (screen + 1 offscreen = 2)
	// and MaxPixels to exact physical backing pixels (32x32 unmanaged screen + 16x16 offscreen)
	// to verify the internal placeholder overhead does not fail a stream at its cap.
	r.SetLimits(Limits{MaxImages: 2, MaxPixels: int64(internalSide*internalSide + 16*16)})
	defer r.Dispose()

	target := &Target{Image: targetImg, Width: side, Height: side, InternalWidth: internalSide, InternalHeight: internalSide}
	if _, err := r.Replay(g, e.b, target); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4*side*side)
	if err := targetImg.ReadPixels([]graphicsdriver.PixelsArgs{{Pixels: got, Region: image.Rect(0, 0, side, side)}}); err != nil {
		t.Fatal(err)
	}
	want := make([]byte, len(got))
	if !bytes.Equal(got, want) {
		t.Errorf("non-origin texel probe produced non-zero pixels: first pixel %v, want [0 0 0 0]", got[:4])
	}
}
