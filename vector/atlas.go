// Copyright 2025 The Ebitengine Authors
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

package vector

import (
	"image"
	"math"
	"slices"

	"github.com/ironpark/ggfx"
)

type atlasRegion struct {
	pathIndex   int
	imageIndex  int
	imageBounds image.Rectangle
}

type atlas struct {
	pathRenderingBounds []image.Rectangle
	// contained reports whether a path lies in its rendering bounds, which is when the path is
	// not clipped by the destination or its bounds.
	contained                   []bool
	atlasRegions                []atlasRegion
	pathIndexToAtlasRegionIndex map[int]int
	atlasSizes                  []image.Point
	atlasImages                 []*ggfx.Image
}

func roundUpAtlasSize(size int) int {
	if size < 16 {
		return 16
	}
	return int(math.Ceil(math.Pow(1.5, math.Ceil(math.Log(float64(size))/math.Log(1.5)))))
}

func roundUp16(x int) int {
	return (x + 15) &^ 15
}

func (a *atlas) setPaths(dstBounds image.Rectangle, paths []*Path, bounds []image.Rectangle, antialias bool) {
	// Reset the members.
	a.pathRenderingBounds = slices.Delete(a.pathRenderingBounds, 0, len(a.pathRenderingBounds))
	a.atlasRegions = slices.Delete(a.atlasRegions, 0, len(a.atlasRegions))
	clear(a.pathIndexToAtlasRegionIndex)
	a.atlasSizes = slices.Delete(a.atlasSizes, 0, len(a.atlasSizes))

	if len(paths) == 0 {
		return
	}

	a.pathRenderingBounds = slices.Grow(a.pathRenderingBounds, len(paths))[:len(paths)]
	a.contained = slices.Grow(a.contained[:0], len(paths))[:len(paths)]
	for i, p := range paths {
		pb := p.Bounds()
		b := pb.Intersect(bounds[i]).Intersect(dstBounds)
		a.contained[i] = !b.Empty() && b == pb
		// Round up the size to 16px in order to encourage reusing sub image cache.
		a.pathRenderingBounds[i] = image.Rectangle{
			Min: b.Min,
			Max: b.Min.Add(image.Pt(roundUp16(b.Dx()), roundUp16(b.Dy()))),
		}
		a.atlasRegions = append(a.atlasRegions, atlasRegion{
			pathIndex: i,
		})
	}

	slices.SortFunc(a.atlasRegions, func(ra, rb atlasRegion) int {
		ba := a.pathRenderingBounds[ra.pathIndex]
		bb := a.pathRenderingBounds[rb.pathIndex]
		if ba.Dy() != bb.Dy() {
			return bb.Dy() - ba.Dy()
		}
		if ba.Dx() != bb.Dx() {
			return ba.Dx() - bb.Dx()
		}
		return ra.pathIndex - rb.pathIndex
	})

	if a.pathIndexToAtlasRegionIndex == nil {
		a.pathIndexToAtlasRegionIndex = make(map[int]int, len(a.atlasRegions))
	}
	for i, r := range a.atlasRegions {
		a.pathIndexToAtlasRegionIndex[r.pathIndex] = i
	}

	w, h := dstBounds.Dx(), dstBounds.Dy()
	// For antialiasing, doubled regions in the X direction are used.
	if antialias {
		w *= 2
	}
	// Use 2^n - 1, as a region in internal/atlas has 1px padding.
	maxImageSize := max(4093, w, h)

	// Pack the regions into an atlas with a very simple algorithm:
	// Order the regions by height and then place them in a row.
	var atlasImageCount int
	{
		a.atlasSizes = append(a.atlasSizes, image.Point{})

		var atlasImageIndex int
		var currentRowHeight int
		var currentPosition image.Point
		for i := range a.atlasRegions {
			pb := a.pathRenderingBounds[a.atlasRegions[i].pathIndex]
			// TODO: What if s already exceeds maxImageSize (#3357)?
			s := pb.Size()
			// An additional image for antialiasing must be on the same atlas,
			// so extend the width and use it as a sub image.
			if antialias {
				s.X *= 2
			}
			if i == 0 {
				currentRowHeight = s.Y
			} else if currentPosition.X+s.X > maxImageSize {
				// Try the next row.
				currentPosition.X = 0
				currentPosition.Y += currentRowHeight
				if currentPosition.Y+s.Y > maxImageSize {
					atlasImageIndex++
					a.atlasSizes = append(a.atlasSizes, image.Point{})
					currentPosition.Y = 0
					currentRowHeight = s.Y
				} else {
					currentRowHeight = max(currentRowHeight, s.Y)
				}
			}
			a.atlasRegions[i].imageIndex = atlasImageIndex
			a.atlasRegions[i].imageBounds = image.Rectangle{
				Min: currentPosition,
				Max: currentPosition.Add(s),
			}
			a.atlasSizes[atlasImageIndex] = image.Point{
				X: max(a.atlasSizes[atlasImageIndex].X, a.atlasRegions[i].imageBounds.Max.X),
				Y: max(a.atlasSizes[atlasImageIndex].Y, a.atlasRegions[i].imageBounds.Max.Y),
			}
			currentPosition.X += s.X
		}
		atlasImageCount = atlasImageIndex + 1
	}

	a.atlasImages = slices.Grow(a.atlasImages, atlasImageCount)[:atlasImageCount]
	for i := range a.atlasImages {
		s := a.atlasSizes[i]
		var origWidth, origHeight int
		if a.atlasImages[i] != nil {
			origWidth = a.atlasImages[i].Bounds().Dx()
			origHeight = a.atlasImages[i].Bounds().Dy()
			if origWidth < s.X || origHeight < s.Y {
				a.atlasImages[i].Deallocate()
				a.atlasImages[i] = nil
			}
		}
		if a.atlasImages[i] != nil {
			a.atlasImages[i].Clear()
		} else {
			// Extend the bounds a little bit by roundUpAtlasSize to avoid creating an image too often.
			w := min(maxImageSize, max(roundUpAtlasSize(s.X), origWidth))
			h := min(maxImageSize, max(roundUpAtlasSize(s.Y), origHeight))
			a.atlasImages[i] = ggfx.NewImage(w, h)
		}
	}
}

