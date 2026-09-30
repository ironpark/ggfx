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

package ggfx

import (
	"github.com/ironpark/ggfx/internal/ui"
)

// UniformScalar is a Go type that a [Uniforms] setter accepts for one scalar element.
type UniformScalar interface {
	int | int32 | uint32 | float32 | float64
}

// Uniforms is the uniform block of one shader, laid out as the shader reads it.
//
// Set values by name, then pass the block as UniformBlock of [DrawTrianglesShaderOptions] or
// [DrawRectShaderOptions]. Unlike the Uniforms map of those options, a setter writes the value to
// the block at once, so a draw neither looks names up nor reflects on values. The block keeps its
// values across draws until they are set again; a uniform never set is zero.
//
// The conversions are those of the map: an integer or a bool converts to a float uniform, and a
// float given for an integer uniform panics. A name the shader does not declare, or a value with
// the wrong number of elements, panics as well.
//
// A draw copies the block, so the block may be changed as soon as the draw returns.
// A Uniforms is not safe for concurrent use.
//
// For the details about uniforms, see docs/shaders.md.
type Uniforms struct {
	shader *Shader
	ui     *ui.Shader
	block  []uint32
}

// NewUniforms returns a zeroed uniform block for s.
//
// If s is disposed, NewUniforms panics.
func (s *Shader) NewUniforms() *Uniforms {
	if s.isDisposed() {
		panic("ggfx: the shader to NewUniforms must not be disposed")
	}
	return &Uniforms{
		shader: s,
		ui:     s.shader,
		block:  make([]uint32, s.shader.UniformDwordCount()),
	}
}

// Set sets the single-element uniform name to v.
func (u *Uniforms) Set[T UniformScalar](name string, v T) {
	ui.PutUniform(u.ui, u.block, name, v)
}

// SetSlice sets the uniform name to v, flattened in WGSL order:
// vector components, matrix columns then rows, array elements.
// len(v) must be the uniform's element count.
func (u *Uniforms) SetSlice[T UniformScalar](name string, v []T) {
	ui.PutUniformSlice(u.ui, u.block, name, v)
}

// SetBool sets the single-element uniform name to 1 if v is true, or 0 otherwise.
func (u *Uniforms) SetBool(name string, v bool) {
	ui.PutUniformBool(u.ui, u.block, name, v)
}

// Reset sets every uniform to zero.
func (u *Uniforms) Reset() {
	clear(u.block)
}

// blockFor returns u's block for a draw with shader. uniforms is the map of the same draw options.
func (u *Uniforms) blockFor(shader *Shader, uniforms map[string]any) []uint32 {
	if uniforms != nil {
		panic("ggfx: Uniforms and UniformBlock must not be specified at the same time")
	}
	if u.shader != shader {
		panic("ggfx: UniformBlock must be created by the shader it is drawn with")
	}
	return u.block
}
