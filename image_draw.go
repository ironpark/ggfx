// Copyright 2014 Hajime Hoshi
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
	"fmt"
	"image"
	"math"
	"unsafe"

	"github.com/ironpark/ggfx/internal/builtinshader"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/render"
)

func canSkipMipmap(det float32, filter builtinshader.Filter) bool {
	if filter != builtinshader.FilterLinear {
		return true
	}
	return math.Abs(float64(det)) >= 0.999
}

// DrawImageOptions represents options for DrawImage.
type DrawImageOptions struct {
	// GeoM is a geometry matrix to draw.
	// The default (zero) value is identity, which draws the image at (0, 0).
	GeoM GeoM

	// ColorScale is a scale of color, applied to premultiplied-alpha colors.
	// The default (zero) value is identity, which is (1, 1, 1, 1).
	ColorScale ColorScale

	// Blend is a blending way of the source color and the destination color.
	// The default (zero) value is the regular alpha blending.
	Blend Blend

	// Filter is a type of texture filter.
	// The default (zero) value is FilterNearest.
	Filter Filter

	// DisableMipmaps disables mipmaps.
	// When Filter is FilterLinear and GeoM shrinks the image, mipmaps are used by default.
	// Mipmap is useful to render a shrunk image with high quality.
	// However, mipmaps can be expensive, especially on mobiles.
	// When DisableMipmaps is true, mipmap is not used.
	// When Filter is not FilterLinear, DisableMipmaps is ignored.
	//
	// The default (zero) value is false.
	DisableMipmaps bool
}

// negZero is -0, the offset that leaves a position untouched bit for bit (see graphics.ConvertVertices).
var negZero = float32(math.Copysign(0, -1))

// vertexFloats returns vertices as floats laid out as graphics.ConvertVertices expects.
func vertexFloats(vertices []Vertex) []float32 {
	if len(vertices) == 0 {
		return nil
	}
	return unsafe.Slice(&vertices[0].DstX, len(vertices)*graphics.VertexFloatCount)
}

// Vertex must consist of exactly graphics.VertexFloatCount floats for vertexFloats.
var _ [unsafe.Sizeof(Vertex{}) - 4*graphics.VertexFloatCount]struct{}

var _ [4*graphics.VertexFloatCount - unsafe.Sizeof(Vertex{})]struct{}

// DrawImage draws the given image on the image i.
//
// DrawImage accepts the options. For details, see the document of
// DrawImageOptions.
//
// For drawing, the pixels of the argument image at the time of this call is
// adopted. Even if the argument image is mutated after this call, the drawing
// result is never affected.
//
// When the given image img is disposed, DrawImage panics.
// When the image i is disposed and img is not, DrawImage does nothing.
//
// When the given image is as same as i, DrawImage panics.
//
// DrawImage works more efficiently as batches
// when the successive calls of DrawImage satisfy the below conditions:
//
//   - All render targets are the same (A in A.DrawImage(B, op))
//   - All Blend values are the same
//   - All Filter values are the same
//
// A whole image and its sub-image are considered to be the same, but some
// environments like browsers might not work efficiently (#2471).
//
// Even when all the above conditions are satisfied, multiple draw commands can
// be used in really rare cases. Ebitengine images usually share an internal
// automatic texture atlas, but when you consume the atlas, or you create a huge
// image, those images cannot be on the same texture atlas. In this case, draw
// commands are separated.
// Another case is when you use an offscreen as a render source. An offscreen
// doesn't share the texture atlas with high probability.
//
// For more performance tips, see https://ebitengine.org/en/documents/performancetips.html
func (i *Image) DrawImage(img *Image, options *DrawImageOptions) {
	i.copyCheck()

	if img.isDisposed() {
		panic("ggfx: the given image to DrawImage must not be disposed")
	}
	if i.isDisposed() {
		return
	}

	i.flushPending()
	img.flushPending()

	i.updateAccessTime()
	img.updateAccessTime()

	if options == nil {
		options = &DrawImageOptions{}
	}

	blend := options.Blend.internalBlend()
	filter := builtinshader.Filter(options.Filter)

	geoM := options.GeoM
	if offsetX, offsetY := i.adjustPosition(0, 0); offsetX != 0 || offsetY != 0 {
		geoM.Translate(float64(offsetX), float64(offsetY))
	}
	a, b, c, d, tx, ty := geoM.elements32()
	det := a*d - b*c
	if det == 0 {
		return
	}

	bounds := img.Bounds()
	sx0, sy0 := img.adjustPosition(bounds.Min.X, bounds.Min.Y)
	sx1, sy1 := img.adjustPosition(bounds.Max.X, bounds.Max.Y)
	cr, cg, cb, ca := options.ColorScale.apply(1, 1, 1, 1)
	vs := i.ensureTmpVertices(4 * graphics.VertexFloatCount)
	graphics.QuadVerticesFromSrcAndMatrix(vs, float32(sx0), float32(sy0), float32(sx1), float32(sy1), a, b, c, d, tx, ty, cr, cg, cb, ca)
	is := graphics.QuadIndices()

	srcs := [graphics.ShaderSrcImageCount]*render.Image{img.image}

	shader := builtinShader(filter, builtinshader.AddressUnsafe)
	i.tmpUniforms = i.tmpUniforms[:0]

	dr := i.adjustedBounds()
	skipMipmap := options.DisableMipmaps
	if !skipMipmap {
		skipMipmap = canSkipMipmap(det, filter)
	}
	i.image.DrawTriangles(srcs, vs, is, blend, dr, [graphics.ShaderSrcImageCount]image.Rectangle{img.adjustedBounds()}, shader.shader, i.tmpUniforms, skipMipmap)
}

