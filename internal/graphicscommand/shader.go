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
	"github.com/ironpark/ggfx/internal/graphics"
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

	// regionReads is which regions of the internal uniform block the shader reads, once asked
	// (see readsRegions).
	regionReads uint8
}

const (
	regionReadsKnown uint8 = 1 << iota
	regionReadsDst
	regionReadsSrc
)

// readsRegions reports whether the shader reads the destination region and the source regions of
// the internal uniform block. Draws that differ in a region it does not read have the same
// uniforms once FilterInternalUniforms has zeroed it.
func (s *Shader) readsRegions() (dst, src bool) {
	if s.regionReads&regionReadsKnown == 0 {
		r := regionReadsKnown
		if s.ir == nil || s.ir.ReadsInternalUniforms(graphics.DstRegionOriginUniformDwordIndex, graphics.SourceTextureSizeUniformDwordIndex) {
			r |= regionReadsDst
		}
		if s.ReadsSourceRegions() {
			r |= regionReadsSrc
		}
		s.regionReads = r
	}
	return s.regionReads&regionReadsDst != 0, s.regionReads&regionReadsSrc != 0
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
	if s.ir == nil {
		return true
	}
	return s.ir.ReadsInternalUniforms(graphics.SourceImageRegionOriginUniformDwordIndex, graphics.SourceImageRegionSizeUniformDwordIndex+4*graphics.ShaderSrcImageCount)
}
