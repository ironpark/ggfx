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

// Dispose deallocates the screen and releases s after the commands queued before it. A window
// system disposes a window's surface before it destroys the window.
func (s *Surface) Dispose() {
	s.surface.Dispose()
	s.screen = nil
}
