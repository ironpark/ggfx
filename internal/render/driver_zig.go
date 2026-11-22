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
	"cmp"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/zigdriver"
)

func parseZigDXFeatureLevel(str string) (uint32, bool) {
	switch str {
	case "11_0":
		return 0xb000, true
	case "11_1":
		return 0xb100, true
	case "12_0":
		return 0xc000, true
	case "12_1":
		return 0xc100, true
	case "12_2":
		return 0xc200, true
	default:
		return 0, false
	}
}

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

	var flags uint32
	var dxFeatureLevel uint32

	if os.Getenv("GGFX_DRIVER_DEBUG") == "1" {
		flags |= zigdriver.DeviceFlagDebug
	}
	if os.Getenv("GGFX_METAL_SWAPCHAIN") == "0" {
		flags |= zigdriver.DeviceFlagMetalNoIOSurface
	}
	if env := cmp.Or(os.Getenv("GGFX_DIRECTX_FEATURE_LEVEL"), os.Getenv("EBITENGINE_DIRECTX_FEATURE_LEVEL")); env != "" {
		if fl, ok := parseZigDXFeatureLevel(env); ok {
			dxFeatureLevel = fl
		}
	}
	if env := cmp.Or(os.Getenv("GGFX_DIRECTX"), os.Getenv("EBITENGINE_DIRECTX"), os.Getenv("EBITEN_DIRECTX")); env != "" {
		for tok := range strings.SplitSeq(env, ",") {
			tok = strings.TrimSpace(tok)
			switch {
			case tok == "warp":
				flags |= zigdriver.DeviceFlagWARP
			case tok == "debug":
				flags |= zigdriver.DeviceFlagDebug
			case tok == "dred":
				flags |= zigdriver.DeviceFlagDRED
			case strings.HasPrefix(tok, "featurelevel="):
				if fl, ok := parseZigDXFeatureLevel(tok[len("featurelevel="):]); ok {
					dxFeatureLevel = fl
				}
			}
		}
	}

	var slots int
	if env := os.Getenv("GGFX_DRIVER_SLOTS"); env != "" {
		if n, err := strconv.Atoi(env); err == nil && (n == 2 || n == 3) {
			slots = n
		}
	}

	g, err := zigdriver.NewGraphics(zigdriver.Options{
		ColorSpace:     o.ColorSpace,
		Backend:        backend,
		Flags:          flags,
		Slots:          slots,
		DXFeatureLevel: dxFeatureLevel,
	})
	if err != nil {
		return nil, 0, err
	}
	return g, outLib, nil
}
