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
)

const (
	// touchedGridSize is how many cells a side of an image's touchedSet grid has.
	touchedGridSize = 64

	// maxTouchedRegions is the most regions a touchedSet keeps exactly. Past it, the grid alone
	// answers, conservatively.
	maxTouchedRegions = 1024
)

// touchedSet is the set of regions of an image commands read or drew into. It answers whether a
// region overlaps any of them: a grid of cells over the image tells most regions apart cheaply, and
// only a region in a cell something touched is checked against the regions themselves.
type touchedSet struct {
	// cells has a bit for each cell some region overlaps, a row of the grid a word.
	cells *[touchedGridSize]uint64
	// cellW and cellH are the size of a cell in pixels.
	cellW, cellH int

	regions []image.Rectangle
	// overflowed reports whether regions past maxTouchedRegions were not kept.
	overflowed bool
	// empty reports whether nothing was added since the last reset.
	empty bool
}

// reset empties the set, for an image of width x height pixels.
func (t *touchedSet) reset(width, height int) {
	if t.cells == nil {
		t.cells = new([touchedGridSize]uint64)
	} else if !t.empty {
		clear(t.cells[:])
	}
	t.cellW = max((width+touchedGridSize-1)/touchedGridSize, 1)
	t.cellH = max((height+touchedGridSize-1)/touchedGridSize, 1)
	t.regions = t.regions[:0]
	t.overflowed = false
	t.empty = true
}

// cellRange returns the cells r overlaps, as the first and last column and row, or false if it
// overlaps none.
func (t *touchedSet) cellRange(r image.Rectangle) (x0, y0, x1, y1 int, ok bool) {
	r = r.Intersect(image.Rect(0, 0, t.cellW*touchedGridSize, t.cellH*touchedGridSize))
	if r.Empty() {
		return 0, 0, 0, 0, false
	}
	return r.Min.X / t.cellW, r.Min.Y / t.cellH, (r.Max.X - 1) / t.cellW, (r.Max.Y - 1) / t.cellH, true
}

// rowMask returns the bits of the columns x0 to x1.
func rowMask(x0, x1 int) uint64 {
	return (^uint64(0) >> (touchedGridSize - 1 - (x1 - x0))) << x0
}

// add adds r to the set.
func (t *touchedSet) add(r image.Rectangle) {
	x0, y0, x1, y1, ok := t.cellRange(r)
	if !ok {
		return
	}
	t.empty = false
	m := rowMask(x0, x1)
	for y := y0; y <= y1; y++ {
		t.cells[y] |= m
	}
	if len(t.regions) < maxTouchedRegions {
		t.regions = append(t.regions, r)
	} else {
		t.overflowed = true
	}
}

// overlaps reports whether r overlaps a region of the set. Past maxTouchedRegions regions, it may
// report an overlap that is not.
func (t *touchedSet) overlaps(r image.Rectangle) bool {
	if t.empty {
		return false
	}
	x0, y0, x1, y1, ok := t.cellRange(r)
	if !ok {
		return false
	}
	m := rowMask(x0, x1)
	hit := false
	for y := y0; y <= y1; y++ {
		if t.cells[y]&m != 0 {
			hit = true
			break
		}
	}
	if !hit {
		return false
	}
	if t.overflowed {
		return true
	}
	for _, tr := range t.regions {
		if r.Overlaps(tr) {
			return true
		}
	}
	return false
}
