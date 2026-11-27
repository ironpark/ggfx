// Copyright 2024 The Ebitengine Authors
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

//go:build darwin && !ios

package opengl

import (
	"fmt"
	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl/gl"
)

// NewGraphics creates an implementation of graphicsdriver.Graphics for OpenGL.
// The returned graphics value is nil iff the error is not nil.
func NewGraphics() (graphicsdriver.Graphics, error) {
	created.Store(true)
	ctx, err := gl.NewDefaultContext()
	if err != nil {
		return nil, err
	}

	return newGraphics(ctx, color.ColorSpaceSRGB), nil
}

// presenterFromDescriptor takes the Presenter a surface's frames are presented through.
func presenterFromDescriptor(desc graphicsdriver.SurfaceDescriptor) (Presenter, error) {
	if desc.Kind != graphicsdriver.SurfaceKindGLPresenter {
		return nil, fmt.Errorf("opengl: NewSurface needs SurfaceKindGLPresenter but got %s", desc.Kind)
	}
	if err := desc.Validate(); err != nil {
		return nil, err
	}
	return desc.GLPresenter, nil
}
