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

package rendertest_test

import (
	"image/color"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/recent"
)

// atlasImages counts the images on atlas pages and the isolated ones.
func atlasImages() int {
	var d ggfx.DebugInfo
	ggfx.ReadDebugInfo(&d)
	n := d.Isolated.Images
	for _, p := range d.AtlasPages {
		n += p.Images
	}
	return n
}

// The mipmap levels a scaled-down draw makes are dropped once no draw uses them for a while, and
// made again, from the image's current pixels, when a draw needs them again.
func TestMipmapLevelsDropped(t *testing.T) {
	const size = 256
	src := ggfx.NewImage(size, size)
	defer src.Deallocate()
	dst := ggfx.NewImage(64, 64)
	defer dst.Deallocate()

	drawScaled := func() {
		dst.Clear()
		op := &ggfx.DrawImageOptions{Filter: ggfx.FilterLinear}
		op.GeoM.Scale(1.0/8, 1.0/8)
		dst.DrawImage(src, op)
	}

	red := color.RGBA{0xff, 0, 0, 0xff}
	green := color.RGBA{0, 0xff, 0, 0xff}
	frame(t, func() {
		src.Fill(red)
		dst.Fill(color.White)
	})
	base := atlasImages()

	frame(t, drawScaled)
	withLevels := atlasImages()
	if withLevels <= base {
		t.Fatalf("images after a scaled-down draw: got %d, want more than %d", withLevels, base)
	}
	// The level drawn from stays while draws use it. The levels below it are needed only to
	// make it again, and go.
	for range 2 * recent.Window {
		frame(t, drawScaled)
	}
	if got := atlasImages(); got <= base || got > withLevels {
		t.Errorf("images while drawing scaled down every frame: got %d, want more than %d and at most %d", got, base, withLevels)
	}

	// They go once no draw uses them for a while.
	for range recent.Window + 2 {
		frame(t, func() {})
	}
	if got := atlasImages(); got != base {
		t.Errorf("images after %d idle frames: got %d, want %d", recent.Window+2, got, base)
	}

	// A draw needs them again: they are made from the image's current pixels.
	frame(t, func() { src.Fill(green) })
	var got color.RGBA
	frame(t, drawScaled)
	frame(t, func() { got = dst.At(16, 16).(color.RGBA) })
	if got != green {
		t.Errorf("dst after the levels were made again: got %v, want %v", got, green)
	}

	// Writing the image makes the levels made from its old pixels stale.
	frame(t, func() { src.Fill(red) })
	frame(t, drawScaled)
	frame(t, func() { got = dst.At(16, 16).(color.RGBA) })
	if got != red {
		t.Errorf("dst after the image was filled again: got %v, want %v", got, red)
	}
}
