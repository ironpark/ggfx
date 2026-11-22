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

//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"fmt"
	"image"
	"runtime"
	"slices"
	"sync/atomic"
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// Surface implements graphicsdriver.Surface and optional surface capabilities
// (KeptSurface, DamagedSurface, WindowStateSurface, ReadableSurface), honoring
// the SurfaceCaps flags reported by the active native backend.
type Surface struct {
	g        *Graphics
	id       uint32
	caps     SurfaceCaps
	disposed atomic.Bool

	// Protected by g.surfacesMu.
	damageDirty bool
	damageRects []image.Rectangle
}

var (
	_ graphicsdriver.Surface            = (*Surface)(nil)
	_ graphicsdriver.KeptSurface        = (*Surface)(nil)
	_ graphicsdriver.DamagedSurface     = (*Surface)(nil)
	_ graphicsdriver.WindowStateSurface = (*Surface)(nil)
	_ graphicsdriver.ReadableSurface    = (*Surface)(nil)
)

func toABISurfaceDesc(backend uint32, desc graphicsdriver.SurfaceDescriptor) (SurfaceDesc, error) {
	if err := desc.Validate(); err != nil {
		return SurfaceDesc{}, err
	}

	var kind uint32
	var h1, h2 uint64

	switch desc.Kind {
	case graphicsdriver.SurfaceKindNSView:
		if backend != BackendMetal && backend != BackendNull {
			return SurfaceDesc{}, fmt.Errorf("%w: NSView surface requires Metal backend", ErrUnsupported)
		}
		kind = SurfaceKindNSView
		h1 = uint64(desc.NSView)
	case graphicsdriver.SurfaceKindHWND:
		if backend != BackendDX12 && backend != BackendNull {
			return SurfaceDesc{}, fmt.Errorf("%w: HWND surface requires DX12 backend", ErrUnsupported)
		}
		kind = SurfaceKindHWND
		h1 = uint64(desc.HWND)
	case graphicsdriver.SurfaceKindX11:
		if backend != BackendVulkan && backend != BackendNull {
			return SurfaceDesc{}, fmt.Errorf("%w: X11 surface requires Vulkan backend", ErrUnsupported)
		}
		kind = SurfaceKindX11
		h1 = uint64(desc.X11.Display)
		h2 = uint64(desc.X11.Window)
	case graphicsdriver.SurfaceKindWayland:
		if backend != BackendVulkan && backend != BackendNull {
			return SurfaceDesc{}, fmt.Errorf("%w: Wayland surface requires Vulkan backend", ErrUnsupported)
		}
		kind = SurfaceKindWayland
		h1 = uint64(desc.Wayland.Display)
		h2 = uint64(desc.Wayland.Surface)
	default:
		return SurfaceDesc{}, fmt.Errorf("%w: surface kind %s is not supported", ErrUnsupported, desc.Kind)
	}

	var flags uint32
	if desc.Transparent {
		flags |= SurfaceFlagTransparent
	}

	return SurfaceDesc{
		StructSize: uint32(unsafe.Sizeof(SurfaceDesc{})),
		Kind:       kind,
		Flags:      flags,
		Handle1:    h1,
		Handle2:    h2,
	}, nil
}

// NewSurface implements graphicsdriver.Graphics. Called on the OS main thread.
func (g *Graphics) NewSurface(desc graphicsdriver.SurfaceDescriptor) (graphicsdriver.Surface, error) {
	if g.closed.Load() {
		return nil, ErrClosed
	}

	sd, err := toABISurfaceDesc(g.caps.Backend, desc)
	if err != nil {
		return nil, err
	}

	id := g.nextSurfaceID.Add(1)
	var scaps SurfaceCaps

	g.devMu.RLock()
	defer g.devMu.RUnlock()
	if g.closed.Load() || g.dev == 0 {
		return nil, ErrClosed
	}

	g.mainErrMu.Lock()
	g.mainErrBuf[0] = 0
	st := g.lib.surfaceCreate(g.dev, id, &sd, &scaps, &g.mainErrBuf[0], uintptr(len(g.mainErrBuf)))
	runtime.KeepAlive(&sd)
	runtime.KeepAlive(&scaps)
	err = statusToError("surface_create", st, g.mainErrBuf[:])
	g.mainErrMu.Unlock()
	if err != nil {
		return nil, err
	}

	s := &Surface{
		g:    g,
		id:   id,
		caps: scaps,
	}
	g.surfacesMu.Lock()
	g.surfaces = append(g.surfaces, s)
	g.surfacesMu.Unlock()
	return s, nil
}

