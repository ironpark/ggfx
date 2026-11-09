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
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
	"github.com/ironpark/ggfx/internal/render"
)

// compactTestImages makes n images of w x h on one source page, each with pixels of its own.
func compactTestImages(t *testing.T, format graphicsdriver.PixelFormat, n, w, h int) []*atlas.Image {
	t.Helper()
	bpp := format.BytesPerPixel()
	imgs := make([]*atlas.Image, n)
	for k := range imgs {
		img := atlas.NewImageWithFormat(w, h, atlas.ImageTypeRegular, format)
		pix := make([]byte, bpp*w*h)
		for i := range pix {
			pix[i] = compactTestPixel(k, i)
		}
		img.WritePixels(pix, image.Rect(0, 0, w, h))
		imgs[k] = img
	}
	for _, img := range imgs[1:] {
		if !img.SharesBackendForTesting(imgs[0]) {
			t.Fatalf("the images are not on one page")
		}
	}
	return imgs
}

func compactTestPixel(k, i int) byte { return byte(k*37 + i*5 + 1) }

func checkCompactTestImage(t *testing.T, img *atlas.Image, format graphicsdriver.PixelFormat, k, w, h int) {
	t.Helper()
	got := make([]byte, format.BytesPerPixel()*w*h)
	if _, err := img.ReadPixels(render.GraphicsDriver(), got, image.Rect(0, 0, w, h)); err != nil {
		t.Fatal(err)
	}
	for i, g := range got {
		if want := compactTestPixel(k, i); g != want {
			t.Fatalf("image %d, byte %d: got %d, want %d", k, i, g, want)
		}
	}
}

// compactTestClock is the last frame the compaction checks of these tests began at. A page outlives
// a test, and remembers the frame it was first seen mostly empty at, so the frames these tests give
// only go forward, whatever order the tests run in. They start far after the frame clock, which
// stays put while the tests run in one frame.
var compactTestClock int64 = 1 << 30

// compactTestFrames returns a frame to begin a run of compaction checks at, a hundred windows after
// the last one, so a run can use the frames up to there.
func compactTestFrames() int64 {
	compactTestClock += 100 * recent.Window
	return compactTestClock
}

// compactAfterWindow runs the compaction check as a page that stays as it is would see it over a
// whole window of frames.
func compactAfterWindow() {
	start := compactTestFrames()
	atlas.CompactPagesForTesting(start)
	atlas.CompactPagesForTesting(start + recent.Window)
}

// A page whose images mostly went is compacted: it shrinks, and the images left keep their pixels
// and stay usable.
func TestCompactPage(t *testing.T) {
	atlas.SetImageSizeForTesting(256, minDestinationImageSizeForTesting, maxImageSizeForTesting)
	defer atlas.ResetImageSizeForTesting()

	for _, format := range []graphicsdriver.PixelFormat{graphicsdriver.PixelFormatRGBA8, graphicsdriver.PixelFormatA8} {
		t.Run(format.String(), func(t *testing.T) {
			const n, w, h = 600, 30, 20
			imgs := compactTestImages(t, format, n, w, h)
			oldW, oldH := imgs[0].BackendSizeForTesting()

			// Keep one image in 20.
			var kept []int
			for k, img := range imgs {
				if k%20 == 7 {
					kept = append(kept, k)
					continue
				}
				img.Deallocate()
			}
			defer func() {
				for _, k := range kept {
					imgs[k].Deallocate()
				}
			}()
			first := imgs[kept[0]]
			if tracked, nodes := first.BackendUsedAreaForTesting(); tracked != nodes {
				t.Fatalf("used area: tracked %d, the nodes cover %d", tracked, nodes)
			}

			compactAfterWindow()

			newW, newH := first.BackendSizeForTesting()
			if newW*newH*2 > oldW*oldH {
				t.Errorf("page after compaction: %dx%d, want at most half of %dx%d", newW, newH, oldW, oldH)
			}
			if tracked, nodes := first.BackendUsedAreaForTesting(); tracked != nodes {
				t.Errorf("used area after compaction: tracked %d, the nodes cover %d", tracked, nodes)
			}
			for _, k := range kept {
				if !imgs[k].SharesBackendForTesting(first) {
					t.Errorf("image %d left the page", k)
				}
				checkCompactTestImage(t, imgs[k], format, k, w, h)
			}

			// The images can still be drawn from, freed, and their places reused.
			dst := atlas.NewImageWithFormat(w, h, atlas.ImageTypeRegular, format)
			defer dst.Deallocate()
			vs := quadVertices(w, h, 0, 0, 1)
			dr := image.Rect(0, 0, w, h)
			dst.DrawTriangles([graphics.ShaderSrcImageCount]*atlas.Image{imgs[kept[1]]}, vs, graphics.QuadIndices(), graphicsdriver.BlendCopy, dr, [graphics.ShaderSrcImageCount]image.Rectangle{dr}, atlas.NearestFilterShader, nil)
			checkCompactTestImage(t, dst, format, kept[1], w, h)

			imgs[kept[2]].Deallocate()
			kept = append(kept[:2], kept[3:]...)
			more := compactTestImages(t, format, 3, w, h)
			for k, img := range more {
				defer img.Deallocate()
				checkCompactTestImage(t, img, format, k, w, h)
			}
			for _, k := range kept {
				checkCompactTestImage(t, imgs[k], format, k, w, h)
			}
		})
	}
}

