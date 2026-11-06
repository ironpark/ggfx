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

package atlas_test

import (
	"image"
	"testing"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/render"
)

// A source page that starts small grows as images come, and keeps the pixels of the images it
// held, for RGBA8 and A8 pages alike.
func TestGrowSmallSourcePage(t *testing.T) {
	atlas.SetImageSizeForTesting(256, minDestinationImageSizeForTesting, maxImageSizeForTesting)
	defer atlas.ResetImageSizeForTesting()

	for _, format := range []graphicsdriver.PixelFormat{graphicsdriver.PixelFormatRGBA8, graphicsdriver.PixelFormatA8} {
		t.Run(format.String(), func(t *testing.T) {
			bpp := format.BytesPerPixel()
			// 120 images of 30x30 and their paddings take more than a 256x256 page.
			const n, w, h = 120, 30, 30
			pixel := func(k, x, y int) byte { return byte(k*31 + x*7 + y*3 + 1) }

			imgs := make([]*atlas.Image, n)
			for k := range imgs {
				img := atlas.NewImageWithFormat(w, h, atlas.ImageTypeRegular, format)
				defer img.Deallocate()
				pix := make([]byte, bpp*w*h)
				for y := range h {
					for x := range w {
						for c := range bpp {
							pix[bpp*(y*w+x)+c] = pixel(k, x, y)
						}
					}
				}
				img.WritePixels(pix, image.Rect(0, 0, w, h))
				imgs[k] = img
			}

			if bw, bh := imgs[0].BackendSizeForTesting(); bw*bh <= 256*256 {
				t.Errorf("the first image's page is %dx%d, want it grown past 256x256", bw, bh)
			}
			if !imgs[0].SharesBackendForTesting(imgs[n-1]) {
				t.Errorf("the first and the last image are on different pages, want the page grown")
			}

			for k, img := range imgs {
				got := make([]byte, bpp*w*h)
				if _, err := img.ReadPixels(render.GraphicsDriver(), got, image.Rect(0, 0, w, h)); err != nil {
					t.Fatal(err)
				}
				for y := range h {
					for x := range w {
						for c := range bpp {
							if g, want := got[bpp*(y*w+x)+c], pixel(k, x, y); g != want {
								t.Fatalf("image %d at (%d, %d)[%d]: got %d, want %d", k, x, y, c, g, want)
							}
						}
					}
				}
			}
		})
	}
}