// Vertex represents a vertex passed to DrawTriangles.
type Vertex struct {
	// DstX and DstY represents a point on a destination image.
	DstX float32
	DstY float32

	// SrcX and SrcY represents a point on a source image.
	// Be careful that SrcX/SrcY coordinates are on the image's bounds.
	// This means that an upper-left point of a sub-image might not be (0, 0).
	//
	// Before passing vertices to a shader, SrcX/SrcY are converted to texture coordinates of the first image,
	// which is DrawRectShaderOptions.Image[0] or DrawTrianglesShaderOptions.Images[0].
	// If the image is nil, SrcX/SrcY are not converted and used as-is.
	SrcX float32
	SrcY float32

	// ColorR/ColorG/ColorB/ColorA represents color scaling values.
	// Their interpretation depends on the concrete draw call used:
	// - DrawTriangles: straight-alpha or premultiplied-alpha encoded color multiplier.
	//   The format is determined by ColorScaleMode in DrawTrianglesOptions.
	//   If ColorA is 0, the vertex is fully transparent and color is ignored.
	//   If ColorA is 1, the vertex has the color (ColorR, ColorG, ColorB).
	//   Vertex colors are converted to premultiplied-alpha internally and
	//   interpolated linearly respecting alpha.
	// - DrawTrianglesShader: arbitrary floating point values sent to the shader.
	//   These are interpolated linearly and independently of each other.
	ColorR float32
	ColorG float32
	ColorB float32
	ColorA float32

	// Custom0/Custom1/Custom2/Custom3 represents general-purpose values passed to the shader.
	// In order to use them, Fragment must have an additional vec4 argument.
	//
	// These values are valid only when DrawTrianglesShader is used.
	// In other cases, these values are ignored.
	Custom0 float32
	Custom1 float32
	Custom2 float32
	Custom3 float32
}

var _ [0]byte = [unsafe.Sizeof(Vertex{}) - unsafe.Sizeof(float32(0))*graphics.VertexFloatCount]byte{}

// Address represents a sampler address mode.
type Address int

const (
	// AddressUnsafe means there is no guarantee when the texture coordinates are out of range.
	AddressUnsafe Address = Address(builtinshader.AddressUnsafe)

	// AddressClampToZero means that out-of-range texture coordinates return 0 (transparent).
	AddressClampToZero Address = Address(builtinshader.AddressClampToZero)

	// AddressRepeat means that texture coordinates wrap to the other side of the texture.
	AddressRepeat Address = Address(builtinshader.AddressRepeat)
)

// ColorScaleMode is the mode of color scales in vertices.
type ColorScaleMode int

const (
	// ColorScaleModeStraightAlpha indicates color scales in vertices are
	// straight-alpha encoded color multiplier.
	ColorScaleModeStraightAlpha ColorScaleMode = iota

	// ColorScaleModePremultipliedAlpha indicates color scales in vertices are
	// premultiplied-alpha encoded color multiplier.
	ColorScaleModePremultipliedAlpha
)

