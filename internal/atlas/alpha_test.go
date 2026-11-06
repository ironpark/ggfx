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

package atlas_test

import (
	"bytes"
	"image"
	"testing"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/render"
)

// forEachA8Mode runs f with A8 images on A8 backends, where the driver supports them, and on RGBA8
// backends, as on a driver that does not.
func forEachA8Mode(t *testing.T, f func(t *testing.T, native bool)) {
	for _, native := range []bool{true, false} {
		name := "fallback"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			if native && !atlas.A8SupportedForTesting() {
				t.Skip("the driver does not support A8")
			}
			defer atlas.SetA8SupportedForTesting(native)()
			f(t, native)
		})
	}
}

func readPixels(t *testing.T, img *atlas.Image, bpp int, r image.Rectangle) []byte {
	t.Helper()
	pix := make([]byte, bpp*r.Dx()*r.Dy())
	ok, err := img.ReadPixels(render.GraphicsDriver(), pix, r)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("ReadPixels failed")
	}
	return pix
}

func wantBackendFormat(t *testing.T, img *atlas.Image, native bool) {
	t.Helper()
	want := graphicsdriver.PixelFormatRGBA8
	if native {
		want = graphicsdriver.PixelFormatA8
	}
	got, ok := img.BackendFormatForTesting()
	if !ok {
		t.Fatal("the image is not allocated")
	}
	if got != want {
		t.Errorf("backend format: got %s, want %s", got, want)
	}
	if img.Format() != graphicsdriver.PixelFormatA8 {
		t.Errorf("Format(): got %s, want A8", img.Format())
	}
}

func TestA8WriteReadPixels(t *testing.T) {
	forEachA8Mode(t, func(t *testing.T, native bool) {
		for _, typ := range []atlas.ImageType{atlas.ImageTypeRegular, atlas.ImageTypeUnmanaged} {
			const w, h = 5, 7
			img := atlas.NewImageWithFormat(w, h, typ, graphicsdriver.PixelFormatA8)
			defer img.Deallocate()

			// The whole region: a managed image's padding is written too.
			pix := make([]byte, w*h)
			for i := range pix {
				pix[i] = byte(17 * i)
			}
			img.WritePixels(pix, image.Rect(0, 0, w, h))
			wantBackendFormat(t, img, native)
			if got := readPixels(t, img, 1, image.Rect(0, 0, w, h)); !bytes.Equal(got, pix) {
				t.Errorf("type %d: got %v, want %v", typ, got, pix)
			}

			// A part.
			img.WritePixels([]byte{1, 2, 3, 4}, image.Rect(1, 2, 3, 4))
			if got, want := readPixels(t, img, 1, image.Rect(1, 2, 3, 4)), []byte{1, 2, 3, 4}; !bytes.Equal(got, want) {
				t.Errorf("type %d: part: got %v, want %v", typ, got, want)
			}
			if got, want := readPixels(t, img, 1, image.Rect(0, 0, 1, 1)), pix[:1]; !bytes.Equal(got, want) {
				t.Errorf("type %d: outside the part: got %v, want %v", typ, got, want)
			}
		}
	})
}

func TestA8LengthPanics(t *testing.T) {
	img := atlas.NewImageWithFormat(2, 2, atlas.ImageTypeRegular, graphicsdriver.PixelFormatA8)
	defer img.Deallocate()
	defer func() {
		if recover() == nil {
			t.Error("WritePixels with RGBA bytes on an A8 image did not panic")
		}
	}()
	img.WritePixels(make([]byte, 16), image.Rect(0, 0, 2, 2))
}

func TestA8Pages(t *testing.T) {
	forEachA8Mode(t, func(t *testing.T, native bool) {
		a0 := atlas.NewImageWithFormat(16, 16, atlas.ImageTypeRegular, graphicsdriver.PixelFormatA8)
		defer a0.Deallocate()
		a1 := atlas.NewImageWithFormat(16, 16, atlas.ImageTypeRegular, graphicsdriver.PixelFormatA8)
		defer a1.Deallocate()
		rgba := atlas.NewImage(16, 16, atlas.ImageTypeRegular)
		defer rgba.Deallocate()

		// Writing pixels puts each image on a source page.
		a0.WritePixels(make([]byte, 16*16), image.Rect(0, 0, 16, 16))
		a1.WritePixels(make([]byte, 16*16), image.Rect(0, 0, 16, 16))
		rgba.WritePixels(make([]byte, 4*16*16), image.Rect(0, 0, 16, 16))

		if !a0.SharesBackendForTesting(a1) {
			t.Error("two A8 images are not on one page")
		}
		if got := a0.SharesBackendForTesting(rgba); got == native {
			t.Errorf("an A8 and an RGBA image share a page: %t, want %t", got, !native)
		}
		wantBackendFormat(t, a0, native)
	})
}

