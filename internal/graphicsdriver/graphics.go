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
	NewImage(width, height int) (Image, error)

	// NewSurface creates a presentation target for a native window. target is what the platform's
	// UI layer has: an NSWindow or HWND handle as uintptr, or an opengl.Presenter for OpenGL.
	// transparent asks for a surface that composites with what is behind the window; a driver
	// that cannot present one returns an error. NewSurface is called on the main thread.
	NewSurface(target any, transparent bool) (Surface, error)
	SetVsyncEnabled(enabled bool)
	NeedsClearingScreen() bool
	MaxImageSize() int

	NewShader(program *shader.Program) (Shader, error)

	// DrawTriangles draws an image onto another image with the given parameters.
	DrawTriangles(dst ImageID, srcs [graphics.ShaderSrcImageCount]ImageID, shader ShaderID, dstRegions []DstRegion, indexOffset int, blend Blend, uniforms []uint32) error
}

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
	ReadPixels(args []PixelsArgs) error
	WritePixels(args []PixelsArgs) error
}

type ImageID int

type PixelsArgs struct {
	Pixels []byte
	Region image.Rectangle
}

type Shader interface {
	ID() ShaderID
	Dispose()
}

type ShaderID int