// DrawTrianglesOptions represents options for DrawTriangles.
type DrawTrianglesOptions struct {

	// ColorScaleMode is the mode of color scales in vertices.
	// ColorScaleMode affects the color calculation with vertex colors, but doesn't affect with a color matrix.
	// The default (zero) value is ColorScaleModeStraightAlpha.
	ColorScaleMode ColorScaleMode

	// Blend is a blending way of the source color and the destination color.
	// The default (zero) value is the regular alpha blending.
	Blend Blend

	// Filter is a type of texture filter.
	// The default (zero) value is FilterNearest.
	Filter Filter

	// Address is a sampler address mode.
	// The default (zero) value is AddressUnsafe.
	Address Address

	// DisableMipmaps disables mipmaps.
	// When Filter is FilterLinear and GeoM shrinks the image, mipmaps are used by default.
	// Mipmap is useful to render a shrunk image with high quality.
	// However, mipmaps can be expensive, especially on mobiles.
	// When DisableMipmaps is true, mipmap is not used.
	// When Filter is not FilterLinear, DisableMipmaps is ignored.
	//
	// The default (zero) value is false.
	DisableMipmaps bool
}

// MaxVertexCount is the maximum number of vertices for DrawTriangles and DrawTrianglesShader.
const MaxVertexCount = graphicscommand.MaxVertexCount

// Index is the type of the vertex indices given to [Image.DrawTriangles] and [Image.DrawTrianglesShader].
type Index interface {
	uint16 | uint32
}

// DrawTriangles draws triangles with the specified vertices and their indices.
//
// The indices are uint16 or uint32 values. uint32 indices are used as they are,
// and uint16 indices are converted to uint32 in a buffer of the image i.
// For a nil slice, specify the index type explicitly like DrawTriangles[uint16].
//
// img is used as a source image. img cannot be nil.
// If you want to draw triangles with a solid color, use a small white image
// and adjust the color elements in the vertices. For an actual implementation,
// see the example 'vector'.
//
// Vertex contains color values, which are interpreted as straight-alpha colors by default.
// This depends on the option's ColorScaleMode.
//
// If len(vertices) is more than MaxVertexCount, the exceeding part is ignored.
//
// If len(indices) is not multiple of 3, DrawTriangles panics.
//
// If a value in indices is out of range of vertices, or not less than MaxVertexCount, DrawTriangles panics.
//
// The rule in which DrawTriangles works effectively is same as DrawImage's.
//
// When the given image is disposed, DrawTriangles panics.
//
// When the image i is disposed and the given image is not, DrawTriangles does nothing.
func (i *Image) DrawTriangles[I Index](vertices []Vertex, indices []I, img *Image, options *DrawTrianglesOptions) {
	i.drawTriangles(vertices, i.indices32(indices), img, options)
}

// indices32 returns indices as uint32 values. uint32 indices are returned as they are; uint16
// indices are converted in i's temporary index buffer.
func (i *Image) indices32[I Index](indices []I) []uint32 {
	if is, ok := any(indices).([]uint32); ok {
		return is
	}
	i.tmpIndices = resizeScratch(i.tmpIndices, len(indices))
	is := i.tmpIndices
	if is16, ok := any(indices).([]uint16); ok {
		graphics.WidenIndices(is, is16)
		return is
	}
	for j, idx := range indices {
		is[j] = uint32(idx)
	}
	return is
}

// checkIndices panics if an index is out of vertexCount vertices.
func checkIndices(indices []uint32, vertexCount int) {
	if graphics.MaxIndex(indices) < uint32(vertexCount) {
		return
	}
	for i, idx := range indices {
		if idx >= uint32(vertexCount) {
			panic(fmt.Sprintf("ggfx: indices[%d] must be less than len(vertices) (%d) but was %d", i, vertexCount, idx))
		}
	}
}

