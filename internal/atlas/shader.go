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

package atlas

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/ironpark/ggfx/internal/builtinshader"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/shader"
)

// Shader is a compiled program and the lifetime of its GPU shader: the command queue's shader is made
// at the first draw and disposed with the queue's order kept outside a frame. The uniform layout is
// the program's.
type Shader struct {
	ir      *shader.Program
	shader  *graphicscommand.Shader
	name    string
	cleanup runtime.Cleanup
}

func NewShader(ir *shader.Program, name string) *Shader {
	// A shader is initialized lazily, and the lock is not needed.
	return &Shader{
		ir:   ir,
		name: name,
	}
}

// Program returns the compiled program, whose uniform layout the draws' uniforms follow.
func (s *Shader) Program() *shader.Program {
	return s.ir
}

func (s *Shader) ensureShader() *graphicscommand.Shader {
	if s.shader != nil {
		return s.shader
	}
	s.shader = graphicscommand.NewShader(s.ir, s.name)
	// The cleanup argument must not refer to s, or s would never be collected.
	s.cleanup = runtime.AddCleanup(s, func(internalShader *graphicscommand.Shader) {
		// A function from cleanup must not be blocked, but disposing operation can be blocked.
		// Defer this operation until it becomes safe. (#913)
		appendDeferred(func() {
			internalShader.Dispose()
		})
	}, s.shader)
	return s.shader
}

// Deallocate deallocates the internal state.
func (s *Shader) Deallocate() {
	backendsM.Lock()
	defer backendsM.Unlock()

	if !inFrame {
		appendDeferred(func() {
			s.deallocate()
		})
		return
	}

	s.deallocate()
}

func (s *Shader) deallocate() {
	s.cleanup.Stop()
	if s.shader == nil {
		return
	}
	s.shader.Dispose()
	s.shader = nil
}

// The built-in shaders are compiled at their first use, so that importing atlas costs nothing.
var (
	NearestFilterShader = sync.OnceValue(func() *Shader {
		return newBuiltinShader(builtinshader.ShaderSource(builtinshader.FilterNearest, builtinshader.AddressUnsafe), "nearest")
	})
	LinearFilterShader = sync.OnceValue(func() *Shader {
		return newBuiltinShader(builtinshader.ShaderSource(builtinshader.FilterLinear, builtinshader.AddressUnsafe), "linear")
	})
	clearShader = sync.OnceValue(func() *Shader {
		return newBuiltinShader([]byte(builtinshader.ClearShaderSource), "clear")
	})
)

func newBuiltinShader(src []byte, name string) *Shader {
	ir, err := shader.CompileUser(src)
	if err != nil {
		panic(fmt.Errorf("atlas: compiling the %s shader failed: %w", name, err))
	}
	return NewShader(ir, name)
}
