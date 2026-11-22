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

package graphicsdriver

import (
	"image"

	"github.com/ironpark/ggfx/internal/graphics"
)

// Optional capabilities implemented by Graphics drivers.
//
// Centralizing these interfaces in package graphicsdriver defines the exact
// extension contract between the renderer (internal/render, internal/graphicscommand)
// and any backend implementation (Go or native/Zig).

// MainThreadRunner is an optional capability for drivers that need to dispatch
// UI/layer mutations onto the OS main thread synchronously (e.g. Metal during live resize).
type MainThreadRunner interface {
	SetMainThreadRunner(func(func()))
}

// GLContextSetter is an optional capability for desktop OpenGL drivers that require
// a hidden shared drawing context before Initialize.
type GLContextSetter interface {
	SetDrawingContext(interface{ MakeContextCurrent() error })
}

// ESReporter is an optional capability for OpenGL drivers to report whether they
// use OpenGL ES rather than desktop OpenGL.
type ESReporter interface {
	IsES() bool
}

// DirectCompositionReporter is an optional capability for Windows drivers to report
// whether they present via DirectComposition (allowing windows without a redirection surface).
type DirectCompositionReporter interface {
	SupportsDirectComposition() bool
}

// OcclusionReporter is an optional capability for drivers to report whether the last
// presentation targeted an occluded swap chain and returned without waiting for vsync.
type OcclusionReporter interface {
	IsOccluded() bool
}

// ForcedFrameFinisher is an optional capability for pipelined drivers (e.g. DirectX 12)
// to synchronize a frame forced while the main loop is blocked (such as during live resize).
type ForcedFrameFinisher interface {
	FinishForcedFrame() error
}

// FramebufferSizer is an optional capability for drivers whose backing canvas decides
// the drawing framebuffer size (WebGL).
type FramebufferSizer interface {
	ScreenFramebufferSize() (width, height int)
}

// Closer is an optional capability for drivers that support explicit resource teardown
// when the renderer terminates (e.g. native/Zig backends releasing GPU handles).
// Close is called after the render thread has drained all queued commands and exited.
type Closer interface {
	Close() error
}

// Optional capabilities implemented by Surface instances.

// KeptSurface is an optional capability for a surface that can preserve the previously
// presented frame as the starting contents of the next frame instead of clearing it.
type KeptSurface interface {
	// Kept reports whether the next frame starts from the last presented frame,
	// and prepares the surface accordingly.
	Kept() bool
}

// DamagedSurface is an optional capability for a surface that supports partial-frame
// damage hints for presentation or kept-frame copies.
type DamagedSurface interface {
	// SetDamage records the regions (in pixels) changed by the frame being drawn;
	// nil indicates the entire surface may have changed.
	SetDamage(rects []image.Rectangle)
}

// WindowStateSurface is an optional capability for a surface that reacts to window
// lifecycle transitions (live resize, fullscreen, hidden).
type WindowStateSurface interface {
	SetWindowState(liveResizing, fullscreen, hidden bool)
}

// ReadableSurface is an optional capability for a surface whose screen image can be
// sampled as a rendering source within the same frame.
type ReadableSurface interface {
	Readable() bool
}

// Compile-time assertions pinning the POD layout of the preserved internal uniform block
// and vertex format shared across all Go and native drivers.
var (
	_ [0]struct{} = [graphics.ShaderSrcImageCount - 4]struct{}{}
	_ [0]struct{} = [graphics.DstTextureSizeUniformDwordIndex - 0]struct{}{}
	_ [0]struct{} = [graphics.DstRegionOriginUniformDwordIndex - 2]struct{}{}
	_ [0]struct{} = [graphics.DstRegionSizeUniformDwordIndex - 4]struct{}{}
	_ [0]struct{} = [graphics.SourceTextureSizeUniformDwordIndex - 8]struct{}{}
	_ [0]struct{} = [graphics.SourceImageRegionOriginUniformDwordIndex - 24]struct{}{}
	_ [0]struct{} = [graphics.SourceImageRegionSizeUniformDwordIndex - 40]struct{}{}
	_ [0]struct{} = [graphics.ProjectionMatrixUniformDwordIndex - 56]struct{}{}
	_ [0]struct{} = [graphics.PreservedUniformDwordCount - 72]struct{}{}
)
