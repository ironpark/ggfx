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

	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/mipmap"
)

// quadScratch is what a DrawQuads keeps its lists in.
type quadScratch struct {
	locked  []*Image
	mipmaps []*mipmap.Mipmap
}

var quadScratches = sync.Pool{New: func() any { return new(quadScratch) }}

// DrawQuads draws quads, quad k of four vertices in vertices from srcs[k] within srcRegions[k],
// as one DrawTriangles a quad would with canSkipMipmap, in order: the vertices are in the
// sources' own coordinates. A source may be listed more than once, and none may be i.
func (i *Image) DrawQuads(srcs []*Image, vertices []float32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions []image.Rectangle, shader *Shader) {
	if len(srcs) == 0 {
		return
	}
	s := quadScratches.Get().(*quadScratch)
	defer quadScratches.Put(s)

	// Lock the destination and each source once, in the order of their IDs, as imagesLocker
	// does.
	locked := append(s.locked[:0], i)
	locked = append(locked, srcs...)
	slices.SortFunc(locked, func(a, b *Image) int { return cmp.Compare(a.id, b.id) })
	locked = slices.Compact(locked)
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

	if i.modifyCallback != nil {
		i.modifyCallback()
	}
	i.lastBlend = blend

	mipmaps := s.mipmaps[:0]
	for _, src := range srcs {
		mipmaps = append(mipmaps, src.mipmap)
	}
	i.mipmap.DrawQuads(mipmaps, vertices, blend, dstRegion, srcRegions, shader.shader)
	clear(mipmaps)
	s.mipmaps = mipmaps[:0]
}
