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

package ggfx_test

import (
	"image"
	"image/color"
	"image/draw"
	"sync"
	"testing"

	"github.com/ironpark/ggfx"
)

// Concurrent draws into the same cached sub-image (which shares one *ggfx.Image) must not race
// on scratch buffers.
func TestConcurrentDrawsToSameSubImage(t *testing.T) {
	dst := ggfx.NewImage(64, 64)
	defer dst.Deallocate()
	src := ggfx.NewImage(16, 16)
	defer src.Deallocate()
	src.Fill(color.RGBA{R: 0x80, G: 0x40, B: 0x20, A: 0xff})

	shader, err := ggfx.NewShader([]byte(`
fn fragment(v: Vertex) -> vec4f {
	return v.color;
}
`))
	if err != nil {
		t.Fatal(err)
	}
	defer shader.Dispose()

	vs := []ggfx.Vertex{
		{DstX: 0, DstY: 0, SrcX: 0, SrcY: 0, ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1},
		{DstX: 16, DstY: 0, SrcX: 16, SrcY: 0, ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1},
		{DstX: 0, DstY: 16, SrcX: 0, SrcY: 16, ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1},
		{DstX: 16, DstY: 16, SrcX: 16, SrcY: 16, ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1},
	}
	is16 := []uint16{0, 1, 2, 1, 2, 3}
	is32 := []uint32{0, 1, 2, 1, 2, 3}

	subRect := image.Rect(4, 4, 36, 36)
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sub := dst.SubImage(subRect).(*ggfx.Image)
			draws := []ggfx.ImageDraw{{Image: src, X: float64(id * 2), Y: 0}}
			for range 25 {
				sub.DrawImage(src, nil)
				sub.DrawTriangles(vs, is16, src, nil)
				sub.DrawTriangles(vs, is32, src, nil)
				sub.DrawTrianglesShader(vs, is16, shader, nil)
				sub.DrawRectShader(16, 16, shader, nil)
				sub.DrawImages(draws, nil)
			}
		}(g)
	}
	wg.Wait()

	// Ensure queued commands execute cleanly.
	_ = dst.RGBA64At(8, 8)
}

func TestSetRGBA64AndDrawOverZeroAllocs(t *testing.T) {
	dst := ggfx.NewImage(16, 16)
	defer dst.Deallocate()
	c := color.RGBA64{R: 0x1234, G: 0x5678, B: 0x9abc, A: 0xffff}
	dst.SetRGBA64(3, 4, c)
	want := color.RGBA64{R: 0x1212, G: 0x5656, B: 0x9a9a, A: 0xffff}
	if got := dst.RGBA64At(3, 4); got != want {
		t.Errorf("RGBA64At(3, 4) = %+v, want %+v", got, want)
	}
	// Out-of-bounds SetRGBA64 is a no-op.
	dst.SetRGBA64(-1, 0, c)
	dst.SetRGBA64(16, 0, c)

	// Alpha image keeps only alpha.
	alpha := ggfx.NewImageWithOptions(image.Rect(0, 0, 8, 8), &ggfx.NewImageOptions{Format: ggfx.PixelFormatAlpha})
	defer alpha.Deallocate()
	alpha.SetRGBA64(2, 2, color.RGBA64{R: 0x1111, G: 0x2222, B: 0x3333, A: 0x8080})
	if got, want := alpha.RGBA64At(2, 2), (color.RGBA64{R: 0x8080, G: 0x8080, B: 0x8080, A: 0x8080}); got != want {
		t.Errorf("alpha RGBA64At(2, 2) = %+v, want %+v", got, want)
	}

	// Steady-state draw.Draw Over on a cached tile must allocate 0 times per call.
	src := image.NewUniform(color.RGBA{R: 0x40, G: 0x20, B: 0x10, A: 0x80})
	draw.Draw(dst, dst.Bounds(), src, image.Point{}, draw.Over)
	allocs := testing.AllocsPerRun(20, func() {
		draw.Draw(dst, dst.Bounds(), src, image.Point{}, draw.Over)
	})
	if allocs != 0 {
		t.Errorf("draw.Draw Over allocs/run = %v, want 0", allocs)
	}
}
