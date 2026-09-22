// Copyright 2020 The Ebiten Authors
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

package ggfx

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/builtinshader"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/ui"
)

// Shader represents a compiled shader program.
//
// For the details about shaders, see docs/shaders.md.
type Shader struct {
	shader *ui.Shader
}

func NewShader(src []byte) (*Shader, error) {
	return newShader(src, "")
}

func newShader(src []byte, name string) (*Shader, error) {
	program, err := graphics.CompileShader(src)
	if err != nil {
		return nil, err
	}
	return &Shader{
		shader: ui.NewShader(program, name),
	}, nil
}

func (s *Shader) Dispose() {
	s.shader.Deallocate()
	s.shader = nil
}

func (s *Shader) isDisposed() bool {
	return s.shader == nil
}

// Deallocate deallocates the internal state of the shader.
// Even after Deallocate is called, the shader is still available.
// In this case, the shader's internal state is allocated again.
//
// Usually, you don't have to call Deallocate since the internal state is automatically released by GC.
// However, if you are sure that the shader is no longer used but not sure how this shader object is referred,
// you can call Deallocate to make sure that the internal state is deallocated.
//
// If the shader is disposed, Deallocate does nothing.
func (s *Shader) Deallocate() {
	if s.shader == nil {
		return
	}
	s.shader.Deallocate()
}

func (s *Shader) appendUniforms(dst []uint32, uniforms map[string]any) []uint32 {
	return s.shader.AppendUniforms(dst, uniforms)
}

var (
	builtinShadersForRead atomic.Pointer[[builtinshader.FilterCount][builtinshader.AddressCount]*Shader]
	builtinShadersM       sync.Mutex
)

func builtinShader(filter builtinshader.Filter, address builtinshader.Address) *Shader {
	if read := builtinShadersForRead.Load(); read != nil {
		if s := (*read)[filter][address]; s != nil {
			return s
		}
	}

	builtinShadersM.Lock()
	defer builtinShadersM.Unlock()

	// Double check in case another goroutine already created a shader.
	if read := builtinShadersForRead.Load(); read != nil {
		if s := (*read)[filter][address]; s != nil {
			return s
		}
	}

	var shader *Shader
	if address == builtinshader.AddressUnsafe && (filter == builtinshader.FilterNearest || filter == builtinshader.FilterLinear) {
		switch filter {
		case builtinshader.FilterNearest:
			shader = &Shader{shader: ui.NearestFilterShader}
		case builtinshader.FilterLinear:
			shader = &Shader{shader: ui.LinearFilterShader}
		}
	} else {
		var name string
		switch filter {
		case builtinshader.FilterNearest:
			name = "nearest"
		case builtinshader.FilterLinear:
			name = "linear"
		case builtinshader.FilterPixelated:
			name = "pixelated"
		}
		switch address {
		case builtinshader.AddressClampToZero:
			name += "-clamptozero"
		case builtinshader.AddressRepeat:
			name += "-repeat"
		}
		s, err := newShader(builtinshader.ShaderSource(filter, address), name)
		if err != nil {
			panic(fmt.Sprintf("ggfx: NewShader for a built-in shader failed: %v", err))
		}
		shader = s
	}

	var shaders [builtinshader.FilterCount][builtinshader.AddressCount]*Shader
	if ptr := builtinShadersForRead.Load(); ptr != nil {
		shaders = *ptr
	}
	shaders[filter][address] = shader
	builtinShadersForRead.Store(&shaders)
	return shader
}
