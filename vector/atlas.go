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
	// atlasRegions are in the order they are laid out in, so the regions of an atlas image are
	// consecutive.
	atlasRegions                []atlasRegion
	pathIndexToAtlasRegionIndex []int
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

// roundUpAtlasSize returns the smallest power of 1.5 not less than size, rounded up, and 16 at least.
func roundUpAtlasSize(size int) int {
	if size < 16 {
		return 16
	}
	// 1.5^k is 3^k/2^k. 3^k fits in 64 bits up to k = 40, which is for sizes up to about 10^7.
	n, d := uint64(1), uint64(1)
	for n < uint64(size)*d {
		n *= 3
		d *= 2
	}
	return int((n + d - 1) / d)
}

func roundUp16(x int) int {
	return (x + 15) &^ 15
}

// setRenderingBounds sets rbs to the rendering bounds of paths, and contained to whether each path
// lies in them, which is when the path is not clipped by the destination or its bounds.
func setRenderingBounds(rbs []image.Rectangle, contained []bool, dstBounds image.Rectangle, paths []*Path, bounds []image.Rectangle) ([]image.Rectangle, []bool) {
	rbs = slices.Grow(rbs[:0], len(paths))[:len(paths)]
	contained = slices.Grow(contained[:0], len(paths))[:len(paths)]
	for i, p := range paths {
		pb := p.Bounds()
		b := pb.Intersect(bounds[i]).Intersect(dstBounds)
		contained[i] = !b.Empty() && b == pb
		// Round up the size to 16px in order to encourage reusing sub image cache.
		rbs[i] = image.Rectangle{
			Min: b.Min,
			Max: b.Min.Add(image.Pt(roundUp16(b.Dx()), roundUp16(b.Dy()))),
		}
	}
	return rbs, contained
}

// layout lays out the paths whose rendering bounds are rbs, but the ones whose slot is not
// transient, which get no region. slots may be nil, for all paths on the atlas.
func (a *atlas) layout(now int64, dstBounds image.Rectangle, rbs []image.Rectangle, antialias bool, slots []stencilSlot) {
	// Reset the members.
	a.atlasRegions = slices.Delete(a.atlasRegions, 0, len(a.atlasRegions))
	a.pathIndexToAtlasRegionIndex = a.pathIndexToAtlasRegionIndex[:0]
	a.atlasSizes = slices.Delete(a.atlasSizes, 0, len(a.atlasSizes))

	n := len(rbs)
	size := func(i int) image.Point {
		if slots != nil && !slots[i].transient {
			return image.Point{}
		}
		return rbs[i].Size()
	}
	var found bool
	for i := range n {
		a.atlasRegions = append(a.atlasRegions, atlasRegion{
			pathIndex: i,
		})
		if s := size(i); s.X > 0 && s.Y > 0 {
			found = true
		}
	}

	w, h := dstBounds.Dx(), dstBounds.Dy()
	// For antialiasing, doubled regions in the X direction are used.
	if antialias {
		w *= 2
	}
	// Use 2^n - 1, as a region in internal/atlas has 1px padding.
	maxImageSize := max(4093, w, h)

	if !found {
		// No image is needed, but the images kept still age.
		a.ensureImages(now, 0, maxImageSize)
		return
	}

	slices.SortFunc(a.atlasRegions, func(ra, rb atlasRegion) int {
		ba := size(ra.pathIndex)
		bb := size(rb.pathIndex)
		if ba.Y != bb.Y {
			return bb.Y - ba.Y
		}
		if ba.X != bb.X {
			return ba.X - bb.X
		}
		return ra.pathIndex - rb.pathIndex
	})

	a.pathIndexToAtlasRegionIndex = slices.Grow(a.pathIndexToAtlasRegionIndex[:0], n)[:n]
	for i, r := range a.atlasRegions {
		a.pathIndexToAtlasRegionIndex[r.pathIndex] = i
	}

	// Pack the regions into an atlas with a very simple algorithm:
	// Order the regions by height and then place them in a row.
	var atlasImageCount int
	{
		a.atlasSizes = append(a.atlasSizes, image.Point{})

		var atlasImageIndex int
		var currentRowHeight int
		var currentPosition image.Point
		for i := range a.atlasRegions {
			// TODO: What if s already exceeds maxImageSize (#3357)?
			s := size(a.atlasRegions[i].pathIndex)
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

// ensureImages makes atlasImages count images at least as large as atlasSizes, up to
// maxImageSize, and clears each within its atlasSizes. An image is reallocated larger when it is
// too small, and smaller when it is far larger than the recent fills needed; the images beyond the
// count recently needed are deallocated.
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
				// Clear only what this batch's buffers take: no draw reads past them, and an
				// image kept large after a large fill would otherwise be cleared whole for
				// every small one.
				sub := img.RecyclableSubImage(image.Rectangle{Max: s})
				clearStencilImage(sub)
				sub.Recycle()
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

// regionAt returns the atlas image of path i and its region on it, which holds its stencil buffers.
// ok is false if the path has no region.
func (a *atlas) regionAt(i int) (img *ggfx.Image, region image.Rectangle, ok bool) {
	if i >= len(a.pathIndexToAtlasRegionIndex) {
		return nil, image.Rectangle{}, false
	}
	ar := a.atlasRegions[a.pathIndexToAtlasRegionIndex[i]]
	if ar.imageBounds.Empty() {
		return nil, image.Rectangle{}, false
	}
	return a.atlasImages[ar.imageIndex], ar.imageBounds, true
}
