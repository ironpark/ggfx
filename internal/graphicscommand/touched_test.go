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

package graphicscommand

import (
	"image"
	"testing"
)

func TestTouchedSet(t *testing.T) {
	var s touchedSet
	// Cells of 256x16 pixels, as on a 16384x1024 page.
	s.reset(16384, 1024)
	if s.overlaps(image.Rect(0, 0, 16384, 1024)) {
		t.Errorf("an empty set overlaps")
	}

	s.add(image.Rect(10, 2, 20, 12))
	for _, tc := range []struct {
		r    image.Rectangle
		want bool
	}{
		{image.Rect(15, 5, 16, 6), true},
		// In the same cell, but apart: told apart by the regions.
		{image.Rect(22, 2, 30, 12), false},
		{image.Rect(20, 2, 30, 12), false},
		{image.Rect(19, 2, 30, 12), true},
		// In another cell.
		{image.Rect(300, 2, 310, 12), false},
		{image.Rect(0, 100, 16384, 200), false},
	} {
		if got := s.overlaps(tc.r); got != tc.want {
			t.Errorf("overlaps(%v): got %v, want %v", tc.r, got, tc.want)
		}
	}

	// Past maxTouchedRegions, the cells answer alone: a region in a touched cell overlaps.
	for range maxTouchedRegions - 1 {
		s.add(image.Rect(10, 2, 11, 3))
	}
	if s.overflowed {
		t.Fatalf("overflowed at %d regions", maxTouchedRegions+1)
	}
	s.add(image.Rect(10, 2, 11, 3))
	if !s.overflowed {
		t.Fatalf("not overflowed past %d regions", maxTouchedRegions)
	}
	if !s.overlaps(image.Rect(22, 2, 30, 12)) {
		t.Errorf("overflowed: a region in a touched cell does not overlap")
	}
	if s.overlaps(image.Rect(300, 2, 310, 12)) {
		t.Errorf("overflowed: a region in an untouched cell overlaps")
	}

	// A reset set is empty again, and reuses its cells.
	cells := s.cells
	s.reset(64, 64)
	if s.cells != cells || s.overlaps(image.Rect(0, 0, 64, 64)) {
		t.Errorf("after reset: the set is not empty, or its cells were not reused")
	}
	s.add(image.Rect(63, 63, 65, 65))
	if !s.overlaps(image.Rect(63, 63, 64, 64)) {
		t.Errorf("a region at the image's corner does not overlap itself")
	}
}

func TestAppendReplacing(t *testing.T) {
	var args []writePixelsCommandArgs
	var bounds image.Rectangle
	arg := func(r image.Rectangle) writePixelsCommandArgs {
		return writePixelsCommandArgs{pixels: nil, region: r}
	}
	args = appendReplacing(args, &bounds, arg(image.Rect(0, 0, 2, 2)))
	args = appendReplacing(args, &bounds, arg(image.Rect(10, 0, 12, 2)))
	if got, want := bounds, image.Rect(0, 0, 12, 2); got != want {
		t.Errorf("bounds: got %v, want %v", got, want)
	}
	// A write apart from the bounds covers nothing.
	args = appendReplacing(args, &bounds, arg(image.Rect(0, 10, 2, 12)))
	if len(args) != 3 {
		t.Fatalf("len(args): got %d, want 3", len(args))
	}
}
