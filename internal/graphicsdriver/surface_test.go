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

package graphicsdriver_test

import (
	"testing"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

type stubGLPresenter struct{}

func (stubGLPresenter) MakeContextCurrent() error { return nil }
func (stubGLPresenter) SwapInterval(int) error    { return nil }
func (stubGLPresenter) SwapBuffers() error        { return nil }

func TestSurfaceDescriptorValidate(t *testing.T) {
	valid := []graphicsdriver.SurfaceDescriptor{
		{Kind: graphicsdriver.SurfaceKindNSView, NSView: 1},
		{Kind: graphicsdriver.SurfaceKindHWND, HWND: 1},
		{Kind: graphicsdriver.SurfaceKindX11, X11: graphicsdriver.X11Window{Display: 1, Window: 2}},
		{Kind: graphicsdriver.SurfaceKindWayland, Wayland: graphicsdriver.WaylandSurface{Display: 1, Surface: 2}},
		{Kind: graphicsdriver.SurfaceKindGLPresenter, GLPresenter: stubGLPresenter{}},
		{Kind: graphicsdriver.SurfaceKindRemote},
	}
	for _, d := range valid {
		if err := d.Validate(); err != nil {
			t.Errorf("Validate(%s) failed: %v", d.Kind, err)
		}
	}

	invalid := []graphicsdriver.SurfaceDescriptor{
		{},
		{Kind: graphicsdriver.SurfaceKind(99)},
		{Kind: graphicsdriver.SurfaceKindNSView},
		{Kind: graphicsdriver.SurfaceKindHWND},
		{Kind: graphicsdriver.SurfaceKindX11, X11: graphicsdriver.X11Window{Display: 1}},
		{Kind: graphicsdriver.SurfaceKindX11, X11: graphicsdriver.X11Window{Window: 1}},
		{Kind: graphicsdriver.SurfaceKindWayland, Wayland: graphicsdriver.WaylandSurface{Display: 1}},
		{Kind: graphicsdriver.SurfaceKindWayland, Wayland: graphicsdriver.WaylandSurface{Surface: 1}},
		{Kind: graphicsdriver.SurfaceKindGLPresenter},
	}
	for _, d := range invalid {
		if err := d.Validate(); err == nil {
			t.Errorf("Validate(%+v) unexpectedly succeeded", d)
		}
	}
}

func TestSurfaceKindString(t *testing.T) {
	cases := []struct {
		kind graphicsdriver.SurfaceKind
		want string
	}{
		{graphicsdriver.SurfaceKindNone, "None"},
		{graphicsdriver.SurfaceKindNSView, "NSView"},
		{graphicsdriver.SurfaceKindHWND, "HWND"},
		{graphicsdriver.SurfaceKindX11, "X11"},
		{graphicsdriver.SurfaceKindWayland, "Wayland"},
		{graphicsdriver.SurfaceKindGLPresenter, "GLPresenter"},
		{graphicsdriver.SurfaceKindCanvas, "Canvas"},
		{graphicsdriver.SurfaceKindRemote, "Remote"},
		{graphicsdriver.SurfaceKind(42), "SurfaceKind(42)"},
	}
	for _, tc := range cases {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("SurfaceKind(%d).String() = %q, want %q", tc.kind, got, tc.want)
		}
	}
}
