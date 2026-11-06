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

package atlas

import (
	"cmp"
	"image"
	"slices"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/packing"
	"github.com/ironpark/ggfx/internal/recent"
)

// A page grows as images come, but gives its room back only once all of them have gone. A page
// whose images use less than 1/compactUsageDivisor of it for recent.Window frames on end is
// compacted: its images are copied, packed anew, to a page of their own size, which must be at
// most 1/compactShrinkDivisor of the old one, and the old page is disposed of.
//
// Pages are compacted only at the start of a frame, before anything is drawn in it, so no image is
// in the middle of being drawn from or into. A page compacted moves no image to another page: the
// backend keeps its images, with a new texture and new places on it.
const (
	compactUsageDivisor  = 4
	compactShrinkDivisor = 2
)

// frameCount counts the frames begun, the clock pages are compacted by.
var frameCount int64

// addImage records that i was put on the page b.
func (b *backend) addImage(i *imageImpl) {
	i.pageIndex = len(b.images)
	b.images = append(b.images, i)
	r := i.node.Region()
	b.usedArea += int64(r.Dx()) * int64(r.Dy())
}

// removeImage records that i left the page b.
func (b *backend) removeImage(i *imageImpl) {
	k := i.pageIndex
	if k >= len(b.images) || b.images[k] != i {
		return
	}
	last := len(b.images) - 1
	b.images[k] = b.images[last]
	b.images[k].pageIndex = k
	b.images[last] = nil
	b.images = b.images[:last]
	r := i.node.Region()
	b.usedArea -= int64(r.Dx()) * int64(r.Dy())
}

// replaceImage records that the image i on the page b is now new, i's copy, which has i's
// pageIndex (see moveTo).
func (b *backend) replaceImage(i, new *imageImpl) {
	if k := i.pageIndex; k < len(b.images) && b.images[k] == i {
		b.images[k] = new
	}
}

// minPageSize returns the width and height a page like b starts at.
func (b *backend) minPageSize() int {
	if b.source {
		return minSourceSize
	}
	return minDestinationSize
}

// compactPages compacts the pages that have been mostly empty since recent.Window frames before
// now. It must be called with backendsM locked, between the frame's start and its first draw.
func compactPages(now int64) {
	// compact changes no backend's place in theBackends.
	for _, b := range theBackends {
		if b.page == nil || b.backendImage == nil || b.screen {
			continue
		}
		area := int64(b.width) * int64(b.height)
		minSize := int64(b.minPageSize())
		if b.usedArea*compactUsageDivisor >= area || area <= minSize*minSize {
			b.lowSince = 0
			continue
		}
		if b.lowSince == 0 {
			b.lowSince = now
			continue
		}
		if now-b.lowSince < recent.Window {
			continue
		}
		// Whether or not the page is compacted, look at it again only a window later.
		b.lowSince = now
		b.compact()
	}
}

// compact packs b's images anew on a page of their own size, if that is at most
// 1/compactShrinkDivisor of b's, and copies them there.
func (b *backend) compact() bool {
	imgs := slices.Clone(b.images)
	// Pack the tallest first, then the widest; break ties by the places on the old page, so that
	// the order does not depend on the order the images came in.
	slices.SortFunc(imgs, func(a, b *imageImpl) int {
		ra, rb := a.node.Region(), b.node.Region()
		return cmp.Or(
			cmp.Compare(rb.Dy(), ra.Dy()),
			cmp.Compare(rb.Dx(), ra.Dx()),
			cmp.Compare(ra.Min.Y, rb.Min.Y),
			cmp.Compare(ra.Min.X, rb.Min.X),
		)
	})

	// Start as allocate does for the first image, at the minimum size grown to fit the largest.
	width, height := b.minPageSize(), b.minPageSize()
	for _, i := range imgs {
		r := i.node.Region()
		for r.Dx() > width && width < maxSize {
			width *= 2
		}
		for r.Dy() > height && height < maxSize {
			height *= 2
		}
	}
	// The new page has the old one's overhang: the padding of the images on it.
	page := packing.NewPage(width, height, maxSize, b.page.Overhang())
	nodes := make([]*packing.Node, len(imgs))
	for k, i := range imgs {
		r := i.node.Region()
		n := page.Alloc(r.Dx(), r.Dy())
		if n == nil {
			return false
		}
		nodes[k] = n
	}
	newW, newH := page.Size()
	if int64(newW)*int64(newH)*compactShrinkDivisor > int64(b.width)*int64(b.height) {
		return false
	}

	newImg := graphicscommand.NewImageWithFormat(newW, newH, b.format, "")
	clearImage(newImg, image.Rect(0, 0, newW, newH))

	// Copy every image, without its padding, which the new page has cleared, in one draw.
	vs := make([]float32, 4*graphics.VertexFloatCount*len(imgs))
	is := make([]uint32, 6*len(imgs))
	quad := graphics.QuadIndices()
	for k, i := range imgs {
		s := i.node.Region().Min
		d := nodes[k].Region().Min
		w, h := float32(i.width), float32(i.height)
		graphics.QuadVerticesFromDstAndSrc(vs[4*graphics.VertexFloatCount*k:4*graphics.VertexFloatCount*(k+1)],
			float32(d.X), float32(d.Y), float32(d.X)+w, float32(d.Y)+h,
			float32(s.X), float32(s.Y), float32(s.X)+w, float32(s.Y)+h,
			1, 1, 1, 1)
		for j, idx := range quad {
			is[6*k+j] = uint32(4*k) + idx
		}
	}
	srcs := [graphics.ShaderSrcImageCount]*graphicscommand.Image{b.backendImage}
	dr := image.Rect(0, 0, newW, newH)
	sr := image.Rect(0, 0, b.width, b.height)
	newImg.DrawTriangles(srcs, vs, is, graphicsdriver.BlendCopy, dr, [graphics.ShaderSrcImageCount]image.Rectangle{sr}, NearestFilterShader.ensureShader(), nil)

	b.backendImage.Dispose()
	b.backendImage = newImg
	b.page = page
	b.width = newW
	b.height = newH
	b.restoreInfo = restoreInfo{}
	for k, i := range imgs {
		i.node = nodes[k]
	}
	// The images keep their places in b.images, which imgs is a sorted copy of.
	return true
}
