// Copyright 2026 The Ebiten Authors
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
	"github.com/ironpark/ggfx/internal/render"
)

// This file is the renderer's API for a window system: it initializes the renderer, makes
// surfaces of the windows' native views, and draws and presents frames on them. ggwd
// (github.com/ironpark/ggwd) is such a window system.
//
// A frame of a window system's loop goes:
//
//	ggfx.AdvanceTick()
//	if err := ggfx.BeginFrame(); err != nil { ... }
//	screen := surface.Screen(width, height)
//	// Draw to screen.
//	if err := ggfx.EndFrame(); err != nil { ... }
//	// BeginFrame to EndFrame again for every other surface.
//	if err := ggfx.Flush(true); err != nil { ... }

// InitOptions are what [Init] is given.
type InitOptions struct {
	// GraphicsLibrary is the graphics library to draw with. GraphicsLibraryAuto, the default,
	// chooses one, and the GGFX_GRAPHICS_LIBRARY environment variable can name one then.
	GraphicsLibrary GraphicsLibrary

	// ColorSpace is the color space of the screens. It is honored by Metal and WebGL.
	ColorSpace ColorSpace

	// SingleThread runs the graphics commands on the goroutine that flushes them, instead of on
	// a thread of the renderer's own. The browser always runs them so.
	SingleThread bool

	// MainThread runs a function on the OS main thread and returns when it has run. Metal sets
	// its layer's properties through it while a window is resized by the user. Nil runs the
	// function on the calling goroutine, which is right when the loop runs on the main thread.
	MainThread func(func())

	// Canvas is the browser's canvas, a js.Value, which is the one surface WebGL draws to. It is
	// ignored elsewhere.
	Canvas any
}

// Init creates the graphics driver and starts the render thread. It is called once, on the main
// thread, before anything is rendered. Images can be made and drawn to before Init, but their
// draws run in the first frame after it.
func Init(options *InitOptions) error {
	if options == nil {
		options = &InitOptions{}
	}
	return render.Init(&render.Options{
		GraphicsLibrary: render.GraphicsLibrary(options.GraphicsLibrary),
		ColorSpace:      toRenderColorSpace(options.ColorSpace),
		SingleThread:    options.SingleThread,
		MainThread:      options.MainThread,
		Canvas:          options.Canvas,
	})
}

// Terminate waits for the queued graphics commands to run and stops the render thread. A window
// system calls it before it destroys the windows and the OpenGL context the commands draw to.
// Nothing is rendered after it.
func Terminate() {
	render.Terminate()
}

// GLContext is an OpenGL context: the one the renderer draws in on the desktop, set with
// [SetGLContext].
type GLContext interface {
	MakeContextCurrent() error
}

// GLPresenter is a window's own OpenGL context, the target of an OpenGL surface. A frame is drawn
// in the [GLContext], which shares objects with it, and copied to it to be swapped.
type GLPresenter interface {
	MakeContextCurrent() error
	SwapInterval(interval int) error
	SwapBuffers() error
}

// SetGLContext sets the context the OpenGL driver draws everything in on the desktop. It shares
// objects with every GLPresenter's context and is never presented, so a hidden window's context
// does. A window system creates it after [Init], as [IsOpenGLES] asks, and sets it before the
// first frame. It is for OpenGL alone.
func SetGLContext(context GLContext) {
	render.SetGLContext(context)
}

// CurrentGraphicsLibrary returns the graphics library [Init] chose, or GraphicsLibraryUnknown
// before Init.
func CurrentGraphicsLibrary() GraphicsLibrary {
	return GraphicsLibrary(render.Library())
}

// IsOpenGLES reports whether the OpenGL driver draws with OpenGL ES, which decides the contexts
// a window system creates for it. It is false for the other drivers.
func IsOpenGLES() bool {
	return render.IsOpenGLES()
}

// SupportsDirectComposition reports whether the DirectX driver presents through
// DirectComposition. A window can then be created without a redirection surface, which
// composites a transparent window correctly.
func SupportsDirectComposition() bool {
	return render.SupportsDirectComposition()
}

// NSView is an NSView pointer, the target of a Metal surface. The view renders the surface and
// nothing else: the surface becomes its layer. A window's content view is the usual one.
type NSView = render.NSView

// HWND is a Win32 window handle, the target of a DirectX surface.
type HWND = render.HWND

// Surface is a presentation target: a view's layer, a window's swap chain or its OpenGL context.
type Surface struct {
	surface *render.Surface
	screen  *Image
}

// NewSurface makes a surface for target: an [NSView] for Metal, an [HWND] for DirectX, a
// [GLPresenter] for OpenGL on the desktop, and the canvas given to [Init] in the browser, which
// has one surface. transparent asks for a surface that composites with what is behind it; DirectX
// returns an error for one. NewSurface is called on the main thread, after Init.
func NewSurface(target any, transparent bool) (*Surface, error) {
	s, err := render.NewSurface(target, transparent)
	if err != nil {
		return nil, err
	}
	return &Surface{surface: s}, nil
}

// Screen returns the image presented on s, width by height pixels. A size other than the last
// one replaces the image and deallocates the old one. Screen is called once a frame, between
// [BeginFrame] and [EndFrame]: it clears the image where the graphics driver does not.
func (s *Surface) Screen(width, height int) *Image {
	s.screen = screenImage(s.screen, s.surface.Screen(width, height))
	return s.screen
}

// FramebufferSize returns the size in pixels of the drawing buffer behind s, and whether the
// graphics driver knows it. Only WebGL does, whose canvas decides it.
func (s *Surface) FramebufferSize() (width, height int, ok bool) {
	return s.surface.FramebufferSize()
}

// Dispose deallocates the screen and releases s after the commands queued before it. A window
// system disposes a window's surface before it destroys the window.
func (s *Surface) Dispose() {
	s.surface.Dispose()
	s.screen = nil
}

// BeginFrame begins a frame. Screens are drawn between BeginFrame and [EndFrame], which is called
// when BeginFrame succeeded. A read of pixels that missed the last frame runs at BeginFrame.
func BeginFrame() error {
	return render.BeginFrame()
}

// EndFrame ends the frame [BeginFrame] began.
func EndFrame() error {
	return render.EndFrame()
}

// Flush runs the graphics commands of the frames since the last flush, and presents every
// surface whose screen was drawn to when present is set. Presenting waits for the display where
// the driver's vsync makes it, unless [Occluded] reports that nothing of the surface is visible.
func Flush(present bool) error {
	return render.Flush(present)
}

// Occluded reports whether the last present went to a swap chain nothing of which is visible, and
// so returned without waiting for the display. A loop paces itself then. Only DirectX knows it.
func Occluded() bool {
	return render.Occluded()
}

// FinishForcedFrame waits for a frame drawn and flushed while the main thread was blocked, as in
// a live resize on macOS, to be presented.
func FinishForcedFrame() error {
	return render.FinishForcedFrame()
}

// AdvanceTick advances the clock that [Tick] reads, against which the image GC and the glyph
// caches expire. A window system advances it once per iteration of its loop.
func AdvanceTick() {
	render.IncrementTick()
}
