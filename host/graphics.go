// Copyright 2014 Hajime Hoshi
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

package host

import (
	"github.com/ironpark/ggfx/internal/render"
)

// GraphicsLibrary represents graphics libraries supported by the renderer.
type GraphicsLibrary int

const (
	// GraphicsLibraryAuto lets the renderer choose the graphics library.
	GraphicsLibraryAuto GraphicsLibrary = GraphicsLibrary(render.GraphicsLibraryAuto)

	// GraphicsLibraryUnknown represents the state at which graphics library cannot be determined,
	// e.g. hasn't loaded yet or failed to initialize.
	GraphicsLibraryUnknown GraphicsLibrary = GraphicsLibrary(render.GraphicsLibraryUnknown)

	// GraphicsLibraryOpenGL represents the graphics library OpenGL.
	GraphicsLibraryOpenGL GraphicsLibrary = GraphicsLibrary(render.GraphicsLibraryOpenGL)

	// GraphicsLibraryDirectX represents the graphics library Microsoft DirectX.
	GraphicsLibraryDirectX GraphicsLibrary = GraphicsLibrary(render.GraphicsLibraryDirectX)

	// GraphicsLibraryMetal represents the graphics library Apple's Metal.
	GraphicsLibraryMetal GraphicsLibrary = GraphicsLibrary(render.GraphicsLibraryMetal)
)

// String returns a string representing the graphics library.
func (g GraphicsLibrary) String() string {
	return render.GraphicsLibrary(g).String()
}

// Ensures GraphicsLibraryAuto is zero (the default value for Options).
var _ [GraphicsLibraryAuto]int = [0]int{}

// ColorSpace represents the color space of the screen.
type ColorSpace int

const (
	// ColorSpaceDefault represents the default color space.
	ColorSpaceDefault ColorSpace = iota

	// ColorSpaceSRGB represents the sRGB color space (https://en.wikipedia.org/wiki/SRGB).
	ColorSpaceSRGB

	// ColorSpaceDisplayP3 represents the Display P3 color space (https://en.wikipedia.org/wiki/DCI-P3).
	ColorSpaceDisplayP3
)
