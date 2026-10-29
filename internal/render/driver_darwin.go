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

//go:build !ios

package render

import (
	"fmt"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl"
)

func newGraphicsDriverFor(lib GraphicsLibrary, o *Options) (graphicsdriver.Graphics, error) {
	switch lib {
	case GraphicsLibraryMetal:
		return metal.NewGraphics(o.ColorSpace)
	case GraphicsLibraryOpenGL:
		return opengl.NewGraphics()
	}
	return nil, errUnsupported(lib)
}

func newAutoGraphicsDriver(o *Options) (graphicsdriver.Graphics, GraphicsLibrary, error) {
	m, err1 := newGraphicsDriverFor(GraphicsLibraryMetal, o)
	if err1 == nil {
		return m, GraphicsLibraryMetal, nil
	}
	g, err2 := newGraphicsDriverFor(GraphicsLibraryOpenGL, o)
	if err2 == nil {
		return g, GraphicsLibraryOpenGL, nil
	}
	return nil, GraphicsLibraryUnknown, fmt.Errorf("render: failed to choose graphics drivers: Metal: %v, OpenGL: %v", err1, err2)
}
