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

package ggfx

import (
	"github.com/ironpark/ggfx/internal/builtinshader"
	"github.com/ironpark/ggfx/internal/graphics"
)

// ImageDraw is one image DrawImages draws.
type ImageDraw struct {
	// Image is the image to draw.
	Image *Image

	// X and Y translate Image before the options' GeoM applies, as GeoM.Translate on an
	// identity matrix concatenated with it would.
	X, Y float64

	// ColorScale scales Image's colors in place of the options' ColorScale.
	ColorScale ColorScale
}

// DrawImages draws each of draws on i, in order, as DrawImage would with options' GeoM after a
// translation by the draw's X and Y, and the draw's ColorScale in place of options'. It is for
// many small images drawn alike, as the glyphs of a text are: images that share an atlas are
// drawn as one, without the cost of a DrawImage each.
//
// Where a draw could not skip mipmaps, each image is drawn with DrawImage.
func (i *Image) DrawImages(draws []ImageDraw, options *DrawImageOptions) {
	i.copyCheck()
	if i.isDisposed() || len(draws) == 0 {
		return
	}
	if options == nil {
		options = &DrawImageOptions{}
	}

	filter := builtinshader.Filter(options.Filter)
	geoM := options.GeoM
	a, b, c, d, _, _ := geoM.elements32()
	det := a*d - b*c
	if det == 0 {
		return
	}
	if !options.DisableMipmaps && !canSkipMipmap(det, filter) {
		op := *options
		for _, dr := range draws {
			op.GeoM.Reset()
			op.GeoM.Translate(dr.X, dr.Y)
			op.GeoM.Concat(options.GeoM)
			op.ColorScale = dr.ColorScale
			i.DrawImage(dr.Image, &op)
		}
		return
	}

	i.flushPending()
	i.updateAccessTime()

	// The translation by (X, Y) and then by geoM, as GeoM.Concat computes it.
	ga, gb, gc, gd := geoM.a_1+1, geoM.b, geoM.c, geoM.d_1+1
	ox, oy := i.adjustPosition(0, 0)
	offsetX, offsetY := float64(ox), float64(oy)
	const quad = 4 * graphics.VertexFloatCount
	vs := i.ensureTmpVertices(quad * len(draws))
	srcs := i.tmpQuadSources[:0]
	regions := i.tmpQuadRegions[:0]
	for k, dr := range draws {
		img := dr.Image
		if img.isDisposed() {
			panic("ggfx: the given image to DrawImages must not be disposed")
		}
		img.flushPending()
		img.updateAccessTime()
		// As DrawImage translates by the offset after the GeoM.
		tx := float64(ga*dr.X) + float64(gb*dr.Y) + geoM.tx + offsetX
		ty := float64(gc*dr.X) + float64(gd*dr.Y) + geoM.ty + offsetY
		bounds := img.Bounds()
		sx0, sy0 := img.adjustPosition(bounds.Min.X, bounds.Min.Y)
		sx1, sy1 := img.adjustPosition(bounds.Max.X, bounds.Max.Y)
		cr, cg, cb, ca := dr.ColorScale.apply(1, 1, 1, 1)
		graphics.QuadVerticesFromSrcAndMatrix(vs[k*quad:], float32(sx0), float32(sy0), float32(sx1), float32(sy1), a, b, c, d, float32(tx), float32(ty), cr, cg, cb, ca)
		srcs = append(srcs, img.image)
		regions = append(regions, img.adjustedBounds())
	}

	shader := builtinShader(filter, builtinshader.AddressUnsafe)
	i.image.DrawQuads(srcs, vs, options.Blend.internalBlend(), i.adjustedBounds(), regions, shader.shader)
	clear(srcs)
	i.tmpQuadSources = srcs[:0]
	i.tmpQuadRegions = regions[:0]
}
