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
	"errors"
	"fmt"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/directx"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl"
	"github.com/ironpark/ggfx/internal/winver"
)

type graphicsDriverCreatorImpl struct {
	colorSpace color.ColorSpace
}

func (g *graphicsDriverCreatorImpl) newAuto() (graphicsdriver.Graphics, GraphicsLibrary, error) {
	var dxErr error
	var glErr error
	if winver.IsWindows10OrGreater() {
		d, err := g.newDirectX()
		if err == nil {
			return d, GraphicsLibraryDirectX, nil
		}
		dxErr = err

		o, err := g.newOpenGL()
		if err == nil {
			return o, GraphicsLibraryOpenGL, nil
		}
		glErr = err
	} else {
		// Creating a swap chain on an older machine than Windows 10 might fail (#2613).
		// Prefer OpenGL to DirectX.
		o, err := g.newOpenGL()
		if err == nil {
			return o, GraphicsLibraryOpenGL, nil
		}
		glErr = err

		// Initializing OpenGL can fail, though this is pretty rare.
		d, err := g.newDirectX()
		if err == nil {
			return d, GraphicsLibraryDirectX, nil
		}
		dxErr = err
	}

	return nil, GraphicsLibraryUnknown, fmt.Errorf("render: failed to choose graphics drivers: DirectX: %v, OpenGL: %v", dxErr, glErr)
}

func (*graphicsDriverCreatorImpl) newOpenGL() (graphicsdriver.Graphics, error) {
	return opengl.NewGraphics()
}

func (*graphicsDriverCreatorImpl) newDirectX() (graphicsdriver.Graphics, error) {
	return directx.NewGraphics()
}

func (*graphicsDriverCreatorImpl) newMetal() (graphicsdriver.Graphics, error) {
	return nil, errors.New("render: Metal is not supported in this environment")
}

func (*graphicsDriverCreatorImpl) newPlayStation5() (graphicsdriver.Graphics, error) {
	return nil, errors.New("render: PlayStation 5 is not supported in this environment")
}

func newGraphicsDriverCreator(colorSpace color.ColorSpace, canvas any) graphicsDriverCreator {
	return &graphicsDriverCreatorImpl{colorSpace: colorSpace}
}
