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
	"sync/atomic"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/recent"
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

	// atlasImages are the atlas images the current paths are on: images[:len(atlasImages)].
	atlasImages []*ggfx.Image
	// images are the atlas images kept, which may be more than the current paths need.
	images []*ggfx.Image
	// sizePeaks track the size images[i] recently had to have, and countPeak how many images
	// were recently needed.
	sizePeaks []sizePeak
	countPeak recent.Peak

	// allocations counts the atlas images allocated, for testing.
	allocations int
	// bytes is the size in bytes of the kept atlas images.
	bytes atomic.Int64
}

// The stencil atlas images are sized to what the fills of the last recent.Window to
// 2*recent.Window frames needed. The time is counted in frames, render's ticks, rather than in
// calls of setPaths or in flushes of the command queue: a frame may fill any number of batches,
// and reading pixels flushes the queue, but a window of frames spans about the same time, a second
// or two, however the frames are drawn. A frame with no fill counts too, so the sizes decay while
// the frames fill nothing; no image is given back before the next fill, though.

// sizePeak tracks the size an atlas image recently had to have, per dimension.
type sizePeak struct {
	x, y recent.Peak
}

func (p *sizePeak) advanceTo(now int64) {
	p.x.AdvanceTo(now)
	p.y.AdvanceTo(now)
}

func (p *sizePeak) observe(s image.Point) {
	p.x.Observe(s.X)
	p.y.Observe(s.Y)
}

func (p *sizePeak) peak() image.Point {
	return image.Pt(p.x.Max(), p.y.Max())
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

// setPaths lays out paths in the atlas at time now, in frames.
func (a *atlas) setPaths(now int64, dstBounds image.Rectangle, paths []*Path, bounds []image.Rectangle, antialias bool) {
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

	a.ensureImages(now, atlasImageCount, maxImageSize)
}

// ensureImages makes atlasImages count cleared images at least as large as atlasSizes, up to
// maxImageSize. An image is reallocated larger when it is too small, and smaller when it is far
// larger than the recent fills needed; the images beyond the count recently needed are
// deallocated.
//
// The images are unmanaged. They are render targets, which internal/atlas would put on
// destination pages whose sizes are powers of two, so a large one could hold close to twice its
// size, or up to four times, of GPU memory; and a stencil image sharing a page with the
// destination it is drawn to would make the atlas move one of them. An unmanaged image is
// exactly its size. A stencil image is the only source of the draws reading it, and the stencil
// draws into it are batched per image anyway, so being off the atlas costs no batching.
func (a *atlas) ensureImages(now int64, count int, maxImageSize int) {
	a.countPeak.AdvanceTo(now)
	a.countPeak.Observe(count)
	for len(a.images) < count {
		a.images = append(a.images, nil)
		a.sizePeaks = append(a.sizePeaks, sizePeak{})
	}
	for i := range a.sizePeaks {
		a.sizePeaks[i].advanceTo(now)
	}

	for i := range count {
		// A region larger than maxImageSize is clipped by the image (#3357).
		s := image.Pt(min(a.atlasSizes[i].X, maxImageSize), min(a.atlasSizes[i].Y, maxImageSize))
		a.sizePeaks[i].observe(s)
		peak := a.sizePeaks[i].peak()
		// Extend the bounds a little bit by roundUpAtlasSize to avoid creating an image too often.
		want := image.Point{
			X: min(maxImageSize, roundUpAtlasSize(peak.X)),
			Y: min(maxImageSize, roundUpAtlasSize(peak.Y)),
		}

		img := a.images[i]
		if img != nil {
			cur := img.Bounds().Size()
			switch {
			case cur.X < s.X || cur.Y < s.Y:
				// Too small. Keep a larger dimension, so that sizes alternating between wide
				// and tall ones settle on an image fitting both.
				want = image.Pt(min(maxImageSize, max(want.X, cur.X)), min(maxImageSize, max(want.Y, cur.Y)))
			case cur.X*cur.Y > recent.ShrinkFactor*want.X*want.Y:
				// Far larger than the recent fills needed.
			default:
				img.Clear()
				continue
			}
			img.Deallocate()
		}
		a.images[i] = ggfx.NewImageWithOptions(image.Rectangle{Max: want}, &ggfx.NewImageOptions{Unmanaged: true})
		a.allocations++
	}

	keep := max(count, a.countPeak.Max())
	for i := keep; i < len(a.images); i++ {
		if a.images[i] != nil {
			a.images[i].Deallocate()
			a.images[i] = nil
		}
	}
	a.images = a.images[:min(keep, len(a.images))]
	a.sizePeaks = a.sizePeaks[:len(a.images)]
	a.atlasImages = a.images[:count]

	var bytes int64
	for _, img := range a.images {
		if img != nil {
			s := img.Bounds().Size()
			bytes += 4 * int64(s.X) * int64(s.Y)
		}
	}
	a.bytes.Store(bytes)
}

// dispose deallocates the atlas images.
func (a *atlas) dispose() {
	for i, img := range a.images {
		if img != nil {
			img.Deallocate()
		}
		a.images[i] = nil
	}
	a.images = a.images[:0]
	a.atlasImages = a.atlasImages[:0]
	a.sizePeaks = a.sizePeaks[:0]
	a.bytes.Store(0)
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
