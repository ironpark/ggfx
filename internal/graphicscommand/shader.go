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

package graphicscommand

import (
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/shader"
)

var nextShaderID = 1

func genNextShaderID() int {
	id := nextShaderID
	nextShaderID++
	return id
}

type Shader struct {
	shader graphicsdriver.Shader
	ir     *shader.Program
	id     int

	// name is used only for logging.
	name string
}

// readsRegions reports whether the shader reads the destination region and the source regions of
// the internal uniform block. Draws that differ in a region it does not read have the same
// uniforms once FilterInternalUniforms has zeroed it. A shader without an IR is taken to read both.
func (s *Shader) readsRegions() (dst, src bool) {
	if s.ir == nil {
		return true, true
	}
	return s.ir.ReadsDstRegion, s.ir.ReadsSrcRegions
}

func NewShader(ir *shader.Program, name string) *Shader {
	s := &Shader{
		ir:   ir,
		id:   genNextShaderID(),
		name: name,
	}
	c := &newShaderCommand{
		result: s,
		ir:     ir,
	}
	theCommandQueueManager.enqueueCommand(c)
	return s
}

func (s *Shader) Dispose() {
	c := &disposeShaderCommand{
		target: s,
	}
	theCommandQueueManager.enqueueCommand(c)
}

// ReadsSourceRegions reports whether the shader reads the regions of its source images: draws from
// different regions of one image can be merged into one only when it does not, as the regions are
// then zeroed out of their uniforms (see EnqueueDrawTrianglesCommand).
func (s *Shader) ReadsSourceRegions() bool {
	_, src := s.readsRegions()
	return src
}
