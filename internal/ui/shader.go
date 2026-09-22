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
	// uniformDwordCount is the size of the user's uniform block in dwords.
	uniformDwordCount int
}

func NewShader(program *shader.Program, name string) *Shader {
	return &Shader{
		shader:            atlas.NewShader(program, name),
		uniforms:          program.Uniforms,
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

func uniformScalar(name string, kind ir.ScalarKind, v reflect.Value) uint32 {
	switch v.Kind() {
	case reflect.Bool:
		var n uint32
		if v.Bool() {
			n = 1
		}
		if kind == ir.ScalarFloat {
			return math.Float32bits(float32(n))
		}
		return n
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if kind == ir.ScalarFloat {
			return math.Float32bits(float32(v.Int()))
		}
		return uint32(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if kind == ir.ScalarFloat {
			return math.Float32bits(float32(v.Uint()))
		}
		return uint32(v.Uint())
	case reflect.Float32, reflect.Float64:
		if kind != ir.ScalarFloat {
			panic(fmt.Sprintf("ui: unexpected uniform value type for %s: a float was given for an integer uniform", name))
		}
		return math.Float32bits(float32(v.Float()))
	default:
		panic(fmt.Sprintf("ui: unexpected uniform value type for %s: %s", name, v.Kind().String()))
	}
}
