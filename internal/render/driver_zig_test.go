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

package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

func getDriverFallbackErr() error {
	driverFallbackMu.Lock()
	defer driverFallbackMu.Unlock()
	return driverFallbackErr
}

func resetDriverSelectionForTest(t *testing.T) {
	t.Helper()
	prevImpl := DriverImplementation()
	driverFallbackMu.Lock()
	prevFallbackErr := driverFallbackErr
	prevFallbackWarned := driverFallbackWarned
	driverFallbackErr = nil
	driverFallbackWarned = false
	driverFallbackMu.Unlock()
	t.Cleanup(func() {
		driverImpl.Store(prevImpl)
		driverFallbackMu.Lock()
		driverFallbackErr = prevFallbackErr
		driverFallbackWarned = prevFallbackWarned
		driverFallbackMu.Unlock()
	})

	t.Setenv("GGFX_DRIVER", "")
	t.Setenv("GGFX_DRIVER_LIB", "")
	t.Setenv("GGFX_DRIVER_STRICT", "")
	t.Setenv("GGFX_GRAPHICS_LIBRARY", "")
	t.Setenv("EBITENGINE_GRAPHICS_LIBRARY", "")
	t.Setenv("EBITEN_GRAPHICS_LIBRARY", "")

	driverImpl.Store("")
}

func TestZigDriverSelectionAndFallback(t *testing.T) {
	t.Run("DefaultUsesGoDriver", func(t *testing.T) {
		resetDriverSelectionForTest(t)

		g, _, err := newGraphicsDriver(GraphicsLibraryAuto, &Options{})
		if err != nil {
			t.Fatalf("newGraphicsDriver(Auto): %v", err)
		}
		if c, ok := g.(graphicsdriver.Closer); ok {
			defer func() { _ = c.Close() }()
		}
		if got := DriverImplementation(); got != "go" {
			t.Fatalf("DriverImplementation() = %q, want %q", got, "go")
		}
	})

	t.Run("MissingLibFallsBackToGo", func(t *testing.T) {
		resetDriverSelectionForTest(t)
		t.Setenv("GGFX_DRIVER_LIB", filepath.Join(t.TempDir(), "nonexistent_ggfx_driver"))

		g, _, err := newGraphicsDriver(GraphicsLibraryAuto, &Options{})
		if err != nil {
			t.Fatalf("newGraphicsDriver with missing GGFX_DRIVER_LIB should fall back, got err: %v", err)
		}
		if c, ok := g.(graphicsdriver.Closer); ok {
			defer func() { _ = c.Close() }()
		}
		if got := DriverImplementation(); got != "go" {
			t.Fatalf("DriverImplementation() = %q, want %q after fallback", got, "go")
		}
		if getDriverFallbackErr() == nil {
			t.Fatalf("driverFallbackErr = nil, want non-nil error recorded on fallback")
		}
	})

	t.Run("MissingLibStrictReturnsError", func(t *testing.T) {
		resetDriverSelectionForTest(t)
		t.Setenv("GGFX_DRIVER_LIB", filepath.Join(t.TempDir(), "nonexistent_ggfx_driver"))
		t.Setenv("GGFX_DRIVER_STRICT", "1")

		g, _, err := newGraphicsDriver(GraphicsLibraryAuto, &Options{})
		if g != nil {
			if c, ok := g.(graphicsdriver.Closer); ok {
				_ = c.Close()
			}
		}
		if err == nil {
			t.Fatalf("newGraphicsDriver with GGFX_DRIVER_STRICT=1 and missing lib = nil error, want error")
		}
	})

	t.Run("ExplicitGoDriverIgnoresLibAndStrict", func(t *testing.T) {
		resetDriverSelectionForTest(t)
		t.Setenv("GGFX_DRIVER", "go")
		t.Setenv("GGFX_DRIVER_LIB", filepath.Join(t.TempDir(), "nonexistent_ggfx_driver"))
		t.Setenv("GGFX_DRIVER_STRICT", "1")

		g, _, err := newGraphicsDriver(GraphicsLibraryAuto, &Options{})
		if err != nil {
			t.Fatalf("newGraphicsDriver(GGFX_DRIVER=go): %v", err)
		}
		if c, ok := g.(graphicsdriver.Closer); ok {
			defer func() { _ = c.Close() }()
		}
		if got := DriverImplementation(); got != "go" {
			t.Fatalf("DriverImplementation() = %q, want %q", got, "go")
		}
	})

	t.Run("GraphicsLibraryZigStrictReturnsErrorOnMissingLib", func(t *testing.T) {
		resetDriverSelectionForTest(t)
		t.Setenv("GGFX_GRAPHICS_LIBRARY", "zig")
		t.Setenv("GGFX_DRIVER_LIB", filepath.Join(t.TempDir(), "nonexistent_ggfx_driver"))
		t.Setenv("GGFX_DRIVER_STRICT", "1")

		g, _, err := newGraphicsDriver(GraphicsLibraryAuto, &Options{})
		if g != nil {
			if c, ok := g.(graphicsdriver.Closer); ok {
				_ = c.Close()
			}
		}
		if err == nil {
			t.Fatalf("expected error for GGFX_GRAPHICS_LIBRARY=zig with missing lib and STRICT=1")
		}
	})

	t.Run("GraphicsLibraryVulkanFallsBackToAutoGo", func(t *testing.T) {
		resetDriverSelectionForTest(t)
		t.Setenv("GGFX_GRAPHICS_LIBRARY", "vulkan")
		t.Setenv("GGFX_DRIVER_LIB", filepath.Join(t.TempDir(), "nonexistent_ggfx_driver"))

		g, _, err := newGraphicsDriver(GraphicsLibraryAuto, &Options{})
		if err != nil {
			t.Fatalf("newGraphicsDriver with GGFX_GRAPHICS_LIBRARY=vulkan and missing lib should fall back to auto Go driver, got err: %v", err)
		}
		if c, ok := g.(graphicsdriver.Closer); ok {
			defer func() { _ = c.Close() }()
		}
		if got := DriverImplementation(); got != "go" {
			t.Fatalf("DriverImplementation() = %q, want %q after vulkan fallback", got, "go")
		}
		if getDriverFallbackErr() == nil {
			t.Fatalf("driverFallbackErr = nil, want non-nil error recorded on vulkan fallback")
		}
	})

	t.Run("ExplicitLibSelectsZigDriver", func(t *testing.T) {
		libPath := os.Getenv("GGFX_DRIVER_LIB")
		if libPath == "" {
			t.Skip("GGFX_DRIVER_LIB not set")
		}
		resetDriverSelectionForTest(t)
		t.Setenv("GGFX_DRIVER", "zig")
		t.Setenv("GGFX_DRIVER_LIB", libPath)
		t.Setenv("GGFX_DRIVER_STRICT", "1")

		g, _, err := newGraphicsDriver(GraphicsLibraryAuto, &Options{})
		if err != nil {
			t.Fatalf("newGraphicsDriver(zig): %v", err)
		}
		if c, ok := g.(graphicsdriver.Closer); ok {
			defer func() {
				if err := c.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}()
		}
		if got := DriverImplementation(); got != "zig" {
			t.Fatalf("DriverImplementation() = %q, want %q", got, "zig")
		}
	})
}
