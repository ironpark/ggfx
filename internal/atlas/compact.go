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
	compactUsageDivisor     = 4
	compactShrinkDivisor    = 2
	consolidateUsageDivisor = 2
)

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

// minPageSize returns the width and height a source or a destination page starts at.
func minPageSize(source bool) int {
	if source {
		return minSourceSize
	}
	return minDestinationSize
}

// initialPageSize returns the size a new page starts at to hold an image of w x h pixels, its
// padding included: the minimum size, doubled until the image fits. It reports false if the image
// does not fit a page of maxPageSize.
func initialPageSize(source bool, w, h int) (width, height int, ok bool) {
	pageMax := maxPageSize()
	minSize := min(minPageSize(source), pageMax)
	width, height = minSize, minSize
	for w > width {
		if width >= pageMax {
			return 0, 0, false
		}
		width *= 2
	}
	for h > height {
		if height >= pageMax {
			return 0, 0, false
		}
		height *= 2
	}
	return width, height, true
}

// compactPages consolidates sparse sibling pages and compacts any remaining pages that have been
// mostly empty since recent.Window frames before now. It must be called with backendsM locked,
// between the frame's start and its first draw.
func compactPages(now int64) {
	consolidatePages(now)
	// compact changes no backend's place in theBackends.
	for _, b := range theBackends {
		if b.page == nil || b.backendImage == nil || b.screen {
			continue
		}
		area := int64(b.width) * int64(b.height)
		minSize := int64(min(minPageSize(b.source), maxPageSize()))
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

type pageGroupKey struct {
	source bool
	format graphicsdriver.PixelFormat
}

// consolidatePages merges sparse atlas pages of the same (source, format) group into sibling
// pages that already have free space, disposing emptied GPU textures without growing any sibling.
// It must be called with backendsM locked, between the frame's start and its first draw.
func consolidatePages(now int64) {
	var groups map[pageGroupKey][]*backend
	for _, b := range theBackends {
		if b.page == nil || b.backendImage == nil || b.screen {
			continue
		}
		k := pageGroupKey{source: b.source, format: b.format}
		if groups == nil {
			groups = map[pageGroupKey][]*backend{}
		}
		groups[k] = append(groups[k], b)
	}
	if len(groups) == 0 {
		return
	}

	removed := map[*backend]bool{}
	for _, pages := range groups {
		if len(pages) < 2 {
			pages[0].sparseSince = 0
			continue
		}
		for _, b := range pages {
			area := int64(b.width) * int64(b.height)
			if b.usedArea*consolidateUsageDivisor < area {
				if b.sparseSince == 0 {
					b.sparseSince = now
				}
			} else {
				b.sparseSince = 0
			}
		}
		// Try draining the sparsest pages first so the fewest images are moved.
		slices.SortFunc(pages, func(a, b *backend) int {
			return cmp.Compare(a.usedArea, b.usedArea)
		})
		for i, src := range pages {
			if removed[src] || len(src.images) == 0 {
				continue
			}
			if src.sparseSince == 0 || now-src.sparseSince < recent.Window {
				continue
			}
			targets := make([]*backend, 0, len(pages)-1)
			for j, dst := range pages {
				if j == i || removed[dst] || dst.backendImage == nil || dst.page == nil {
					continue
				}
				if dst.page.Overhang() != src.page.Overhang() {
					continue
				}
				targets = append(targets, dst)
			}
			if len(targets) == 0 {
				continue
			}
			if drainPageInto(src, targets) {
				removed[src] = true
			} else {
				src.sparseSince = now
			}
		}
	}

	if len(removed) == 0 {
		return
	}
	theBackends = slices.DeleteFunc(theBackends, func(b *backend) bool {
		return removed[b]
	})
}

// drainPageInto tries to relocate every image on src into existing free space on targets without
// extending any target page. On success it copies all pixels, updates image ownership, disposes
// src's GPU texture, and returns true; on failure it rolls back all tentative allocations.
func drainPageInto(src *backend, targets []*backend) bool {
	imgs := slices.Clone(src.images)
	slices.SortFunc(imgs, func(a, b *imageImpl) int {
		ra, rb := a.node.Region(), b.node.Region()
		return cmp.Or(
			cmp.Compare(rb.Dy(), ra.Dy()),
			cmp.Compare(rb.Dx(), ra.Dx()),
			cmp.Compare(ra.Min.Y, rb.Min.Y),
			cmp.Compare(ra.Min.X, rb.Min.X),
		)
	})

	type placement struct {
		img  *imageImpl
		dst  *backend
		node *packing.Node
	}
	placed := make([]placement, 0, len(imgs))
	for _, img := range imgs {
		r := img.node.Region()
		var chosenDst *backend
		var chosenNode *packing.Node
		for _, dst := range targets {
			if n := dst.page.AllocNoExtend(r.Dx(), r.Dy()); n != nil {
				chosenDst = dst
				chosenNode = n
				break
			}
		}
		if chosenNode == nil {
			for _, pl := range placed {
				pl.dst.page.Free(pl.node)
			}
			return false
		}
		placed = append(placed, placement{img: img, dst: chosenDst, node: chosenNode})
	}

	// Copy images grouped by destination page in one batched draw per destination.
	byDst := map[*backend][]placement{}
	for _, pl := range placed {
		byDst[pl.dst] = append(byDst[pl.dst], pl)
	}
	quad := graphics.QuadIndices()
	sr := image.Rect(0, 0, src.width, src.height)
	srcs := [graphics.ShaderSrcImageCount]*graphicscommand.Image{src.backendImage}
	for dst, list := range byDst {
		vs := make([]float32, 4*graphics.VertexFloatCount*len(list))
		is := make([]uint32, 6*len(list))
		for k, pl := range list {
			s := pl.img.node.Region().Min
			d := pl.node.Region().Min
			w, h := float32(pl.img.width), float32(pl.img.height)
			graphics.QuadVerticesFromDstAndSrc(
				vs[4*graphics.VertexFloatCount*k:4*graphics.VertexFloatCount*(k+1)],
				float32(d.X), float32(d.Y), float32(d.X)+w, float32(d.Y)+h,
				float32(s.X), float32(s.Y), float32(s.X)+w, float32(s.Y)+h,
				1, 1, 1, 1,
			)
			for j, idx := range quad {
				is[6*k+j] = uint32(4*k) + idx
			}
		}
		dr := image.Rect(0, 0, dst.width, dst.height)
		dst.backendImage.DrawTriangles(srcs, vs, is, graphicsdriver.BlendCopy, dr, [graphics.ShaderSrcImageCount]image.Rectangle{sr}, NearestFilterShader().ensureShader(), nil)
	}

	for _, pl := range placed {
		pl.img.backend = pl.dst
		pl.img.node = pl.node
		pl.dst.addImage(pl.img)
		pl.dst.lowSince = 0
		pl.dst.sparseSince = 0
	}
	src.images = nil
	src.usedArea = 0
	src.backendImage.Dispose()
	src.backendImage = nil
	return true
}

// compact packs b's images anew on a page of their own size, if that is at most
// 1/compactShrinkDivisor of b's, and copies them there; otherwise it leaves b as it is.
func (b *backend) compact() {
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
	var maxW, maxH int
	for _, i := range imgs {
		r := i.node.Region()
		maxW, maxH = max(maxW, r.Dx()), max(maxH, r.Dy())
	}
	width, height, ok := initialPageSize(b.source, maxW, maxH)
	if !ok {
		return
	}
	// The new page has the old one's overhang: the padding of the images on it.
	page := packing.NewPage(width, height, maxPageSize(), b.page.Overhang())
	nodes := make([]*packing.Node, len(imgs))
	for k, i := range imgs {
		r := i.node.Region()
		n := page.Alloc(r.Dx(), r.Dy())
		if n == nil {
			return
		}
		nodes[k] = n
	}
	newW, newH := page.Size()
	if int64(newW)*int64(newH)*compactShrinkDivisor > int64(b.width)*int64(b.height) {
		return
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
	newImg.DrawTriangles(srcs, vs, is, graphicsdriver.BlendCopy, dr, [graphics.ShaderSrcImageCount]image.Rectangle{sr}, NearestFilterShader().ensureShader(), nil)

	b.backendImage.Dispose()
	b.backendImage = newImg
	b.page = page
	b.width = newW
	b.height = newH
	for k, i := range imgs {
		i.node = nodes[k]
	}
	// The images keep their places in b.images, which imgs is a sorted copy of.
}
