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

package graphicscommand_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/render"
	etesting "github.com/ironpark/ggfx/internal/testing"
)

// flushQueue runs the queued commands, so that a test starts with an empty queue.
func flushQueue(t *testing.T) {
	t.Helper()
	if err := graphicscommand.FlushCommands(render.GraphicsDriver(), graphicsdriver.FlushModeIntermediate); err != nil {
		t.Fatal(err)
	}
}

// writeColor writes c to region of img.
func writeColor(img *graphicscommand.Image, region image.Rectangle, c color.RGBA) {
	img.WritePixels(graphics.NewManagedBytes(4*region.Dx()*region.Dy(), func(bs []byte) {
		for i := 0; i < len(bs); i += 4 {
			bs[i], bs[i+1], bs[i+2], bs[i+3] = c.R, c.G, c.B, c.A
		}
	}), region)
}

// copyRegion copies region of src to the same region of dst.
func copyRegion(dst, src *graphicscommand.Image, region image.Rectangle) {
	vs := make([]float32, 4*graphics.VertexFloatCount)
	x0, y0, x1, y1 := float32(region.Min.X), float32(region.Min.Y), float32(region.Max.X), float32(region.Max.Y)
	graphics.QuadVerticesFromDstAndSrc(vs, x0, y0, x1, y1, x0, y0, x1, y1, 1, 1, 1, 1)
	dst.DrawTriangles([graphics.ShaderSrcImageCount]*graphicscommand.Image{src}, vs, graphics.QuadIndices(), graphicsdriver.BlendCopy, region, [graphics.ShaderSrcImageCount]image.Rectangle{region}, nearestFilterShader, nil)
}

var clearFillShader = graphicscommand.NewShader(etesting.ShaderProgramFill(0, 0, 0, 0), "")

// clearRegion clears region of dst, as the atlas clears a freed slot.
func clearRegion(dst *graphicscommand.Image, region image.Rectangle) {
	vs := make([]float32, 4*graphics.VertexFloatCount)
	x0, y0, x1, y1 := float32(region.Min.X), float32(region.Min.Y), float32(region.Max.X), float32(region.Max.Y)
	graphics.QuadVerticesFromDstAndSrc(vs, x0, y0, x1, y1, 0, 0, 0, 0, 0, 0, 0, 0)
	dst.DrawTriangles([graphics.ShaderSrcImageCount]*graphicscommand.Image{}, vs, graphics.QuadIndices(), graphicsdriver.BlendClear, region, [graphics.ShaderSrcImageCount]image.Rectangle{}, clearFillShader, nil)
}

// pixelAt reads the pixel at (x, y) of img.
func pixelAt(t *testing.T, img *graphicscommand.Image, x, y int) color.RGBA {
	t.Helper()
	pix := make([]byte, 4)
	if err := img.ReadPixels(render.GraphicsDriver(), []graphicsdriver.PixelsArgs{{Pixels: pix, Region: image.Rect(x, y, x+1, y+1)}}); err != nil {
		t.Fatal(err)
	}
	return color.RGBA{R: pix[0], G: pix[1], B: pix[2], A: pix[3]}
}

func slot(k int) image.Rectangle {
	return image.Rect(1+4*k, 1, 4+4*k, 4)
}

// Images written and drawn one after another, as a text renderer does with new glyphs, are
// uploaded in one command.
func TestWritePixelsJoinAcrossDraws(t *testing.T) {
	const w, h = 64, 8
	page := graphicscommand.NewImage(w, h, nil, "")
	dst := graphicscommand.NewImage(w, h, nil, "")
	clearRegion(page, image.Rect(0, 0, w, h))
	clearRegion(dst, image.Rect(0, 0, w, h))
	flushQueue(t)

	const n = 8
	for k := range n {
		writeColor(page, slot(k), color.RGBA{R: byte(k + 1), A: 0xff})
		copyRegion(dst, page, slot(k))
	}
	if got, writes := graphicscommand.QueuedWritePixelsCommandsForTesting(); got != 1 || writes != n {
		t.Errorf("write-pixels commands: got %d holding %d writes, want 1 holding %d", got, writes, n)
	}

	for k := range n {
		r := slot(k)
		if got, want := pixelAt(t, dst, r.Min.X+1, r.Min.Y+1), (color.RGBA{R: byte(k + 1), A: 0xff}); got != want {
			t.Errorf("dst at slot %d: got %v, want %v", k, got, want)
		}
	}
}

