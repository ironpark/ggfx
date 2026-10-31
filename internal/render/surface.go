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
	"image"

	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// Surface is a presentation target: a window's layer or swap chain, or its OpenGL context.
type Surface struct {
	surface graphicsdriver.Surface
	screen  *Image
	age     int // the age of the buffer the frame being drawn draws into
}

// keptSurface is a driver's surface that keeps what its buffers held, and
// presents only what changed.
type keptSurface interface {
	// BufferAge picks the buffer the next frame draws into, and returns
	// how many presents ago it was last presented: 1 the last, 2 the one
	// before; 0 when what it holds is not known.
	BufferAge() int
	// SetDamage gives the parts of the frame being drawn, in pixels, that
	// differ from the frame presented before it, for the present to show
	// those alone; nil when all of it may.
	SetDamage(rects []image.Rectangle)
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
	s.age = 0
	if s.screen == nil {
		s.screen = NewScreenImage(width, height, s.surface)
	} else if k, ok := s.surface.(keptSurface); ok {
		s.age = k.BufferAge()
	}
	// A buffer that keeps a frame is drawn over, not afresh.
	if s.age == 0 && driver.NeedsClearingScreen() {
		s.screen.Clear()
	}
	return s.screen
}

// BufferAge reports how many presents ago the buffer the frame being drawn
// draws into was last presented, as the last Screen found it: 1 the last,
// 2 the one before; 0 when what it holds is not known, and the frame is
// drawn whole on a cleared screen. Only a driver that keeps its buffers
// reports more than 0.
func (s *Surface) BufferAge() int { return s.age }

// SetDamage gives the parts of the frame being drawn, in pixels, that
// differ from the frame presented before it, so that the present shows
// those alone; nil, or a driver that cannot, presents all of it.
func (s *Surface) SetDamage(rects []image.Rectangle) {
	if k, ok := s.surface.(keptSurface); ok {
		k.SetDamage(rects)
	}
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

// windowStateSetter is a driver's surface that asks what its window is doing.
type windowStateSetter interface {
	SetWindowState(liveResizing, fullscreen bool)
}

// SetWindowState tells the surface what its window is doing, for the frames after it. Only Metal
// asks.
func (s *Surface) SetWindowState(liveResizing, fullscreen bool) {
	if w, ok := s.surface.(windowStateSetter); ok {
		w.SetWindowState(liveResizing, fullscreen)
	}
}

// Dispose deallocates the screen and releases s once the commands queued before it ran.
func (s *Surface) Dispose() {
	if s.screen != nil {
		s.screen.Deallocate()
		s.screen = nil
	}
	graphicscommand.DisposeSurface(s.surface)
}