func (i *Image) drawTriangles(vertices []Vertex, indices []uint32, img *Image, options *DrawTrianglesOptions) {
	i.copyCheck()

	if img != nil && img.isDisposed() {
		panic("ggfx: the given image to DrawTriangles must not be disposed")
	}
	if i.isDisposed() {
		return
	}

	if len(indices) == 0 {
		return
	}

	i.flushPending()
	img.flushPending()

	img.updateAccessTime()
	i.updateAccessTime()

	if len(vertices) > graphicscommand.MaxVertexCount {
		// The last part cannot be specified by indices. Just omit them.
		vertices = vertices[:graphicscommand.MaxVertexCount]
	}
	if len(indices)%3 != 0 {
		panic("ggfx: len(indices) % 3 must be 0")
	}
	checkIndices(indices, len(vertices))

	if options == nil {
		options = &DrawTrianglesOptions{}
	}

	blend := options.Blend.internalBlend()

	address := builtinshader.Address(options.Address)
	filter := builtinshader.Filter(options.Filter)

	vs := i.ensureTmpVertices(len(vertices) * graphics.VertexFloatCount)
	dx, dy := i.positionOffsetF32()
	sx, sy := img.positionOffsetF32()
	var flags graphics.ConvertVerticesFlags
	if options.ColorScaleMode == ColorScaleModeStraightAlpha {
		flags |= graphics.ConvertVerticesPremultiplyAlpha
	}
	graphics.ConvertVertices(vs, vertexFloats(vertices), dx, dy, sx, sy, flags)

	srcs := [graphics.ShaderSrcImageCount]*render.Image{img.image}

	shader := builtinShader(filter, address)
	i.tmpUniforms = i.tmpUniforms[:0]

	skipMipmap := options.DisableMipmaps
	if !skipMipmap {
		skipMipmap = filter != builtinshader.FilterLinear
	}
	i.image.DrawTriangles(srcs, vs, indices, blend, i.adjustedBounds(), [graphics.ShaderSrcImageCount]image.Rectangle{img.adjustedBounds()}, shader.shader, i.tmpUniforms, skipMipmap)
}

// DrawTrianglesShaderOptions represents options for DrawTrianglesShader.
type DrawTrianglesShaderOptions struct {

	// Blend is a blending way of the source color and the destination color.
	// The default (zero) value is the regular alpha blending.
	Blend Blend

	// Uniforms is a set of uniform variables for the shader.
	// The keys are the names of the uniform variables.
	// The values must be a numeric/boolean type, or a slice or an array of a numeric/boolean type.
	// If the uniform variable type is an array, a vector or a matrix,
	// you have to specify linearly flattened values as a slice or an array.
	// For example, if the uniform variable type is [4]vec4, the length will be 16.
	//
	// If a uniform variable's name doesn't exist in Uniforms, this is treated as if zero values are specified.
	Uniforms map[string]any

	// UniformBlock is the shader's uniform block, set by name ahead of the draw.
	// It avoids the map lookups and the reflection of Uniforms, so prefer it for a draw repeated every frame.
	// UniformBlock must be created by the shader of the draw with [Shader.NewUniforms].
	// If both Uniforms and UniformBlock are specified, the draw panics.
	//
	// The default (zero) value is nil, which uses Uniforms.
	UniformBlock *Uniforms

	// Images is a set of the source images. The images may have different sizes.
	Images [4]*Image
}

// Check the number of images.
var _ [len(DrawTrianglesShaderOptions{}.Images) - graphics.ShaderSrcImageCount]struct{} = [0]struct{}{}

// DrawTrianglesShader draws triangles with the specified vertices and their indices with the specified shader.
//
// Vertex contains color values, which can be interpreted for any purpose by the shader.
//
// The indices are uint16 or uint32 values, as for [Image.DrawTriangles].
//
// For the details about shaders, see docs/shaders.md.
//
// the size of the image at index 0 of the specified images.
// If the image at index 0 is nil, its size is treated as (0, 0) for this comparison.
// If one of the specified image is non-nil and is disposed, DrawTrianglesShader panics.
//
// If len(vertices) is more than MaxVertexCount, the exceeding part is ignored.
//
// If len(indices) is not multiple of 3, DrawTrianglesShader panics.
//
// If a value in indices is out of range of vertices, or not less than MaxVertexCount, DrawTrianglesShader panics.
//
// When the given shader is disposed, DrawTrianglesShader panics.
//
// When a specified image is non-nil and is disposed, DrawTrianglesShader panics.
//
// If a specified uniform variable's length or type doesn't match with an expected one, DrawTrianglesShader panics.
//
// Even if a result is an invalid color as a premultiplied-alpha color, i.e. an alpha value exceeds other color values,
// the value is kept and is not clamped.
//
// When the image i is disposed and no disposed shader or image is given, DrawTrianglesShader does nothing.
func (i *Image) DrawTrianglesShader[I Index](vertices []Vertex, indices []I, shader *Shader, options *DrawTrianglesShaderOptions) {
	i.drawTrianglesShader(vertices, i.indices32(indices), shader, options)
}

