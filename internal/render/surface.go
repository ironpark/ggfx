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
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// Surface is a presentation target: a window's layer or swap chain, or its OpenGL context.
type Surface struct {
	surface graphicsdriver.Surface
	screen  *Image
	kept    bool // the frame being drawn starts from the last

	// resizeGen increments whenever the screen image is replaced on a size change; disposedGen
	// advances on the render thread once the ordered callback queued right after the old screen's
	// Dispose has run (or been dropped on flush error). While disposedGen < resizeGen, Screen
	// reports kept = false without consulting KeptSurface.Kept().
	resizeGen   uint64
	disposedGen atomic.Uint64
}

// NewSurface makes a surface for the native target described by desc. In the browser, the first
// surface creates the driver for its canvas. desc.Transparent asks for a surface that composites
// with what is behind it. NewSurface must be called on the main thread, after Init.
func NewSurface(desc graphicsdriver.SurfaceDescriptor) (*Surface, error) {
	if state.Load() != stateRunning {
		return nil, errors.New("render: NewSurface is called before Init or after Terminate")
	}
	if err := desc.Validate(); err != nil {
		return nil, err
	}
	if driver == nil {
		g, err := newGraphicsDriverForSurface(desc, &options)
		if err != nil {
			return nil, err
		}
		setDriver(g)
	}
	s, err := driver.NewSurface(desc)
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
		s.screen.Dispose()
		s.screen = nil
		s.resizeGen++
		gen := s.resizeGen
		atlas.EnqueueCallback(func() {
			for cur := s.disposedGen.Load(); gen > cur; cur = s.disposedGen.Load() {
				if s.disposedGen.CompareAndSwap(cur, gen) {
					break
				}
			}
		})
	}
	s.kept = false
	if s.screen == nil {
		s.screen = NewScreenImage(width, height, s.surface)
	} else if s.disposedGen.Load() >= s.resizeGen {
		if k, ok := s.surface.(graphicsdriver.KeptSurface); ok {
			s.kept = k.Kept()
		}
	}
	// A frame that starts from the last is drawn over it, not afresh.
	if !s.kept && driver.NeedsClearingScreen() {
		s.screen.Clear()
	}
	return s.screen
}

// Kept reports whether the frame being drawn starts from the last, as the
// last Screen found: the screen holds what the last frame presented, and
// only what changed since needs drawing. Else it is cleared, for a frame
// drawn whole. Only a driver that can keep a frame reports true.
func (s *Surface) Kept() bool { return s.kept }

// SetDamage gives the parts of the frame being drawn, in pixels, that
// differ from the frame presented before it, so that the present shows
// those alone; nil, or a driver that cannot, presents all of it.
func (s *Surface) SetDamage(rects []image.Rectangle) {
	if d, ok := s.surface.(graphicsdriver.DamagedSurface); ok {
		d.SetDamage(rects)
	}
}

// FramebufferSize returns the size of the drawing buffer behind s, and whether the driver knows
// it. Only WebGL does, whose canvas decides it.
func (s *Surface) FramebufferSize() (width, height int, ok bool) {
	g, ok := driver.(graphicsdriver.FramebufferSizer)
	if !ok {
		return 0, 0, false
	}
	width, height = g.ScreenFramebufferSize()
	return width, height, true
}

// SetWindowState tells the surface what its window is doing, for the frames after it. Only Metal
// asks.
func (s *Surface) SetWindowState(liveResizing, fullscreen, hidden bool) {
	if w, ok := s.surface.(graphicsdriver.WindowStateSurface); ok {
		w.SetWindowState(liveResizing, fullscreen, hidden)
	}
}

// Dispose deallocates the screen and releases s once the commands queued before it ran.
func (s *Surface) Dispose() {
	if s.surface == nil {
		return
	}
	if s.screen != nil {
		s.screen.Dispose()
		s.screen = nil
	}
	atlas.DisposeSurface(s.surface)
	s.surface = nil
}
