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

// Package fakedriver is a graphics driver for tests of the remote driver: it draws nothing, keeps
// the pixels written to its images, logs every call in a form two runs can be compared by, counts
// what is alive, and panics on any call a real driver could not take, which is what a replay's
// checks must keep from it.
package fakedriver

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"unsafe"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/shader"
)

// MaxImageSize is the largest image the driver makes.
const MaxImageSize = 4096

// Graphics is the fake driver.
type Graphics struct {
	// Log is every call made, ids and uniform versions numbered in the order they first appear.
	Log []string

	next      int
	images    int
	shaders   int
	vertices  int
	indices   int
	versions  map[uint64]int
	nextBlock int
}

var _ graphicsdriver.Graphics = (*Graphics)(nil)

// New returns a fake driver.
func New() *Graphics {
	return &Graphics{versions: map[uint64]int{}}
}

// Live returns how many images and shaders are alive.
func (g *Graphics) Live() (images, shaders int) { return g.images, g.shaders }

func (g *Graphics) logf(format string, args ...any) {
	g.Log = append(g.Log, fmt.Sprintf(format, args...))
}

func hash(b []byte) string {
	s := sha256.Sum256(b)
	return fmt.Sprintf("%x", s[:6])
}

func (g *Graphics) Initialize() error                       { return nil }
func (g *Graphics) ColorSpace() color.ColorSpace            { return color.ColorSpaceSRGB }
func (g *Graphics) Begin() error                            { return nil }
func (g *Graphics) End(mode graphicsdriver.FlushMode) error { return nil }
func (g *Graphics) NeedsClearingScreen() bool               { return false }
func (g *Graphics) MaxImageSize() int                       { return MaxImageSize }
func (g *Graphics) FlipsY() bool                            { return false }
func (g *Graphics) SupportsPixelFormat(f graphicsdriver.PixelFormat) bool {
	return f <= graphicsdriver.PixelFormatA8
}

func (g *Graphics) NewSurface(target any, transparent bool) (graphicsdriver.Surface, error) {
	return nil, errors.New("fakedriver: no surfaces")
}

func (g *Graphics) SetVertices(vertices []float32, indices []uint32) error {
	if len(vertices) == 0 || len(indices) == 0 || len(vertices)%graphics.VertexFloatCount != 0 {
		panic(fmt.Sprintf("fakedriver: SetVertices of %d floats and %d indices", len(vertices), len(indices)))
	}
	n := uint32(len(vertices) / graphics.VertexFloatCount)
	for _, i := range indices {
		if i >= n {
			panic(fmt.Sprintf("fakedriver: index %d past %d vertices", i, n))
		}
	}
	g.vertices, g.indices = int(n), len(indices)
	g.logf("SetVertices %d %d %s %s", n, len(indices),
		hash(unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), 4*len(vertices))),
		hash(unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), 4*len(indices))))
	return nil
}

// NewImage makes an image.
func (g *Graphics) NewImage(width, height int, format graphicsdriver.PixelFormat) (graphicsdriver.Image, error) {
	return g.newImage(width, height, format), nil
}

// NewTarget makes an image as a viewer's target for a remote screen is: like any image.
func (g *Graphics) NewTarget(width, height int) *Image {
	return g.newImage(width, height, graphicsdriver.PixelFormatRGBA8)
}

func (g *Graphics) newImage(width, height int, format graphicsdriver.PixelFormat) *Image {
	if width < 1 || height < 1 || width > MaxImageSize || height > MaxImageSize || !g.SupportsPixelFormat(format) {
		panic(fmt.Sprintf("fakedriver: NewImage %dx%d of format %d", width, height, format))
	}
	g.next++
	g.images++
	i := &Image{g: g, id: g.next, width: width, height: height, format: format,
		pixels: make([]byte, format.BytesPerPixel()*graphics.InternalImageSize(width)*graphics.InternalImageSize(height))}
	g.logf("NewImage #%d %dx%d %s", i.id, width, height, format)
	return i
}

func (g *Graphics) NewShader(program *shader.Program) (graphicsdriver.Shader, error) {
	g.next++
	g.shaders++
	s := &Shader{g: g, id: g.next, program: program}
	g.logf("NewShader #%d textures %d uniforms %d", s.id, program.TextureCount, program.UniformDwordCount)
	return s, nil
}

func (g *Graphics) block(v uint64) int {
	if v == 0 {
		panic("fakedriver: uniform version 0")
	}
	n, ok := g.versions[v]
	if !ok {
		g.nextBlock++
		n = g.nextBlock
		g.versions[v] = n
	}
	return n
}

