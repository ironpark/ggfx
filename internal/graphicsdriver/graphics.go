// Copyright 2018 The Ebiten Authors
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

package graphicsdriver

import (
	"fmt"
	"image"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/shader"
)

type DstRegion struct {
	Region     image.Rectangle
	IndexCount int
}

const (
	InvalidImageID  = 0
	InvalidShaderID = 0
)

// FlushMode specifies whether a command batch completes or presents a frame.
type FlushMode int

const (
	// FlushModeIntermediate submits commands without completing the frame.
	FlushModeIntermediate FlushMode = iota
	// FlushModeEndFrame completes the frame without presenting it.
	FlushModeEndFrame
	// FlushModePresent completes and presents the frame.
	FlushModePresent
)

type Graphics interface {
	Initialize() error
	ColorSpace() color.ColorSpace
	Begin() error
	// End ends a command batch with the given flush mode.
	End(mode FlushMode) error
	SetVertices(vertices []float32, indices []uint32) error

	// NewImage creates an offscreen image of the given size and pixel format. A driver is only
	// asked for a format it reports with SupportsPixelFormat.
	NewImage(width, height int, format PixelFormat) (Image, error)

	// SupportsPixelFormat reports whether NewImage can make images of format, with the semantics
	// PixelFormat documents. Every driver supports PixelFormatRGBA8. SupportsPixelFormat is called
	// after Initialize, on the render thread, and its answer must not change afterwards.
	SupportsPixelFormat(format PixelFormat) bool

	// NewSurface creates a presentation target for a native window. target is what the platform's
	// UI layer has: an NSView or an HWND, or an opengl.Presenter for OpenGL.
	// transparent asks for a surface that composites with what is behind the window; a driver
	// that cannot present one returns an error. NewSurface is called on the main thread.
	NewSurface(target any, transparent bool) (Surface, error)
	NeedsClearingScreen() bool
	MaxImageSize() int

	NewShader(program *shader.Program) (Shader, error)

	// DrawTriangles draws an image onto another image with the given parameters.
	DrawTriangles(dst ImageID, srcs [graphics.ShaderSrcImageCount]ImageID, shader ShaderID, dstRegions []DstRegion, indexOffset int, blend Blend, uniforms []uint32) error
}

// NSView is an NSView pointer. The view a Metal surface is made for renders that surface and
// nothing else: the surface becomes its layer.
type NSView uintptr

// HWND is a Win32 window handle.
type HWND uintptr

// Surface is one presentation target: a window's swap chain or layer, or the default framebuffer
// of a GL context. End(FlushModePresent) presents every surface whose screen image was drawn to
// since the previous flush.
type Surface interface {
	// NewScreenImage returns the image that is presented on this surface. Calling it again
	// replaces the previous screen image. NewScreenImage is called on the render thread.
	NewScreenImage(width, height int) (Image, error)
	Dispose()
}

type Resetter interface {
	Reset() error
}

type Image interface {
	ID() ImageID
	Dispose()
	// ReadPixels and WritePixels take pixels in the image's PixelFormat, rows tightly packed:
	// len(Pixels) is BytesPerPixel * Region.Dx() * Region.Dy().
	ReadPixels(args []PixelsArgs) error
	WritePixels(args []PixelsArgs) error
}

type ImageID int

type PixelsArgs struct {
	Pixels []byte
	Region image.Rectangle
}

// PixelFormat is the format of an image's pixels, on the GPU and in ReadPixels and WritePixels.
//
// A screen image is always PixelFormatRGBA8 as far as the rest of ggfx is concerned, whatever the
// surface's native format is.
type PixelFormat int

const (
	// PixelFormatRGBA8 is four 8-bit unsigned normalized channels of premultiplied-alpha RGBA, 4
	// bytes a pixel in R, G, B, A order. Every driver supports it, and it is the zero value.
	PixelFormatRGBA8 PixelFormat = iota

	// PixelFormatA8 is one 8-bit unsigned normalized channel, 1 byte a pixel. It holds coverage:
	// glyph masks, icon masks, path coverage.
	//
	// A driver that supports it must give it these semantics:
	//
	//   - Sampled in any shader, with any filter, a pixel whose stored value is v reads as
	//     (v, v, v, v): premultiplied white of alpha v. Existing shaders then treat an A8 image
	//     exactly as a white-on-transparent RGBA8 one.
	//   - As a render target, a pixel stores the red channel of the blended result. The blend is
	//     the color (RGB) part of the Blend, with the fragment's alpha available as the source
	//     alpha factor. A destination-alpha blend factor reads 1, as the target has no alpha
	//     channel; blends without one (copy, clear, source-over, lighter, destination-in/out) give
	//     the same red as an RGBA8 target would.
	//   - ReadPixels and WritePixels move 1 byte a pixel.
	//
	// The way to do this is an R8 texture whose sampled swizzle is RRRR:
	//
	//   - Metal: MTLPixelFormatR8Unorm with MTLTextureSwizzleChannels{R, R, R, R} (macOS 10.15, iOS 13).
	//   - OpenGL 3.3+ and OpenGL ES 3.0+: GL_R8 with GL_TEXTURE_SWIZZLE_{R,G,B,A} = GL_RED. GL_R8 is
	//     color-renderable there. WebGL has no texture swizzle: unsupported.
	//   - Direct3D 12: DXGI_FORMAT_R8_UNORM, with the shader resource view's
	//     Shader4ComponentMapping mapping all four components to component 0. The render target
	//     view is a plain R8_UNORM one.
	//   - Direct3D 11 has no component mapping and DXGI_FORMAT_A8_UNORM samples as (0, 0, 0, a):
	//     unsupported.
	//
	// Where a driver does not support it, ggfx makes RGBA8 images in its place and expands the
	// pixels on the CPU (see the atlas package), so a driver never sees PixelFormatA8 it did not
	// report.
	PixelFormatA8
)

// BytesPerPixel returns the size of a pixel of the format in bytes.
func (f PixelFormat) BytesPerPixel() int {
	switch f {
	case PixelFormatRGBA8:
		return 4
	case PixelFormatA8:
		return 1
	}
	panic(fmt.Sprintf("graphicsdriver: unknown pixel format %d", f))
}

// String returns the format's name.
func (f PixelFormat) String() string {
	switch f {
	case PixelFormatRGBA8:
		return "RGBA8"
	case PixelFormatA8:
		return "A8"
	}
	return fmt.Sprintf("PixelFormat(%d)", int(f))
}

type Shader interface {
	ID() ShaderID
	Dispose()
}

type ShaderID int

// MinVertexBufferBytes is the room a driver's vertex or index buffer may always keep, however
// little recent frames needed.
const MinVertexBufferBytes = 64 << 10
