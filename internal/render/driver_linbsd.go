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

//go:build (freebsd || (linux && !android) || netbsd) && !nintendosdk && !playstation5

package render

import (
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl"
)

func newGraphicsDriverFor(lib GraphicsLibrary, o *Options) (graphicsdriver.Graphics, error) {
	if lib == GraphicsLibraryOpenGL {
		return opengl.NewGraphics()
	}
	return nil, errUnsupported(lib)
}

func newAutoGraphicsDriver(o *Options) (graphicsdriver.Graphics, GraphicsLibrary, error) {
	g, err := newGraphicsDriverFor(GraphicsLibraryOpenGL, o)
	return g, GraphicsLibraryOpenGL, err
}
