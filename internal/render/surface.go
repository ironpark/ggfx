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

package render

import (
	"errors"

	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// Surface is a presentation target: a window's layer or swap chain, or its OpenGL context.
type Surface struct {
	surface graphicsdriver.Surface
	screen  *Image
}

// NewSurface makes a surface for target: an NSView for Metal, an HWND for DirectX, the window's
// own context for OpenGL on the desktop, which has MakeContextCurrent, SwapInterval and
// SwapBuffers, and the canvas, a js.Value, in the browser, where the first surface creates the
// driver. transparent asks for a surface that composites with what is behind it. NewSurface must
// be called on the main thread, after Init.
func NewSurface(target any, transparent bool) (*Surface, error) {
	if state.Load() != stateRunning {
		return nil, errors.New("render: NewSurface is called before Init or after Terminate")
	}
	if driver == nil {
		g, err := newGraphicsDriverForSurface(target, &options)
		if err != nil {
			return nil, err
		}
		setDriver(g)
	}
	s, err := driver.NewSurface(target, transparent)
	if err != nil {
		return nil, err
	}
	return &Surface{surface: s}, nil
}

// Screen returns the image presented on s, width by height pixels. A size other than the last
// one replaces the image, and the old one is deallocated. Screen is called once a frame, between
// BeginFrame and EndFrame: it clears the image when the graphics driver does not keep it cleared.
func (s *Surface) Screen(width, height int) *Image {
	if s.screen != nil && (s.screen.width != width || s.screen.height != height) {
		s.screen.Deallocate()
		s.screen = nil
	}
	if s.screen == nil {
		s.screen = NewScreenImage(width, height, s.surface)
	}
	if driver.NeedsClearingScreen() {
		s.screen.Clear()
	}
	return s.screen
}

// FramebufferSize returns the size of the drawing buffer behind s, and whether the driver knows
// it. Only WebGL does, whose canvas decides it.
func (s *Surface) FramebufferSize() (width, height int, ok bool) {
	g, ok := driver.(interface{ ScreenFramebufferSize() (int, int) })
	if !ok {
		return 0, 0, false
	}
	width, height = g.ScreenFramebufferSize()
	return width, height, true
}

// Dispose deallocates the screen and releases s once the commands queued before it ran.
func (s *Surface) Dispose() {
	if s.screen != nil {
		s.screen.Deallocate()
		s.screen = nil
	}
	graphicscommand.DisposeSurface(s.surface)
}