// NewScreenImage implements graphicsdriver.Surface. Called on the render thread.
func (s *Surface) NewScreenImage(width, height int) (graphicsdriver.Image, error) {
	if s.g.closed.Load() {
		return nil, ErrClosed
	}
	if s.disposed.Load() {
		return nil, ErrUnknownHandle
	}
	graphicsdriver.CheckSize("zigdriver", width, height, s.g.MaxImageSize())

	id := s.g.allocImageID()
	s.g.ring.cur().encodeNewScreenImage(id, s.id, uint32(width), uint32(height))
	return &Image{
		g:              s.g,
		id:             id,
		width:          width,
		height:         height,
		internalWidth:  width,
		internalHeight: height,
		format:         graphicsdriver.PixelFormatRGBA8,
		screen:         true,
	}, nil
}

// FlipsY implements graphicsdriver.Surface.
func (s *Surface) FlipsY() bool {
	return s.caps.Flags&SurfaceCapFlagFlipsY != 0
}

// Readable implements graphicsdriver.ReadableSurface.
func (s *Surface) Readable() bool {
	return s.caps.Flags&SurfaceCapFlagReadable != 0
}

// Kept implements graphicsdriver.KeptSurface.
func (s *Surface) Kept() bool {
	if s.caps.Flags&SurfaceCapFlagKept == 0 || s.disposed.Load() {
		return false
	}
	s.g.devMu.RLock()
	defer s.g.devMu.RUnlock()
	if s.g.closed.Load() || s.g.dev == 0 {
		return false
	}
	return s.g.lib.surfaceKept(s.g.dev, s.id) != 0
}

// SetDamage implements graphicsdriver.DamagedSurface.
func (s *Surface) SetDamage(rects []image.Rectangle) {
	if s.caps.Flags&SurfaceCapFlagDamage == 0 || s.disposed.Load() || s.g.closed.Load() {
		return
	}
	s.g.surfacesMu.Lock()
	if rects == nil {
		s.damageRects = nil
	} else if len(rects) == 0 {
		s.damageRects = []image.Rectangle{}
	} else {
		s.damageRects = slices.Clone(rects)
	}
	s.damageDirty = true
	s.g.surfacesMu.Unlock()
}

// SetWindowState implements graphicsdriver.WindowStateSurface.
func (s *Surface) SetWindowState(liveResizing, fullscreen, hidden bool) {
	if s.disposed.Load() {
		return
	}
	s.g.devMu.RLock()
	defer s.g.devMu.RUnlock()
	if s.g.closed.Load() || s.g.dev == 0 {
		return
	}
	var flags uint32
	if liveResizing {
		flags |= WindowStateLiveResizing
	}
	if fullscreen {
		flags |= WindowStateFullscreen
	}
	if hidden {
		flags |= WindowStateHidden
	}
	s.g.lib.surfaceSetWindowState(s.g.dev, s.id, flags)
}

// Dispose implements graphicsdriver.Surface. Called on the render thread.
func (s *Surface) Dispose() {
	if !s.disposed.CompareAndSwap(false, true) {
		return
	}
	s.g.surfacesMu.Lock()
	for i, existing := range s.g.surfaces {
		if existing == s {
			s.g.surfaces = slices.Delete(s.g.surfaces, i, i+1)
			break
		}
	}
	s.damageDirty = false
	s.damageRects = nil
	s.g.surfacesMu.Unlock()

	if !s.g.closed.Load() {
		s.g.ring.cur().encodeDisposeSurface(s.id)
	}
}
