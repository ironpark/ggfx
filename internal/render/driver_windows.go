// Copyright 2016 Hajime Hoshi
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
	"fmt"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/directx"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl"
)

func newGraphicsDriverFor(lib GraphicsLibrary, o *Options) (graphicsdriver.Graphics, error) {
	switch lib {
	case GraphicsLibraryDirectX:
		return directx.NewGraphics()
	case GraphicsLibraryOpenGL:
		return opengl.NewGraphics()
	}
	return nil, errUnsupported(lib)
}

func newAutoGraphicsDriver(o *Options) (graphicsdriver.Graphics, GraphicsLibrary, error) {
	libs := []GraphicsLibrary{GraphicsLibraryDirectX, GraphicsLibraryOpenGL}
	var errs [2]error
	for i, lib := range libs {
		g, err := newGraphicsDriverFor(lib, o)
		if err == nil {
			return g, lib, nil
		}
		errs[i] = err
	}
	return nil, GraphicsLibraryUnknown, fmt.Errorf("render: failed to choose graphics drivers: %s: %v, %s: %v", libs[0], errs[0], libs[1], errs[1])
}
