// Copyright 2022 The Ebiten Authors
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

package ui

import (
	"fmt"
	"math"
	"reflect"

	"github.com/gogpu/naga/ir"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/shader"
)

type Shader struct {
	shader   *atlas.Shader
	uniforms []shader.Uniform
	// uniformIndices maps a uniform's name to its index in uniforms.
	uniformIndices map[string]int
	// uniformDwordCount is the size of the user's uniform block in dwords.
	uniformDwordCount int
}

func NewShader(program *shader.Program, name string) *Shader {
	indices := make(map[string]int, len(program.Uniforms))
	for i, u := range program.Uniforms {
		indices[u.Name] = i
	}
	return &Shader{
		shader:            atlas.NewShader(program, name),
		uniforms:          program.Uniforms,
		uniformIndices:    indices,
		uniformDwordCount: program.UniformDwordCount,
	}
}

func (s *Shader) Deallocate() {
	s.shader.Deallocate()
}

// AppendUniforms appends the user's uniform block, laid out as the shader expects it, to dst.
// A name in uniforms that the shader does not declare is ignored (#2710). A value whose element
// count does not match its uniform panics.
func (s *Shader) AppendUniforms(dst []uint32, uniforms map[string]any) []uint32 {
	origLen := len(dst)
	if cap(dst)-len(dst) >= s.uniformDwordCount {
		dst = dst[:len(dst)+s.uniformDwordCount]
		clear(dst[origLen:])
	} else {
		dst = append(dst, make([]uint32, s.uniformDwordCount)...)
	}
	block := dst[origLen:]

	for i := range s.uniforms {
		u := &s.uniforms[i]
		uv, ok := uniforms[u.Name]
		if !ok {
			continue
		}
		v := reflect.ValueOf(uv)
		switch v.Kind() {
		case reflect.Slice, reflect.Array:
			if got, want := v.Len(), len(u.Slots); got != want {
				panic(fmt.Sprintf("ui: unexpected uniform value length for %s: got %d, want %d", u.Name, got, want))
			}
			for j := range v.Len() {
				block[u.Slots[j]] = uniformScalar(u.Name, u.Kinds[j], v.Index(j))
			}
		default:
			if len(u.Slots) != 1 {
				panic(fmt.Sprintf("ui: unexpected uniform value for %s: a scalar was given for %d elements", u.Name, len(u.Slots)))
			}
			block[u.Slots[0]] = uniformScalar(u.Name, u.Kinds[0], v)
		}
	}

	return dst
}

// UniformDwordCount returns the size of the user's uniform block in dwords.
func (s *Shader) UniformDwordCount() int {
	return s.uniformDwordCount
}

// UniformScalar is a Go type that PutUniform accepts for one scalar element.
type UniformScalar interface {
	int | int32 | uint32 | float32 | float64
}

// PutUniform writes v, a single scalar, to the uniform name in block, laid out as AppendUniforms
// lays it out. An unknown name or a uniform of more than one element panics.
func PutUniform[T UniformScalar](s *Shader, block []uint32, name string, v T) {
	u := s.uniform(name)
	if len(u.Slots) != 1 {
		panic(fmt.Sprintf("ui: unexpected uniform value for %s: a scalar was given for %d elements", u.Name, len(u.Slots)))
	}
	block[u.Slots[0]] = uniformBits(u.Name, u.Kinds[0], v)
}

// PutUniformSlice writes v to the uniform name in block, flattened as AppendUniforms flattens a
// slice. An unknown name or an element count mismatch panics.
func PutUniformSlice[T UniformScalar](s *Shader, block []uint32, name string, v []T) {
	u := s.uniform(name)
	if got, want := len(v), len(u.Slots); got != want {
		panic(fmt.Sprintf("ui: unexpected uniform value length for %s: got %d, want %d", u.Name, got, want))
	}
	for j, e := range v {
		block[u.Slots[j]] = uniformBits(u.Name, u.Kinds[j], e)
	}
}

// PutUniformBool writes v, as 0 or 1, to the single-element uniform name in block.
func PutUniformBool(s *Shader, block []uint32, name string, v bool) {
	u := s.uniform(name)
	if len(u.Slots) != 1 {
		panic(fmt.Sprintf("ui: unexpected uniform value for %s: a scalar was given for %d elements", u.Name, len(u.Slots)))
	}
	block[u.Slots[0]] = uniformFromBool(u.Kinds[0], v)
}

// uniform returns the uniform name. Unlike AppendUniforms, which ignores a name the shader does
// not declare, it panics: a setter is bound to one shader, so an unknown name is a mistake.
func (s *Shader) uniform(name string) *shader.Uniform {
	i, ok := s.uniformIndices[name]
	if !ok {
		panic(fmt.Sprintf("ui: the shader has no uniform %q", name))
	}
	return &s.uniforms[i]
}

func uniformBits[T UniformScalar](name string, kind ir.ScalarKind, v T) uint32 {
	switch v := any(v).(type) {
	case int:
		return uniformFromInt(kind, int64(v))
	case int32:
		return uniformFromInt(kind, int64(v))
	case uint32:
		return uniformFromUint(kind, uint64(v))
	case float32:
		return uniformFromFloat(name, kind, float64(v))
	case float64:
		return uniformFromFloat(name, kind, v)
	}
	panic("ui: not reached")
}

func uniformScalar(name string, kind ir.ScalarKind, v reflect.Value) uint32 {
	switch v.Kind() {
	case reflect.Bool:
		return uniformFromBool(kind, v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return uniformFromInt(kind, v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return uniformFromUint(kind, v.Uint())
	case reflect.Float32, reflect.Float64:
		return uniformFromFloat(name, kind, v.Float())
	default:
		panic(fmt.Sprintf("ui: unexpected uniform value type for %s: %s", name, v.Kind().String()))
	}
}

// The uniformFrom functions convert one Go scalar to a uniform element of kind. An integer or a
// bool converts to a float element; a float does not convert to an integer element.

func uniformFromBool(kind ir.ScalarKind, v bool) uint32 {
	var n uint32
	if v {
		n = 1
	}
	if kind == ir.ScalarFloat {
		return math.Float32bits(float32(n))
	}
	return n
}

func uniformFromInt(kind ir.ScalarKind, v int64) uint32 {
	if kind == ir.ScalarFloat {
		return math.Float32bits(float32(v))
	}
	return uint32(v)
}

func uniformFromUint(kind ir.ScalarKind, v uint64) uint32 {
	if kind == ir.ScalarFloat {
		return math.Float32bits(float32(v))
	}
	return uint32(v)
}

func uniformFromFloat(name string, kind ir.ScalarKind, v float64) uint32 {
	if kind != ir.ScalarFloat {
		panic(fmt.Sprintf("ui: unexpected uniform value type for %s: a float was given for an integer uniform", name))
	}
	return math.Float32bits(float32(v))
}
