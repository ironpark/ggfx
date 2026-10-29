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

// Package host is the renderer's API for a window system: it initializes the renderer, makes
// surfaces of the windows' native views, and draws and presents frames on them. An app draws with
// ggfx and does not need host; ggwd (github.com/ironpark/ggwd) is a window system that does.
//
// An iteration of a window system's loop goes:
//
//	f, err := host.BeginFrame()
//	if err != nil { ... }
//	screen := f.Screen(surface, width, height)
//	// Draw to screen.
//	if err := f.End(); err != nil { ... }
//	// BeginFrame to End again for every other surface.
//	result, err := host.Flush(true)
//
// # Environment variables
//
// `GGFX_GRAPHICS_LIBRARY` environment variable specifies the graphics library.
// If the specified graphics library is not available, [Init] returns an error.
// This environment variable works when Init is called with [GraphicsLibraryAuto].
// This can take one of the following value:
//
//	"auto":    The renderer chooses the graphics library automatically. This is the default value.
//	"opengl":  OpenGL, OpenGL ES, or WebGL.
//	"directx": DirectX. This works only on Windows.
//	"metal":   Metal. This works only on macOS.
//
// `GGFX_DIRECTX` environment variable specifies various parameters for DirectX.
// You can specify multiple values separated by a comma. The default value is empty (i.e. no parameters).
//
//	"debug":                      Use a debug layer.
//	"dred":                       Enable Device Removed Extended Data to diagnose a device removal. This is for DirectX 12.
//	"warp":                       Use WARP (i.e. software rendering).
//	"version=VERSION":            Specify a DirectX version (e.g. 11).
//	"featurelevel=FEATURE_LEVEL": Specify a feature level (e.g. 11_0). This is for DirectX 12.
//
// The options taking arguments are exclusive, and if multiples are specified, the lastly specified value is adopted.
//
// The possible values for the option "version" are "11" and "12".
// If the version is not specified, the default version 11 is adopted.
//
// The option "featurelevel" is valid only for DirectX 12.
// The possible values are "11_0", "11_1", "12_0", "12_1", and "12_2". The default value is "11_0".
//
// The option "dred" is valid only for DirectX 12 and is independent of "debug".
// On a device removal, it reports the GPU's last command and the page-fault address via log/slog.
//
// Ebitengine's names, `EBITENGINE_GRAPHICS_LIBRARY` and `EBITENGINE_DIRECTX`, are read when the
// ggfx ones are not set.
package host

import (
	"runtime"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/bridge"
	ecolor "github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/render"
)

// Options are what [Init] is given.
type Options struct {
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
}

// Init creates the graphics driver and starts the render thread. It is called once, on the main
// thread, before anything is rendered. Images can be made and drawn to before Init, but their
// draws run in the first frame after it. In the browser, the driver is created for the canvas of
// the first [NewSurface].
func Init(options *Options) error {
	if options == nil {
		options = &Options{}
	}
	return render.Init(&render.Options{
		GraphicsLibrary: render.GraphicsLibrary(options.GraphicsLibrary),
		ColorSpace:      toRenderColorSpace(options.ColorSpace),
		SingleThread:    options.SingleThread,
		MainThread:      options.MainThread,
	})
}

// toRenderColorSpace resolves ColorSpaceDefault to the environment's color space.
func toRenderColorSpace(colorSpace ColorSpace) ecolor.ColorSpace {
	if colorSpace != ColorSpaceDefault {
		return ecolor.ColorSpace(colorSpace)
	}
	if runtime.GOOS == "darwin" {
		// On macOS, the default color space is Display P3.
		// TODO: Remove this logic at v3. sRGB should be the default in the future (#3349).
		return ecolor.ColorSpaceDisplayP3
	}
	return ecolor.ColorSpaceSRGB
}

// Terminate waits for the queued graphics commands to run and stops the render thread. A window
// system calls it before it destroys the windows and the OpenGL context the commands draw to.
// Nothing is rendered after it.
func Terminate() {
	render.Terminate()
}

// Driver describes the graphics driver [Init] created, which decides how a window system creates
// its windows.
type Driver struct {
	// Library is the graphics library, or GraphicsLibraryUnknown before Init.
	Library GraphicsLibrary

	// OpenGLES reports whether the OpenGL driver draws with OpenGL ES, which decides the contexts
	// a window system creates for it.
	OpenGLES bool

	// DirectComposition reports whether the DirectX driver presents through DirectComposition.
	// A window can then be created without a redirection surface, which composites a transparent
	// window correctly.
	DirectComposition bool
}

// CurrentDriver describes the graphics driver.
func CurrentDriver() Driver {
	return Driver{
		Library:           GraphicsLibrary(render.Library()),
		OpenGLES:          render.IsOpenGLES(),
		DirectComposition: render.SupportsDirectComposition(),
	}
}

// GLContext is an OpenGL context: the one the renderer draws in on the desktop, set with
// [SetGLContext].
type GLContext interface {
	MakeContextCurrent() error
}

// SetGLContext sets the context the OpenGL driver draws everything in on the desktop. It shares
// objects with every [GLWindow]'s context and is never presented, so a hidden window's context
// does. A window system creates it after [Init], as [Driver.OpenGLES] asks, and sets it before the
// first frame. It is for OpenGL alone.
func SetGLContext(context GLContext) {
	render.SetGLContext(context)
}

// Frame is a frame begun by [BeginFrame]. Screens are drawn in a frame.
type Frame struct {
	begun bool
}

// BeginFrame begins a frame. A read of pixels that missed the last frame runs at BeginFrame.
// [Frame.End] is called when BeginFrame succeeded.
func BeginFrame() (Frame, error) {
	if err := render.BeginFrame(); err != nil {
		return Frame{}, err
	}
	return Frame{begun: true}, nil
}

// Screen returns the image presented on s, width by height pixels. A size other than the last
// one replaces the image and deallocates the old one. Screen is called once a frame for each
// surface drawn: it clears the image where the graphics driver does not.
func (f Frame) Screen(s *Surface, width, height int) *ggfx.Image {
	if !f.begun {
		panic("host: Screen is called outside a frame")
	}
	s.screen = bridge.ScreenImage(s.screen, s.surface.Screen(width, height)).(*ggfx.Image)
	return s.screen
}

// End ends the frame.
func (f Frame) End() error {
	if !f.begun {
		panic("host: End is called outside a frame")
	}
	return render.EndFrame()
}

// FlushResult is what [Flush] tells of the present.
type FlushResult struct {
	// Occluded reports that the present went to a swap chain nothing of which is visible, and so
	// returned without waiting for the display. A loop paces itself then. Only DirectX knows it.
	Occluded bool
}

// Flush runs the graphics commands of the frames since the last flush, and presents every
// surface whose screen was drawn to when present is set. Presenting waits for the display where
// the driver's vsync makes it, unless the result reports the surface occluded. The image and
// glyph caches expire after so many flushes, so a loop flushes once an iteration, presenting or
// not.
func Flush(present bool) (FlushResult, error) {
	if err := render.Flush(present); err != nil {
		return FlushResult{}, err
	}
	return FlushResult{Occluded: present && render.Occluded()}, nil
}

// FinishForcedFrame waits for a frame drawn and flushed while the main thread was blocked, as in
// a live resize on macOS, to be presented.
func FinishForcedFrame() error {
	return render.FinishForcedFrame()
}