// TestA8SampledAsWhite draws an A8 image into an RGBA one: a pixel of alpha a is (a, a, a, a).
func TestA8SampledAsWhite(t *testing.T) {
	forEachA8Mode(t, func(t *testing.T, native bool) {
		const w, h = 4, 4
		src := atlas.NewImageWithFormat(w, h, atlas.ImageTypeRegular, graphicsdriver.PixelFormatA8)
		defer src.Deallocate()
		dst := atlas.NewImage(w, h, atlas.ImageTypeRegular)
		defer dst.Deallocate()

		pix := make([]byte, w*h)
		for i := range pix {
			pix[i] = byte(16*i + 3)
		}
		src.WritePixels(pix, image.Rect(0, 0, w, h))

		vs := quadVertices(w, h, 0, 0, 1)
		r := image.Rect(0, 0, w, h)
		dst.DrawTriangles([graphics.ShaderSrcImageCount]*atlas.Image{src}, vs, graphics.QuadIndices(), graphicsdriver.BlendCopy, r, [graphics.ShaderSrcImageCount]image.Rectangle{r}, atlas.NearestFilterShader, nil)

		want := make([]byte, 4*w*h)
		graphics.ExpandAlpha(want, pix)
		if got := readPixels(t, dst, 4, r); !bytes.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// TestA8DrawInto draws RGBA pixels into an A8 image, which keeps their red.
func TestA8DrawInto(t *testing.T) {
	forEachA8Mode(t, func(t *testing.T, native bool) {
		const w, h = 4, 1
		src := atlas.NewImage(w, h, atlas.ImageTypeRegular)
		defer src.Deallocate()
		dst := atlas.NewImageWithFormat(w, h, atlas.ImageTypeRegular, graphicsdriver.PixelFormatA8)
		defer dst.Deallocate()

		src.WritePixels([]byte{
			0, 0, 0, 0,
			0x40, 0x40, 0x40, 0x40,
			0xff, 0xff, 0xff, 0xff,
			0x10, 0x20, 0x30, 0x80, // Not white: red is kept.
		}, image.Rect(0, 0, w, h))

		vs := quadVertices(w, h, 0, 0, 1)
		r := image.Rect(0, 0, w, h)
		dst.DrawTriangles([graphics.ShaderSrcImageCount]*atlas.Image{src}, vs, graphics.QuadIndices(), graphicsdriver.BlendCopy, r, [graphics.ShaderSrcImageCount]image.Rectangle{r}, atlas.NearestFilterShader, nil)
		wantBackendFormat(t, dst, native)
		if got, want := readPixels(t, dst, 1, r), []byte{0, 0x40, 0xff, 0x10}; !bytes.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// TestA8MovesBetweenBackends moves an A8 image off its source page, as a destination, and back
// onto a source page, as a source: its pixels are copied by draws each time.
func TestA8MovesBetweenBackends(t *testing.T) {
	forEachA8Mode(t, func(t *testing.T, native bool) {
		const w, h = 8, 8
		img := atlas.NewImageWithFormat(w, h, atlas.ImageTypeRegular, graphicsdriver.PixelFormatA8)
		defer img.Deallocate()
		other := atlas.NewImageWithFormat(w, h, atlas.ImageTypeRegular, graphicsdriver.PixelFormatA8)
		defer other.Deallocate()
		dst := atlas.NewImage(w, h, atlas.ImageTypeRegular)
		defer dst.Deallocate()

		pix := make([]byte, w*h)
		for i := range pix {
			pix[i] = byte(4*i + 1)
		}
		img.WritePixels(pix, image.Rect(0, 0, w, h))
		other.WritePixels(make([]byte, w*h), image.Rect(0, 0, w, h))
		if !img.IsOnSourceBackendForTesting() {
			t.Fatal("the image is not on a source page after WritePixels")
		}

		// Used as a destination, img leaves the source page, which other is a source on.
		vs := quadVertices(1, 1, 0, 0, 1)
		r := image.Rect(0, 0, w, h)
		img.DrawTriangles([graphics.ShaderSrcImageCount]*atlas.Image{other}, vs, graphics.QuadIndices(), graphicsdriver.BlendSourceOver, r, [graphics.ShaderSrcImageCount]image.Rectangle{image.Rect(0, 0, 1, 1)}, atlas.NearestFilterShader, nil)
		if img.IsOnSourceBackendForTesting() {
			t.Fatal("the image is on a source page after a draw into it")
		}
		wantBackendFormat(t, img, native)
		if got := readPixels(t, img, 1, r); !bytes.Equal(got, pix) {
			t.Errorf("after leaving the source page: got %v, want %v", got, pix)
		}

		// Used as a source often enough, img is put on a source page again.
		for range atlas.BaseCountToPutOnSourceBackend * 2 {
			vs := quadVertices(w, h, 0, 0, 1)
			dst.DrawTriangles([graphics.ShaderSrcImageCount]*atlas.Image{img}, vs, graphics.QuadIndices(), graphicsdriver.BlendCopy, r, [graphics.ShaderSrcImageCount]image.Rectangle{r}, atlas.NearestFilterShader, nil)
			atlas.PutImagesOnSourceBackendForTesting()
			if img.IsOnSourceBackendForTesting() {
				break
			}
		}
		if !img.IsOnSourceBackendForTesting() {
			t.Fatal("the image is not put on a source page")
		}
		wantBackendFormat(t, img, native)
		if got := readPixels(t, img, 1, r); !bytes.Equal(got, pix) {
			t.Errorf("after moving to a source page: got %v, want %v", got, pix)
		}
	})
}
