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

package vector_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/vector"
)

func rectPaths(rects ...image.Rectangle) ([]*vector.Path, []image.Rectangle) {
	var paths []*vector.Path
	var bounds []image.Rectangle
	for _, r := range rects {
		var p vector.Path
		p.MoveTo(float32(r.Min.X), float32(r.Min.Y))
		p.LineTo(float32(r.Max.X), float32(r.Min.Y))
		p.LineTo(float32(r.Max.X), float32(r.Max.Y))
		p.LineTo(float32(r.Min.X), float32(r.Max.Y))
		p.Close()
		paths = append(paths, &p)
		bounds = append(bounds, image.Rect(-1<<20, -1<<20, 1<<20, 1<<20))
	}
	return paths, bounds
}

var (
	hidpiScreen = image.Rect(0, 0, 2880, 1800)
	largeRect   = image.Rect(0, 0, 2880, 1800)
	smallRect   = image.Rect(10, 10, 110, 110)
)

func TestStencilAtlasShrinksAfterSmallerUse(t *testing.T) {
	a := vector.NewStencilAtlas()
	defer a.Dispose()

	setRects(a, hidpiScreen, largeRect)
	large := a.Bytes()
	if s := a.ImageSizes()[0]; s.X < 2*largeRect.Dx() || s.Y < largeRect.Dy() {
		t.Fatalf("image size: got %v, want at least %v", s, image.Pt(2*largeRect.Dx(), largeRect.Dy()))
	}

	for range 3 * vector.AtlasRecentWindow {
		a.Advance(1)
		setRects(a, hidpiScreen, smallRect)
	}
	if got, want := a.Allocations(), 2; got != want {
		t.Errorf("allocations: got %d, want %d", got, want)
	}
	if s := a.ImageSizes()[0]; s.X > 512 || s.Y > 512 {
		t.Errorf("image size after small fills: got %v, want at most 512x512", s)
	}
	if got := a.Bytes(); got*100 > large {
		t.Errorf("bytes: got %d after small fills, want less than 1%% of %d", got, large)
	}
}

func TestStencilAtlasKeepsSizeWithinWindow(t *testing.T) {
	a := vector.NewStencilAtlas()
	defer a.Dispose()

	setRects(a, hidpiScreen, largeRect)
	sizes := a.ImageSizes()
	for range vector.AtlasRecentWindow - 1 {
		a.Advance(1)
		setRects(a, hidpiScreen, smallRect)
	}
	if got, want := a.Allocations(), 1; got != want {
		t.Errorf("allocations: got %d, want %d", got, want)
	}
	if got := a.ImageSizes(); got[0] != sizes[0] {
		t.Errorf("image size: got %v, want %v", got[0], sizes[0])
	}
}

func TestStencilAtlasNoThrash(t *testing.T) {
	for _, period := range []int64{0, 1, 2, vector.AtlasRecentWindow / 2, vector.AtlasRecentWindow} {
		a := vector.NewStencilAtlas()
		// Alternate within a flush, or every period flushes.
		for i := range 20 * vector.AtlasRecentWindow {
			r := smallRect
			if (period == 0 && i%2 == 0) || (period > 0 && int64(i)%period == 0) {
				r = largeRect
			}
			setRects(a, hidpiScreen, r)
			if period > 0 {
				a.Advance(1)
			}
		}
		if got, want := a.Allocations(), 1; got != want {
			t.Errorf("period %d: allocations: got %d, want %d", period, got, want)
		}
		a.Dispose()
	}
}

func TestStencilAtlasFreesUnusedImages(t *testing.T) {
	a := vector.NewStencilAtlas()
	defer a.Dispose()

	// Each path is too large to share an atlas image with another.
	dst := image.Rect(0, 0, 4093, 4093)
	three := []image.Rectangle{
		image.Rect(0, 0, 2100, 2100),
		image.Rect(100, 100, 2200, 2200),
		image.Rect(200, 200, 2300, 2300),
	}
	setRectsNoAA(a, dst, three...)
	if got, want := a.ActiveImageCount(), 3; got != want {
		t.Fatalf("active images: got %d, want %d", got, want)
	}

	// Alternating counts keep the images.
	for i := range 4 * vector.AtlasRecentWindow {
		a.Advance(1)
		if i%10 == 0 {
			setRectsNoAA(a, dst, three...)
		} else {
			setRectsNoAA(a, dst, three[0])
		}
		if got, want := len(a.ImageSizes()), 3; got != want {
			t.Fatalf("kept images while alternating: got %d, want %d", got, want)
		}
	}
	if got, want := a.Allocations(), 3; got != want {
		t.Errorf("allocations while alternating: got %d, want %d", got, want)
	}

	for range 3 * vector.AtlasRecentWindow {
		a.Advance(1)
		setRectsNoAA(a, dst, three[0])
	}
	if got, want := len(a.ImageSizes()), 1; got != want {
		t.Errorf("kept images: got %d, want %d", got, want)
	}
}

func TestStencilAtlasIdleFlushesAge(t *testing.T) {
	a := vector.NewStencilAtlas()
	defer a.Dispose()

	setRects(a, hidpiScreen, largeRect)
	// Flushes without any fill count as time too.
	a.Advance(3 * vector.AtlasRecentWindow)
	setRects(a, hidpiScreen, smallRect)
	if s := a.ImageSizes()[0]; s.X > 512 || s.Y > 512 {
		t.Errorf("image size: got %v, want at most 512x512", s)
	}
}

// TestFillAfterStencilShrink fills a large path, and then small ones over many flushes, through
// the shared stencil atlas, and checks the pixels and that the stencil atlas shrank.
func TestFillAfterStencilShrink(t *testing.T) {
	const w, h = 2000, 1200
	dst := ggfx.NewImage(w, h)
	defer dst.Deallocate()

	vector.FillRect(dst, 0, 0, w, h, color.White, true)
	if got, want := dst.At(w-1, h-1), (color.RGBA{0xff, 0xff, 0xff, 0xff}); got != want {
		t.Fatalf("large fill: got %v, want %v", got, want)
	}
	large := vector.StencilBytes()
	if large < 4*2*w*h {
		t.Fatalf("stencil bytes after a large fill: got %d, want at least %d", large, 4*2*w*h)
	}

	red := color.RGBA{0xff, 0, 0, 0xff}
	for i := range 3 * vector.AtlasRecentWindow {
		x := float32(i % 100 * 16)
		vector.FillRect(dst, x, 16, 16, 16, red, true)
		// Reading flushes the command queue, which is the stencil atlas's clock.
		if got := dst.At(int(x)+8, 24); got != red {
			t.Fatalf("small fill %d: got %v, want %v", i, got, red)
		}
	}
	if got := vector.StencilBytes(); got*10 > large {
		t.Errorf("stencil bytes after small fills: got %d, want less than a tenth of %d", got, large)
	}

	vector.FillRect(dst, 0, 0, w, h, color.Black, true)
	if got, want := dst.At(w-1, h-1), (color.RGBA{0, 0, 0, 0xff}); got != want {
		t.Errorf("large fill after shrinking: got %v, want %v", got, want)
	}
}

func setRects(a *vector.StencilAtlas, dst image.Rectangle, rects ...image.Rectangle) {
	paths, bounds := rectPaths(rects...)
	a.SetPaths(dst, paths, bounds, true)
}

func setRectsNoAA(a *vector.StencilAtlas, dst image.Rectangle, rects ...image.Rectangle) {
	paths, bounds := rectPaths(rects...)
	a.SetPaths(dst, paths, bounds, false)
}
