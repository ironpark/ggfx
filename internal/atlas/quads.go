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
	"image"
	"slices"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// quadIndices are the indices of as many quads as the most a draw has had, each quad's four
// vertices following the last's. They only grow, under backendsM.
var quadIndices []uint32

// indicesForQuads returns the indices of n quads.
func indicesForQuads(n int) []uint32 {
	for k := len(quadIndices) / 6; k < n; k++ {
		b := uint32(4 * k)
		quadIndices = append(quadIndices, b, b+1, b+2, b+1, b+2, b+3)
	}
	return quadIndices[:6*n]
}

// DrawQuads draws quads, quad k of four vertices in vertices from srcs[k] within srcRegions[k],
// as one DrawTriangles a quad would, in order. Quads whose sources share a backend are drawn in
// one command, when the shader reads no source region.
func (i *Image) DrawQuads(srcs []*Image, vertices []float32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions []image.Rectangle, shader *Shader) {
	backendsM.Lock()
	defer backendsM.Unlock()

	if !inFrame {
		vs := slices.Clone(vertices)
		ss := slices.Clone(srcs)
		rs := slices.Clone(srcRegions)
		appendDeferred(func() {
			i.drawQuads(ss, vs, blend, dstRegion, rs, shader)
		})
		return
	}

	i.drawQuads(srcs, vertices, blend, dstRegion, srcRegions, shader)
}

// quadBackends is drawQuads' buffer for the backends of the sources, under backendsM.
var quadBackends []*backend

func (i *Image) drawQuads(srcs []*Image, vertices []float32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions []image.Rectangle, shader *Shader) {
	if len(srcs) == 0 {
		return
	}
	backends := quadBackends[:0]
	for _, src := range srcs {
		if src.backend == nil {
			src.allocate(nil, i.imageType == ImageTypeRegular)
		}
		src.backend.sourceInThisFrame = true
		if !slices.Contains(backends, src.backend) {
			backends = append(backends, src.backend)
		}
	}
	quadBackends = backends[:0]

	i.ensureIsolatedFromSource(backends)
	clear(backends)

	for _, src := range srcs {
		if i.backend.backendImage == src.backend.backendImage {
			panic("atlas: Image.DrawQuads: source must be different from the receiver")
		}
	}

	r := i.regionWithPadding()
	dstRegion = dstRegion.Add(r.Min)
	dx, dy := float32(r.Min.X), float32(r.Min.Y)

	const quad = 4 * graphics.VertexFloatCount
	gs := shader.ensureShader()
	merge := !gs.ReadsSourceRegions()
	for k := 0; k < len(srcs); {
		// The quads from k to n come from one backend image.
		n := k + 1
		if merge {
			for n < len(srcs) && srcs[n].backend.backendImage == srcs[k].backend.backendImage {
				n++
			}
		}
		for j := k; j < n; j++ {
			sr := srcs[j].regionWithPadding()
			graphics.TranslateVertices(vertices[j*quad:(j+1)*quad], dx, dy, float32(sr.Min.X), float32(sr.Min.Y))
			src := srcs[j]
			if !src.isOnSourceBackend() && src.canBePutOnAtlas() {
				imagesToPutOnSourceBackend.add(src)
			}
		}
		var imgs [graphics.ShaderSrcImageCount]*graphicscommand.Image
		imgs[0] = srcs[k].backend.backendImage
		var regions [graphics.ShaderSrcImageCount]image.Rectangle
		if sr := srcRegions[k]; !sr.Empty() {
			regions[0] = sr.Add(srcs[k].regionWithPadding().Min)
		}
		i.backend.backendImage.DrawTriangles(imgs, vertices[k*quad:n*quad], indicesForQuads(n-k), blend, dstRegion, regions, gs, nil)
		k = n
	}
}