func (i *Image) drawTrianglesShader(vertices []Vertex, indices []uint32, shader *Shader, options *DrawTrianglesShaderOptions) {
	i.copyCheck()

	if shader.isDisposed() {
		panic("ggfx: the given shader to DrawTrianglesShader must not be disposed")
	}

	if options != nil {
		for _, img := range options.Images {
			if img == nil {
				continue
			}
			if img.isDisposed() {
				panic("ggfx: the given image to DrawTrianglesShader must not be disposed")
			}
		}
		checkUniformBlock(shader, options.UniformBlock, options.Uniforms)
	}

	if i.isDisposed() {
		return
	}

	if len(indices) == 0 {
		return
	}

	if options == nil {
		options = &DrawTrianglesShaderOptions{}
	}

	i.flushPending()
	imgs, srcRegions := useShaderSources(&options.Images)
	i.updateAccessTime()

	if len(vertices) > graphicscommand.MaxVertexCount {
		// The last part cannot be specified by indices. Just omit them.
		vertices = vertices[:graphicscommand.MaxVertexCount]
	}
	if len(indices)%3 != 0 {
		panic("ggfx: len(indices) % 3 must be 0")
	}
	checkIndices(indices, len(vertices))

	blend := options.Blend.internalBlend()

	vs := i.ensureTmpVertices(len(vertices) * graphics.VertexFloatCount)
	dx, dy := i.positionOffsetF32()
	sx, sy := negZero, negZero
	if src := options.Images[0]; src != nil {
		sx, sy = src.positionOffsetF32()
	}
	graphics.ConvertVertices(vs, vertexFloats(vertices), dx, dy, sx, sy, graphics.ConvertVerticesCopyCustom)

	uniforms := i.uniforms(shader, options.UniformBlock, options.Uniforms)
	i.image.DrawTriangles(imgs, vs, indices, blend, i.adjustedBounds(), srcRegions, shader.shader, uniforms, true)
}

// useShaderSources flushes the pending work of the source images of a shader draw and marks them
// used, and returns their images and the regions they are drawn from.
func useShaderSources(images *[graphics.ShaderSrcImageCount]*Image) (imgs [graphics.ShaderSrcImageCount]*render.Image, regions [graphics.ShaderSrcImageCount]image.Rectangle) {
	for k, img := range images {
		if img == nil {
			continue
		}
		img.flushPending()
		img.updateAccessTime()
		imgs[k] = img.image
		regions[k] = img.adjustedBounds()
	}
	return imgs, regions
}

// DrawRectShaderOptions represents options for DrawRectShader.
type DrawRectShaderOptions struct {
	// GeoM is a geometry matrix to draw.
	// The default (zero) value is identity, which draws the rectangle at (0, 0).
	GeoM GeoM

	// ColorScale is a scale of color.
	// This scaling values are passed to the shader as v.color in fragment.
	// The default (zero) value is identity, which is (1, 1, 1, 1).
	ColorScale ColorScale

	// Blend is a blending way of the source color and the destination color.
	// The default (zero) value is the regular alpha blending.
	Blend Blend

	// Uniforms is a set of uniform variables for the shader.
	// The keys are the names of the uniform variables.
	// The values must be a numeric/boolean type, or a slice or an array of a numeric/boolean type.
	// If the uniform variable type is an array, a vector or a matrix,
	// you have to specify linearly flattened values as a slice or an array.
	// For example, if the uniform variable type is [4]vec4, the length will be 16.
	//
	// If a uniform variable's name doesn't exist in Uniforms, this is treated as if zero values are specified.
	Uniforms map[string]any

	// UniformBlock is the shader's uniform block, set by name ahead of the draw.
	// It avoids the map lookups and the reflection of Uniforms, so prefer it for a draw repeated every frame.
	// UniformBlock must be created by the shader of the draw with [Shader.NewUniforms].
	// If both Uniforms and UniformBlock are specified, the draw panics.
	//
	// The default (zero) value is nil, which uses Uniforms.
	UniformBlock *Uniforms

	// Images is a set of the source images.
	// All the images' sizes must be the same.
	Images [4]*Image
}

// Check the number of images.
var _ [len(DrawRectShaderOptions{}.Images)]struct{} = [graphics.ShaderSrcImageCount]struct{}{}

