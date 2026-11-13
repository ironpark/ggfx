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

package shader

import (
	"sync"
)

// vertexStub is the fragment function of VertexProgram. The prelude's fragment entry point takes
// every varying as one struct argument whatever the user's function reads, so the stub does not
// change the vertex stage's interface.
const vertexStub = `fn fragment(v: Vertex) -> vec4f { return v.color; }`

var (
	vertexProgramsM sync.Mutex
	vertexPrograms  = map[int]func() (*Program, error){}
)

// VertexProgram returns the program whose vertex stage stands for the vertex stage of every
// program Compile makes with textureCount source textures: the prelude fixes the vertex entry
// point, so its attributes, varyings and internal uniform block are the same for any user shader.
// A driver can translate and compile this vertex stage once and pair it with each program's
// fragment stage. The program is built on the first call for a textureCount and shared after;
// callers must not modify it.
func VertexProgram(textureCount int) (*Program, error) {
	vertexProgramsM.Lock()
	f, ok := vertexPrograms[textureCount]
	if !ok {
		f = sync.OnceValues(func() (*Program, error) {
			return Compile([]byte(vertexStub), textureCount)
		})
		vertexPrograms[textureCount] = f
	}
	vertexProgramsM.Unlock()
	return f()
}
