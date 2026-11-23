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
	"runtime"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/zigdriver"
)

func newZigGraphicsDriver(lib GraphicsLibrary, o *Options) (graphicsdriver.Graphics, GraphicsLibrary, error) {
	var backend uint32
	var outLib GraphicsLibrary

	switch runtime.GOOS {
	case "darwin":
		if lib != GraphicsLibraryAuto && lib != GraphicsLibraryMetal {
			return nil, 0, errUnsupported(lib)
		}
		backend = zigdriver.BackendMetal
		outLib = GraphicsLibraryMetal
	case "windows":
		if lib != GraphicsLibraryAuto && lib != GraphicsLibraryDirectX {
			return nil, 0, errUnsupported(lib)
		}
		backend = zigdriver.BackendDX12
		outLib = GraphicsLibraryDirectX
	case "linux":
		if lib != GraphicsLibraryAuto && lib != GraphicsLibraryVulkan {
			return nil, 0, errUnsupported(lib)
		}
		backend = zigdriver.BackendVulkan
		outLib = GraphicsLibraryVulkan
	default:
		return nil, 0, errUnsupported(lib)
	}

	// The driver reads its own environment variables (debug layers, DirectX
	// options, ring slots); only the backend choice is decided here.
	g, err := zigdriver.NewGraphics(zigdriver.Options{
		ColorSpace: o.ColorSpace,
		Backend:    backend,
	})
	if err != nil {
		return nil, 0, err
	}
	return g, outLib, nil
}
