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

package host

import (
	"image"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/render"
)

// SurfaceTarget is what a surface presents to: an [NSView] for Metal, an [HWND] for DirectX, a
// [GLWindow] for OpenGL on the desktop, and a Canvas in the browser.
type SurfaceTarget interface {
	// driverTarget returns what the graphics driver takes.
	driverTarget() any
}

// NSView is an NSView pointer, the target of a Metal surface. The view renders the surface and
// nothing else: the surface becomes its layer. A window's content view is the usual one.
type NSView uintptr

func (v NSView) driverTarget() any { return graphicsdriver.NSView(v) }

// HWND is a Win32 window handle, the target of a DirectX surface.
type HWND uintptr

func (h HWND) driverTarget() any { return graphicsdriver.HWND(h) }

// GLPresenter is a window's own OpenGL context. A frame is drawn in the [GLContext], which shares
// objects with it, and copied to it to be swapped.
type GLPresenter interface {
	MakeContextCurrent() error
	SwapInterval(interval int) error
	SwapBuffers() error
}

// GLWindow is the target of an OpenGL surface on the desktop: the window's own context.
type GLWindow struct {
	Context GLPresenter
}

func (w GLWindow) driverTarget() any { return w.Context }

// Surface is a presentation target: a view's layer, a window's swap chain or its OpenGL context.
type Surface struct {
	surface *render.Surface
	screen  *ggfx.Image
}

// NewSurface makes a surface for target. transparent asks for a surface that composites with
// what is behind it; DirectX returns an error for one. NewSurface is called on the main thread,
// after [Init].
func NewSurface(target SurfaceTarget, transparent bool) (*Surface, error) {
	s, err := render.NewSurface(target.driverTarget(), transparent)
	if err != nil {
		return nil, err
	}
	return &Surface{surface: s}, nil
}

// FramebufferSize returns the size in pixels of the drawing buffer behind s, and whether the
// graphics driver knows it. Only WebGL does, whose canvas decides it.
func (s *Surface) FramebufferSize() (width, height int, ok bool) {
	return s.surface.FramebufferSize()
}

// WindowState is what a surface's window is doing, which decides how Metal presents to it.
type WindowState struct {
	// LiveResizing is set while the user resizes the window, as NSView's inLiveResize reports it.
	// Metal then sizes its layer and presents on the main thread, through [Options.MainThread], so
	// that the new frame shows in the same Core Animation transaction as the new window size.
	LiveResizing bool

	// Fullscreen is set while the window is in macOS's fullscreen mode. Metal then presents to the
	// display directly rather than in step with Core Animation transactions.
	Fullscreen bool

	// Hidden is set while the window is not shown: ordered out or minimized, not merely covered,
	// which a window switcher still shows. Metal then lets go of the drawables it keeps for the
	// window, and the first frame after draws it whole.
	Hidden bool
}

// SetWindowState tells s what its window is doing. A window system sets it before each frame
// drawn to s, on the goroutine that begins the frame; a surface whose state was never set is a
// window at rest. Only Metal asks.
func (s *Surface) SetWindowState(state WindowState) {
	s.surface.SetWindowState(state.LiveResizing, state.Fullscreen, state.Hidden)
}

// Kept reports whether the frame being drawn on s starts from the last, as
// [Frame.Screen] found: the screen holds what the last frame presented,
// whichever of the driver's buffers it is in, and only what changed since
// needs drawing over it. Else the screen is cleared, and the frame is
// drawn whole. A driver that cannot keep a frame always reports false;
// Metal can.
func (s *Surface) Kept() bool { return s.surface.Kept() }

// SetDamage tells s, while a frame is drawn to it, the parts of the frame
// in pixels that differ from the frame presented before it, for the
// present to show those alone and the compositor to read no more. Every
// pixel in them must be drawn. nil, the default each frame, presents all
// of the frame; a driver that cannot present a part does so too.
func (s *Surface) SetDamage(rects []image.Rectangle) { s.surface.SetDamage(rects) }

// Dispose deallocates the screen and releases s after the commands queued before it. A window
// system disposes a window's surface before it destroys the window.
func (s *Surface) Dispose() {
	s.surface.Dispose()
	s.screen = nil
}
