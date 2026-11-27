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

package render

import (
	"cmp"
	"image"
	"slices"
	"sync"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// quadScratch is what a DrawQuads keeps its lists in.
type quadScratch struct {
	locked   []*Image
	imgs     []*atlas.Image
	vertices []float32
	regions  []image.Rectangle
}

var quadScratches = sync.Pool{New: func() any { return new(quadScratch) }}

// DrawQuads draws quads, quad k of four vertices in vertices from srcs[k] within srcRegions[k],
// as one DrawTriangles a quad would with canSkipMipmap, in order: the vertices are in the
// sources' own coordinates. A source may be listed more than once, and none may be i.
func (i *Image) DrawQuads(srcs []*Image, vertices []float32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions []image.Rectangle, shader *atlas.Shader) {
	if len(srcs) == 0 {
		return
	}
	s := quadScratches.Get().(*quadScratch)
	defer quadScratches.Put(s)

	// Lock the destination and each source once, in the order of their IDs, as imagesLocker
	// does. Deduplicate consecutive identical sources (the common case for glyph/sprite batches)
	// before sorting so single-atlas batches skip slices.SortFunc.
	locked := append(s.locked[:0], i)
	var prev *Image
	for _, src := range srcs {
		if src == i {
			clear(locked)
			s.locked = locked[:0]
			panic("render: Image.DrawQuads: source images must be different from the receiver")
		}
		if src != prev {
			locked = append(locked, src)
			prev = src
		}
	}
	if len(locked) == 2 {
		if locked[0].id > locked[1].id {
			locked[0], locked[1] = locked[1], locked[0]
		}
	} else if len(locked) > 2 {
		slices.SortFunc(locked, func(a, b *Image) int { return cmp.Compare(a.id, b.id) })
		locked = slices.Compact(locked)
	}
	for _, img := range locked {
		img.mu.Lock()
	}
	defer func() {
		for _, img := range slices.Backward(locked) {
			img.mu.Unlock()
		}
		clear(locked)
		s.locked = locked[:0]
	}()

	if i.disposed.Load() {
		return
	}

	var anyDisposed bool
	for _, img := range locked {
		if img != i && img.disposed.Load() {
			anyDisposed = true
			break
		}
	}

	imgs := s.imgs[:0]
	verts := vertices
	regs := srcRegions
	if anyDisposed {
		const quad = 4 * graphics.VertexFloatCount
		verts = s.vertices[:0]
		regs = s.regions[:0]
		for k, src := range srcs {
			if src.disposed.Load() {
				continue
			}
			imgs = append(imgs, src.img)
			verts = append(verts, vertices[k*quad:(k+1)*quad]...)
			regs = append(regs, srcRegions[k])
		}
		if len(imgs) == 0 {
			s.imgs = imgs[:0]
			s.vertices = verts[:0]
			s.regions = regs[:0]
			return
		}
	} else {
		for _, src := range srcs {
			imgs = append(imgs, src.img)
		}
	}

	i.lastBlend = blend

	// As in drawTriangles, pending pixel writes go to the GPU first—once per unique live image.
	for _, img := range locked {
		if !img.disposed.Load() {
			img.cache.writeBackPixelsIfNeeded(img.img)
		}
	}

	defer func() {
		clear(imgs)
		s.imgs = imgs[:0]
		if anyDisposed {
			if cap(verts) > 65536 {
				s.vertices = nil
			} else {
				s.vertices = verts[:0]
			}
			if cap(regs) > 2048 {
				s.regions = nil
			} else {
				s.regions = regs[:0]
			}
		}
	}()
	i.img.DrawQuads(imgs, verts, blend, dstRegion, regs, shader)

	i.cache.reset()
	i.markLevelsDirty()
}
