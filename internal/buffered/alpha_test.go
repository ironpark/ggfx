// Copyright 2026 The Ebitengine Authors
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

package buffered_test

import (
	"bytes"
	"image"
	"testing"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/buffered"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/render"
)

func readA8(t *testing.T, img *buffered.Image, r image.Rectangle) []byte {
	t.Helper()
	pix := make([]byte, r.Dx()*r.Dy())
	ok, err := img.ReadPixels(render.GraphicsDriver(), pix, r)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("ReadPixels failed")
	}
	return pix
}

// TestA8Dots writes single alpha pixels, which are kept as dots and drawn to the GPU when the
// image is used.
func TestA8Dots(t *testing.T) {
	const w, h = 8, 8
	img := buffered.NewImageWithFormat(w, h, atlas.ImageTypeRegular, graphicsdriver.PixelFormatA8)
	defer img.Deallocate()
	if img.Format() != graphicsdriver.PixelFormatA8 {
		t.Fatalf("Format(): got %s", img.Format())
	}

	img.WritePixels(make([]byte, w*h), image.Rect(0, 0, w, h))
	dots := map[image.Point]byte{{1, 1}: 0x10, {3, 2}: 0x80, {7, 7}: 0xff}
	for p, a := range dots {
		img.WritePixels([]byte{a}, image.Rect(p.X, p.Y, p.X+1, p.Y+1))
	}
	// A dot reads back from the cache.
	if got := readA8(t, img, image.Rect(3, 2, 4, 3)); got[0] != 0x80 {
		t.Errorf("dot (3, 2): got %#x, want 0x80", got[0])
	}

	// Drawing from the image draws the dots to the GPU first.
	dst := buffered.NewImageWithFormat(w, h, atlas.ImageTypeRegular, graphicsdriver.PixelFormatA8)
	defer dst.Deallocate()
	vs := make([]float32, 4*graphics.VertexFloatCount)
	graphics.QuadVerticesFromDstAndSrc(vs, 0, 0, w, h, 0, 0, w, h, 1, 1, 1, 1)
	r := image.Rect(0, 0, w, h)
	dst.DrawTriangles([graphics.ShaderSrcImageCount]*buffered.Image{img}, vs, graphics.QuadIndices(), graphicsdriver.BlendCopy, r, [graphics.ShaderSrcImageCount]image.Rectangle{r}, atlas.NearestFilterShader, nil)

	want := make([]byte, w*h)
	for p, a := range dots {
		want[p.Y*w+p.X] = a
	}
	if got := readA8(t, dst, r); !bytes.Equal(got, want) {
		t.Errorf("drawn: got %v, want %v", got, want)
	}
	if got := readA8(t, img, r); !bytes.Equal(got, want) {
		t.Errorf("source: got %v, want %v", got, want)
	}
}

// TestA8Tiles reads and writes a part of an alpha image large enough to be cached in tiles.
func TestA8Tiles(t *testing.T) {
	// An A8 image is tiled from 4 times as many pixels as an RGBA one.
	const width = 2048
	height := buffered.MaxPixelsCacheSize/width + 1
	img := buffered.NewImageWithFormat(width, height, atlas.ImageTypeUnmanaged, graphicsdriver.PixelFormatA8)
	defer img.Deallocate()

	pix := make([]byte, width*height)
	for i := range pix {
		pix[i] = byte(i % 251)
	}
	img.WritePixels(pix, image.Rect(0, 0, width, height))

	r := image.Rect(250, height-10, 270, height)
	got := readA8(t, img, r)
	for j := range r.Dy() {
		for i := range r.Dx() {
			x, y := r.Min.X+i, r.Min.Y+j
			if want := pix[y*width+x]; got[j*r.Dx()+i] != want {
				t.Fatalf("(%d, %d): got %d, want %d", x, y, got[j*r.Dx()+i], want)
			}
		}
	}

	// A write to a cached tile updates the cache.
	img.WritePixels([]byte{1, 2}, image.Rect(255, height-1, 257, height))
	if got := readA8(t, img, image.Rect(255, height-1, 257, height)); !bytes.Equal(got, []byte{1, 2}) {
		t.Errorf("after a write: got %v, want [1 2]", got)
	}
}