// stencilBufferImageAt returns the stencil buffer of path i as a sub-image of its atlas image, or
// nil if the path has no region. antialiasIndex selects one of the two halves for antialiasing.
func (a *atlas) stencilBufferImageAt(i int, antialias bool, antialiasIndex int) *ggfx.Image {
	img, b, ok := a.stencilBufferRegionAt(i, antialias, antialiasIndex)
	if !ok {
		return nil
	}
	return img.SubImage(b).(*ggfx.Image)
}

// stencilBufferRegionAt returns the atlas image of path i and the stencil buffer's bounds on it.
// ok is false if the path has no region.
func (a *atlas) stencilBufferRegionAt(i int, antialias bool, antialiasIndex int) (img *ggfx.Image, bounds image.Rectangle, ok bool) {
	idx, ok := a.pathIndexToAtlasRegionIndex[i]
	if !ok {
		return nil, image.Rectangle{}, false
	}
	ar := a.atlasRegions[idx]
	if ar.imageBounds.Empty() {
		return nil, image.Rectangle{}, false
	}

	b := ar.imageBounds
	if antialias {
		switch antialiasIndex {
		case 0:
			b.Max.X = b.Min.X + b.Dx()/2
		case 1:
			b.Min.X += b.Dx() / 2
		default:
			panic("not reached")
		}
	}
	return a.atlasImages[ar.imageIndex], b, true
}

// atlasImageIndexAt returns the index of the atlas image of path i.
func (a *atlas) atlasImageIndexAt(i int) int {
	return a.atlasRegions[a.pathIndexToAtlasRegionIndex[i]].imageIndex
}

// isContained reports whether path i lies in its rendering bounds.
func (a *atlas) isContained(i int) bool {
	return a.contained[i]
}

func (a *atlas) pathRenderingPositionAt(i int) image.Point {
	return a.pathRenderingBounds[i].Min
}
