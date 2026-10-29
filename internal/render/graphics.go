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

	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// newGraphicsDriver creates the driver for lib. For GraphicsLibraryAuto, the environment variable
// can name one, and otherwise the platform chooses.
func newGraphicsDriver(lib GraphicsLibrary, o *Options) (graphicsdriver.Graphics, GraphicsLibrary, error) {
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
		default:
			return nil, 0, fmt.Errorf("render: an unsupported graphics library is specified by the environment variable: %s", env)
		}
	}

	if lib == GraphicsLibraryAuto {
		return newAutoGraphicsDriver(o)
	}
	g, err := newGraphicsDriverFor(lib, o)
	if err != nil {
		return nil, 0, err
	}
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
	default:
		return fmt.Sprintf("GraphicsLibrary(%d)", g)
	}
}
