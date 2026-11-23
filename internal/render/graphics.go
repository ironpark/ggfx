// Copyright 2022 The Ebiten Authors
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
	"cmp"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

var (
	// driverImpl holds the active graphics driver implementation name ("go" or "zig").
	driverImpl atomic.Value

	// driverFallbackMu protects driverFallbackErr and driverFallbackWarned.
	driverFallbackMu     sync.Mutex
	driverFallbackErr    error
	driverFallbackWarned bool
)

// newGraphicsDriver creates the driver for lib. For GraphicsLibraryAuto, the environment variable
// can name one, and otherwise the platform chooses.
func newGraphicsDriver(lib GraphicsLibrary, o *Options) (graphicsdriver.Graphics, GraphicsLibrary, error) {
	if overrideDriver.g != nil {
		driverImpl.Store("go")
		return overrideDriver.g, overrideDriver.lib, nil
	}

	origLib := lib
	var envWantsZig bool
	if lib == GraphicsLibraryAuto {
		// Ebitengine's names are read too, for backward compatibility.
		env := cmp.Or(os.Getenv("GGFX_GRAPHICS_LIBRARY"), os.Getenv("EBITENGINE_GRAPHICS_LIBRARY"), os.Getenv("EBITEN_GRAPHICS_LIBRARY"))

		switch env {
		case "", "auto":
			// Keep the automatic choosing.
		case "opengl":
			lib = GraphicsLibraryOpenGL
		case "directx":
			lib = GraphicsLibraryDirectX
		case "metal":
			lib = GraphicsLibraryMetal
		case "zig":
			envWantsZig = true
		case "vulkan":
			lib = GraphicsLibraryVulkan
			envWantsZig = true
		default:
			return nil, 0, fmt.Errorf("render: an unsupported graphics library is specified by the environment variable: %s", env)
		}
	}

	driverEnv := os.Getenv("GGFX_DRIVER")
	useZig := false
	switch driverEnv {
	case "":
		useZig = zigDefault || os.Getenv("GGFX_DRIVER_LIB") != "" || envWantsZig || lib == GraphicsLibraryVulkan
	case "zig":
		useZig = true
	case "go":
		useZig = false
	default:
		return nil, 0, fmt.Errorf("render: an unsupported driver implementation is specified by GGFX_DRIVER: %s", driverEnv)
	}

	if lib == GraphicsLibraryOpenGL {
		useZig = false
	}

	if useZig {
		g, resolvedLib, err := newZigGraphicsDriver(lib, o)
		if err == nil {
			driverImpl.Store("zig")
			return g, resolvedLib, nil
		}
		if os.Getenv("GGFX_DRIVER_STRICT") == "1" {
			return nil, 0, err
		}
		driverFallbackMu.Lock()
		driverFallbackErr = err
		shouldWarn := !driverFallbackWarned
		driverFallbackWarned = true
		driverFallbackMu.Unlock()
		if shouldWarn {
			fmt.Fprintf(os.Stderr, "ggfx: zig driver unavailable, falling back: %v\n", err)
		}
		if lib == GraphicsLibraryVulkan && origLib == GraphicsLibraryAuto {
			lib = GraphicsLibraryAuto
		}
	}

	if lib == GraphicsLibraryAuto {
		g, resolvedLib, err := newAutoGraphicsDriver(o)
		if err == nil {
			driverImpl.Store("go")
		}
		return g, resolvedLib, err
	}
	g, err := newGraphicsDriverFor(lib, o)
	if err != nil {
		return nil, 0, err
	}
	driverImpl.Store("go")
	return g, lib, nil
}

// errUnsupported is the error for a graphics library the platform does not have.
func errUnsupported(lib GraphicsLibrary) error {
	return fmt.Errorf("render: %s is not supported in this environment", lib)
}

type GraphicsLibrary int

const (
	GraphicsLibraryAuto GraphicsLibrary = iota
	GraphicsLibraryUnknown
	GraphicsLibraryOpenGL
	GraphicsLibraryDirectX
	GraphicsLibraryMetal
	GraphicsLibraryRemote
	GraphicsLibraryVulkan
)

func (g GraphicsLibrary) String() string {
	switch g {
	case GraphicsLibraryAuto:
		return "Auto"
	case GraphicsLibraryUnknown:
		return "Unknown"
	case GraphicsLibraryOpenGL:
		return "OpenGL"
	case GraphicsLibraryDirectX:
		return "DirectX"
	case GraphicsLibraryMetal:
		return "Metal"
	case GraphicsLibraryRemote:
		return "Remote"
	case GraphicsLibraryVulkan:
		return "Vulkan"
	default:
		return fmt.Sprintf("GraphicsLibrary(%d)", g)
	}
}