func (g *Graphics) DrawTriangles(dst graphicsdriver.Image, srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image, sh graphicsdriver.Shader, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32, versions graphicsdriver.UniformVersions) error {
	d := g.alive(dst)
	var ids [graphics.ShaderSrcImageCount]int
	for k, src := range srcs {
		if src == nil {
			continue
		}
		s := g.alive(src)
		if s == d {
			panic("fakedriver: an image drawn to itself")
		}
		ids[k] = s.id
	}
	s, ok := sh.(*Shader)
	if !ok || s.g != g || s.disposed {
		panic("fakedriver: a shader not alive")
	}
	n := indexOffset
	for _, r := range dstRegions {
		if !r.Region.In(d.bounds()) || r.IndexCount < 0 {
			panic(fmt.Sprintf("fakedriver: a draw to %v of %d indices, in an image of %dx%d", r.Region, r.IndexCount, d.width, d.height))
		}
		n += r.IndexCount
	}
	if indexOffset < 0 || n > g.indices {
		panic(fmt.Sprintf("fakedriver: a draw of indices %d to %d of %d", indexOffset, n, g.indices))
	}
	if blend.BlendFactorSourceRGB > graphicsdriver.BlendFactorSourceAlphaSaturated ||
		blend.BlendFactorSourceAlpha > graphicsdriver.BlendFactorSourceAlphaSaturated ||
		blend.BlendFactorDestinationRGB > graphicsdriver.BlendFactorSourceAlphaSaturated ||
		blend.BlendFactorDestinationAlpha > graphicsdriver.BlendFactorSourceAlphaSaturated ||
		blend.BlendOperationRGB > graphicsdriver.BlendOperationMax ||
		blend.BlendOperationAlpha > graphicsdriver.BlendOperationMax {
		panic(fmt.Sprintf("fakedriver: blend %v", blend))
	}
	if len(uniforms) != graphics.PreservedUniformDwordCount+s.program.UniformDwordCount {
		panic(fmt.Sprintf("fakedriver: %d dwords of uniforms", len(uniforms)))
	}
	g.logf("DrawTriangles #%d %v #%d %v %d %v %s %d %d", d.id, ids, s.id, dstRegions, indexOffset, blend,
		hash(unsafe.Slice((*byte)(unsafe.Pointer(&uniforms[0])), 4*len(uniforms))), g.block(versions.Internal), g.block(versions.User))
	return nil
}

func (g *Graphics) alive(i graphicsdriver.Image) *Image {
	img, ok := i.(*Image)
	if !ok || img.g != g || img.disposed {
		panic("fakedriver: an image not alive")
	}
	return img
}

// Image is an image of the fake driver's, which keeps its pixels.
type Image struct {
	g             *Graphics
	id            int
	width, height int
	format        graphicsdriver.PixelFormat
	pixels        []byte
	disposed      bool
}

// bounds returns the pixels of the image's texture, which is of its internal size, as a real
// driver's is.
func (i *Image) bounds() image.Rectangle {
	return image.Rect(0, 0, graphics.InternalImageSize(i.width), graphics.InternalImageSize(i.height))
}

// Size returns the image's size.
func (i *Image) Size() (int, int) { return i.width, i.height }

func (i *Image) Dispose() {
	i.g.alive(i)
	i.disposed = true
	i.g.images--
	i.g.logf("DisposeImage #%d", i.id)
}

func (i *Image) check(args []graphicsdriver.PixelsArgs) {
	i.g.alive(i)
	for _, a := range args {
		if !a.Region.In(i.bounds()) || a.Region.Empty() ||
			len(a.Pixels) != i.format.BytesPerPixel()*a.Region.Dx()*a.Region.Dy() {
			panic(fmt.Sprintf("fakedriver: %d bytes of pixels at %v in an image of %dx%d", len(a.Pixels), a.Region, i.width, i.height))
		}
	}
}

func (i *Image) WritePixels(args []graphicsdriver.PixelsArgs) error {
	i.check(args)
	bpp := i.format.BytesPerPixel()
	for _, a := range args {
		r := a.Region
		for y := r.Min.Y; y < r.Max.Y; y++ {
			copy(i.pixels[bpp*(y*i.bounds().Dx()+r.Min.X):], a.Pixels[bpp*(y-r.Min.Y)*r.Dx():bpp*(y-r.Min.Y+1)*r.Dx()])
		}
		i.g.logf("WritePixels #%d %v %s", i.id, r, hash(a.Pixels))
	}
	return nil
}

func (i *Image) ReadPixels(args []graphicsdriver.PixelsArgs) error {
	i.check(args)
	bpp := i.format.BytesPerPixel()
	for _, a := range args {
		r := a.Region
		for y := r.Min.Y; y < r.Max.Y; y++ {
			copy(a.Pixels[bpp*(y-r.Min.Y)*r.Dx():bpp*(y-r.Min.Y+1)*r.Dx()], i.pixels[bpp*(y*i.bounds().Dx()+r.Min.X):])
		}
		i.g.logf("ReadPixels #%d %v", i.id, r)
	}
	return nil
}

// Shader is a shader of the fake driver's.
type Shader struct {
	g        *Graphics
	id       int
	program  *shader.Program
	disposed bool
}

func (s *Shader) Dispose() {
	if s.disposed {
		panic("fakedriver: a shader disposed twice")
	}
	s.disposed = true
	s.g.shaders--
	s.g.logf("DisposeShader #%d", s.id)
}