// A write to a freed slot that was cleared must not join a write before the clear: the clear
// would wipe it.
func TestWritePixelsAfterClearOfReusedSlot(t *testing.T) {
	const w, h = 64, 8
	page := graphicscommand.NewImage(w, h, nil, "")
	dst := graphicscommand.NewImage(w, h, nil, "")
	clearRegion(dst, image.Rect(0, 0, w, h))
	flushQueue(t)

	red := color.RGBA{R: 0xff, A: 0xff}
	green := color.RGBA{G: 0xff, A: 0xff}
	blue := color.RGBA{B: 0xff, A: 0xff}

	// Slot 0 holds red, drawn before the slot is freed.
	writeColor(page, slot(0), red)
	copyRegion(dst, page, slot(0))
	// Another image is written and drawn: its write is the pending one.
	writeColor(page, slot(1), green)
	copyRegion(dst, page, slot(1))
	// Slot 0 is freed and cleared, then reused for a blue image.
	clearRegion(page, slot(0))
	writeColor(page, slot(0), blue)
	copyRegion(dst, page, slot(0))

	if got, want := pixelAt(t, page, 2, 2), blue; got != want {
		t.Errorf("page at the reused slot: got %v, want %v", got, want)
	}
	if got, want := pixelAt(t, dst, 2, 2), blue; got != want {
		t.Errorf("dst at the reused slot: got %v, want %v", got, want)
	}
	if got, want := pixelAt(t, dst, 6, 2), green; got != want {
		t.Errorf("dst at slot 1: got %v, want %v", got, want)
	}
}

// A write to a region a draw read must not join a write before that draw: the draw would read the
// later pixels.
func TestWritePixelsWriteDrawWriteDraw(t *testing.T) {
	const w, h = 64, 8
	page := graphicscommand.NewImage(w, h, nil, "")
	dst0 := graphicscommand.NewImage(w, h, nil, "")
	dst1 := graphicscommand.NewImage(w, h, nil, "")
	clearRegion(dst0, image.Rect(0, 0, w, h))
	clearRegion(dst1, image.Rect(0, 0, w, h))
	flushQueue(t)

	red := color.RGBA{R: 0xff, A: 0xff}
	green := color.RGBA{G: 0xff, A: 0xff}

	writeColor(page, slot(0), red)
	copyRegion(dst0, page, slot(0))
	writeColor(page, slot(0), green)
	copyRegion(dst1, page, slot(0))
	if got, _ := graphicscommand.QueuedWritePixelsCommandsForTesting(); got != 2 {
		t.Errorf("write-pixels commands: got %d, want 2", got)
	}

	if got, want := pixelAt(t, dst0, 2, 2), red; got != want {
		t.Errorf("dst0: got %v, want %v", got, want)
	}
	if got, want := pixelAt(t, dst1, 2, 2), green; got != want {
		t.Errorf("dst1: got %v, want %v", got, want)
	}
}

// A write next to a region a draw read, past the pixel filtering may read beyond it, still joins
// the pending write; one within that pixel does not.
func TestWritePixelsNextToReadRegion(t *testing.T) {
	const w, h = 64, 8
	page := graphicscommand.NewImage(w, h, nil, "")
	dst := graphicscommand.NewImage(w, h, nil, "")
	flushQueue(t)

	c := color.RGBA{R: 0xff, A: 0xff}
	writeColor(page, image.Rect(1, 1, 4, 4), c)
	copyRegion(dst, page, image.Rect(1, 1, 4, 4))
	// (5, 1) is two pixels right of the read region: it joins.
	writeColor(page, image.Rect(5, 1, 8, 4), c)
	copyRegion(dst, page, image.Rect(5, 1, 8, 4))
	if got, _ := graphicscommand.QueuedWritePixelsCommandsForTesting(); got != 1 {
		t.Errorf("write-pixels commands after a write two pixels away: got %d, want 1", got)
	}
	// (8, 1) is right next to the last read region: a linear filter may read it.
	writeColor(page, image.Rect(8, 1, 10, 4), c)
	copyRegion(dst, page, image.Rect(8, 1, 10, 4))
	if got, _ := graphicscommand.QueuedWritePixelsCommandsForTesting(); got != 2 {
		t.Errorf("write-pixels commands after a write next to a read region: got %d, want 2", got)
	}
	flushQueue(t)
}