// A page is not compacted while its images use a quarter of it or more, nor before it has been
// mostly empty for a whole window.
func TestCompactPageNotEarly(t *testing.T) {
	atlas.SetImageSizeForTesting(256, minDestinationImageSizeForTesting, maxImageSizeForTesting)
	defer atlas.ResetImageSizeForTesting()

	const n, w, h = 600, 30, 20
	format := graphicsdriver.PixelFormatRGBA8
	imgs := compactTestImages(t, format, n, w, h)
	defer func() {
		for _, img := range imgs {
			if img != nil {
				img.Deallocate()
			}
		}
	}()
	// imgs[1] stays to the end, so the page can be looked at through it.
	used := func() int64 {
		u, _ := imgs[1].BackendUsedAreaForTesting()
		return u
	}

	// The images may have gone on a page an earlier test left larger than they need: let it be
	// compacted first, so the page is one they use well.
	compactAfterWindow()
	oldW, oldH := imgs[1].BackendSizeForTesting()
	area := int64(oldW) * int64(oldH)
	if used()*4 < area {
		t.Fatalf("page %dx%d: used %d before any image went, want a quarter or more", oldW, oldH, used())
	}

	// Free images while a quarter of the page or more stays used: the page stays.
	before := used()
	imgs[0].Deallocate()
	imgs[0] = nil
	each := before - used()
	freed := 1
	for k := 2; k < n && (used()-each)*4 >= area; k += 2 {
		imgs[k].Deallocate()
		imgs[k] = nil
		freed++
	}
	if used()*4 < area {
		t.Fatalf("page %dx%d: used %d after %d images went, want a quarter or more", oldW, oldH, used(), freed)
	}
	compactAfterWindow()
	if gotW, gotH := imgs[1].BackendSizeForTesting(); gotW != oldW || gotH != oldH {
		t.Errorf("page used %d of %dx%d after a window: %dx%d, want %dx%d", used(), oldW, oldH, gotW, gotH, oldW, oldH)
	}

	// Free most: the page stays until a window passes.
	for k, img := range imgs {
		if img != nil && k%20 != 1 {
			img.Deallocate()
			imgs[k] = nil
		}
	}
	if used()*4 >= area {
		t.Fatalf("page %dx%d: used %d after most images went, want less than a quarter", oldW, oldH, used())
	}
	start := compactTestFrames()
	atlas.CompactPagesForTesting(start)
	atlas.CompactPagesForTesting(start + recent.Window - 1)
	if gotW, gotH := imgs[1].BackendSizeForTesting(); gotW != oldW || gotH != oldH {
		t.Errorf("page mostly empty for less than a window: %dx%d, want %dx%d", gotW, gotH, oldW, oldH)
	}
	atlas.CompactPagesForTesting(start + recent.Window)
	if gotW, gotH := imgs[1].BackendSizeForTesting(); gotW*gotH*2 > oldW*oldH {
		t.Errorf("page mostly empty for a window: %dx%d, want it compacted", gotW, gotH)
	}
	for k, img := range imgs {
		if img != nil {
			checkCompactTestImage(t, img, format, k, w, h)
		}
	}

	// A page at its minimum size is never compacted, however empty.
	for k, img := range imgs {
		if img != nil && k != 1 {
			img.Deallocate()
			imgs[k] = nil
		}
	}
	compactAfterWindow()
	compactAfterWindow()
	if gotW, gotH := imgs[1].BackendSizeForTesting(); gotW*gotH < 256*256 {
		t.Errorf("page: %dx%d, want at least the minimum 256x256", gotW, gotH)
	}
	checkCompactTestImage(t, imgs[1], format, 1, w, h)
}