// DrawRectShader draws a rectangle with the specified width and height with the specified shader.
//
// For the details about shaders, see docs/shaders.md.
//
// When the given shader is disposed, DrawRectShader panics.
// When one of the specified image is non-nil and its size is different from (width, height), DrawRectShader panics.
// When one of the specified image is non-nil and is disposed, DrawRectShader panics.
//
// If a specified uniform variable's length or type doesn't match with an expected one, DrawRectShader panics.
//
// In a shader, v.src_pos in fragment represents a position in a source image.
// If no source images are specified, v.src_pos represents the position from (0, 0) to (width, height) in pixels,
// and src0_size returns (width, height).
//
// Even if a result is an invalid color as a premultiplied-alpha color, i.e. an alpha value exceeds other color values,
// the value is kept and is not clamped.
//
// When the image i is disposed and no disposed shader or image is given, DrawRectShader does nothing.
func (i *Image) DrawRectShader(width, height int, shader *Shader, options *DrawRectShaderOptions) {
	i.copyCheck()

	if shader.isDisposed() {
		panic("ggfx: the given shader to DrawRectShader must not be disposed")
	}

	if options != nil {
		for _, img := range options.Images {
			if img == nil {
				continue
			}
			if img.isDisposed() {
				panic("ggfx: the given image to DrawRectShader must not be disposed")
			}
		}
		checkUniformBlock(shader, options.UniformBlock, options.Uniforms)
	}

	if i.isDisposed() {
		return
	}

	if options == nil {
		options = &DrawRectShaderOptions{}
	}

	imgs, srcRegions := useShaderSources(&options.Images)
	i.flushPending()
	i.updateAccessTime()

	for _, img := range options.Images {
		if img != nil && img.Bounds().Size() != image.Pt(width, height) {
			panic("ggfx: all the source images must be the same size with the rectangle")
		}
	}
	if options.Images[0] == nil {
		// Give the source size so that users can get the source size via src0_size (#2166).
		srcRegions[0] = image.Rect(0, 0, width, height)
	}

	blend := options.Blend.internalBlend()

	geoM := options.GeoM
	if offsetX, offsetY := i.adjustPosition(0, 0); offsetX != 0 || offsetY != 0 {
		geoM.Translate(float64(offsetX), float64(offsetY))
	}
	a, b, c, d, tx, ty := geoM.elements32()
	if det := a*d - b*c; det == 0 {
		return
	}
	cr, cg, cb, ca := options.ColorScale.elements()
	vs := i.ensureTmpVertices(4 * graphics.VertexFloatCount)

	// Do not use srcRegions[0].Dx() and srcRegions[0].Dy() as these might be empty.
	graphics.QuadVerticesFromSrcAndMatrix(vs,
		float32(srcRegions[0].Min.X), float32(srcRegions[0].Min.Y),
		float32(srcRegions[0].Min.X+width), float32(srcRegions[0].Min.Y+height),
		a, b, c, d, tx, ty, cr, cg, cb, ca)
	is := graphics.QuadIndices()

	uniforms := i.uniforms(shader, options.UniformBlock, options.Uniforms)
	dr := i.adjustedBounds()
	i.image.DrawTriangles(imgs, vs, is, blend, dr, srcRegions, shader.shader, uniforms, true)
}

func (i *Image) ensureTmpVertices(n int) []float32 {
	i.tmpVertices = resizeScratch(i.tmpVertices, n)
	return i.tmpVertices
}

// uniforms returns the user's uniform block for a draw with shader: block's own when it is given,
// or uniforms laid out in i.tmpUniforms otherwise. checkUniformBlock must have accepted block.
func (i *Image) uniforms(shader *Shader, block *Uniforms, uniforms map[string]any) []uint32 {
	if block != nil {
		return block.block
	}
	i.tmpUniforms = shader.appendUniforms(i.tmpUniforms[:0], uniforms)
	return i.tmpUniforms
}

// checkUniformBlock panics if block cannot be drawn with shader. uniforms is the map of the same
// draw options.
func checkUniformBlock(shader *Shader, block *Uniforms, uniforms map[string]any) {
	if block == nil {
		return
	}
	if uniforms != nil {
		panic("ggfx: Uniforms and UniformBlock must not be specified at the same time")
	}
	if block.shader != shader {
		panic("ggfx: UniformBlock must be created by the shader it is drawn with")
	}
}

// resizeScratch returns a slice of length n backed by s's array when it is big enough.
// Otherwise it allocates a new slice without copying s's contents,
// so it is only for buffers whose contents need not be preserved.
func resizeScratch[T any](s []T, n int) []T {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]T, n)
}
