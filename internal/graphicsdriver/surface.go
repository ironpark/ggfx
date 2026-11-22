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
	"errors"
	"fmt"
)

// SurfaceKind identifies which native window handle a SurfaceDescriptor carries.
type SurfaceKind uint8

const (
	SurfaceKindNone        SurfaceKind = iota
	SurfaceKindNSView                  // Metal (Go & Zig)
	SurfaceKindHWND                    // DirectX (Go & Zig)
	SurfaceKindX11                     // Zig Vulkan
	SurfaceKindWayland                 // Zig Vulkan
	SurfaceKindGLPresenter             // Go OpenGL desktop
	SurfaceKindCanvas                  // Go WebGL
	SurfaceKindRemote                  // remote driver
)

// String returns the kind's name.
func (k SurfaceKind) String() string {
	switch k {
	case SurfaceKindNone:
		return "None"
	case SurfaceKindNSView:
		return "NSView"
	case SurfaceKindHWND:
		return "HWND"
	case SurfaceKindX11:
		return "X11"
	case SurfaceKindWayland:
		return "Wayland"
	case SurfaceKindGLPresenter:
		return "GLPresenter"
	case SurfaceKindCanvas:
		return "Canvas"
	case SurfaceKindRemote:
		return "Remote"
	}
	return fmt.Sprintf("SurfaceKind(%d)", uint8(k))
}

// NSView is an NSView pointer. The view a Metal surface is made for renders that surface and
// nothing else: the surface becomes its layer.
type NSView uintptr

// HWND is a Win32 window handle.
type HWND uintptr

// X11Window holds an X11 Display* and Window XID.
type X11Window struct {
	Display uintptr
	Window  uintptr
}

// WaylandSurface holds a wl_display* and wl_surface*.
type WaylandSurface struct {
	Display uintptr
	Surface uintptr
}

// GLPresenter is what the rendered frame is presented through on desktop OpenGL.
type GLPresenter interface {
	MakeContextCurrent() error
	SwapInterval(interval int) error
	SwapBuffers() error
}

// SurfaceDescriptor describes a native presentation target for Graphics.NewSurface.
type SurfaceDescriptor struct {
	Kind        SurfaceKind
	Transparent bool
	NSView      NSView
	HWND        HWND
	X11         X11Window
	Wayland     WaylandSurface
	GLPresenter GLPresenter
	Canvas      Canvas
}

// Validate checks that d carries a known Kind and that the handle fields required for that Kind
// are non-zero / non-nil.
func (d SurfaceDescriptor) Validate() error {
	switch d.Kind {
	case SurfaceKindNSView:
		if d.NSView == 0 {
			return errors.New("graphicsdriver: NSView surface handle must not be 0")
		}
		return nil
	case SurfaceKindHWND:
		if d.HWND == 0 {
			return errors.New("graphicsdriver: HWND surface handle must not be 0")
		}
		return nil
	case SurfaceKindX11:
		if d.X11.Display == 0 || d.X11.Window == 0 {
			return errors.New("graphicsdriver: X11 Display and Window handles must not be 0")
		}
		return nil
	case SurfaceKindWayland:
		if d.Wayland.Display == 0 || d.Wayland.Surface == 0 {
			return errors.New("graphicsdriver: Wayland Display and Surface handles must not be 0")
		}
		return nil
	case SurfaceKindGLPresenter:
		if d.GLPresenter == nil {
			return errors.New("graphicsdriver: GLPresenter must not be nil")
		}
		return nil
	case SurfaceKindCanvas:
		if !isValidCanvas(d.Canvas) {
			return errors.New("graphicsdriver: Canvas must be valid")
		}
		return nil
	case SurfaceKindRemote:
		return nil
	default:
		return fmt.Errorf("graphicsdriver: invalid surface kind %s", d.Kind)
	}
}
