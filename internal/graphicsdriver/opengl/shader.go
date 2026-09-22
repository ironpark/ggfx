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

package opengl

import (
	"fmt"
	"runtime"

	"github.com/gogpu/naga/glsl"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl/gl"
	ggshader "github.com/ironpark/ggfx/internal/shader"
)

// The uniform buffer binding points the shader's uniform blocks are bound at.
const (
	internalUniformBlockBinding = 0
	userUniformBlockBinding     = 1
)

type Shader struct {
	id       graphicsdriver.ShaderID
	graphics *Graphics

	program *ggshader.Program
	p       program

	// textureNames are the names of the sampler uniforms for the source textures. An empty name
	// means the fragment shader does not read that texture.
	textureNames []string
}

func newShader(id graphicsdriver.ShaderID, graphics *Graphics, program *ggshader.Program) (*Shader, error) {
	s := &Shader{
		id:       id,
		graphics: graphics,
		program:  program,
	}
	if err := s.compile(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Shader) ID() graphicsdriver.ShaderID {
	return s.id
}

func (s *Shader) Dispose() {
	s.graphics.deleteProgram(s.p)
	s.graphics.removeShader(s)
}

// glslOptions returns the options to translate entryPoint of the shader to GLSL.
func glslOptions(version glsl.Version, entryPoint string) glsl.Options {
	o := glsl.DefaultOptions()
	o.LangVersion = version
	o.EntryPoint = entryPoint
	o.BoundsCheckPolicies.ImageLoad = glsl.BoundsCheckUnchecked
	return o
}

// glslTextureName returns the name naga gives the sampler uniform of the source texture i in the
// fragment stage.
func glslTextureName(i int) string {
	b := ggshader.TextureBinding(i)
	return fmt.Sprintf("_group_%d_binding_%d_fs", b.Group, b.Binding)
}

func (s *Shader) compile() error {
	version := s.graphics.context.glslVersion()

	vssrc, vsinfo, err := glsl.Compile(s.program.Module, glslOptions(version, ggshader.VertexEntryPoint))
	if err != nil {
		return fmt.Errorf("opengl: translating the vertex shader to GLSL failed: %w", err)
	}
	fssrc, fsinfo, err := glsl.Compile(s.program.Module, glslOptions(version, ggshader.FragmentEntryPoint))
	if err != nil {
		return fmt.Errorf("opengl: translating the fragment shader to GLSL failed: %w", err)
	}

	vs, err := s.graphics.context.newShader(gl.VERTEX_SHADER, vssrc)
	if err != nil {
		return err
	}
	defer s.graphics.context.ctx.DeleteShader(uint32(vs))

	fs, err := s.graphics.context.newShader(gl.FRAGMENT_SHADER, fssrc)
	if err != nil {
		return err
	}
	defer s.graphics.context.ctx.DeleteShader(uint32(fs))

	p, err := s.graphics.context.newProgram([]shader{vs, fs})
	if err != nil {
		return err
	}

	if s.graphics.context.hasParallelShaderCompile() {
		for s.graphics.context.ctx.GetShaderi(uint32(vs), gl.COMPLETION_STATUS_KHR) != gl.TRUE ||
			s.graphics.context.ctx.GetShaderi(uint32(fs), gl.COMPLETION_STATUS_KHR) != gl.TRUE {
			runtime.Gosched()
		}
	}

	if s.graphics.context.ctx.GetProgrami(uint32(p), gl.LINK_STATUS) == gl.FALSE {
		programInfo := s.graphics.context.ctx.GetProgramInfoLog(uint32(p))
		vertexShaderInfo := s.graphics.context.ctx.GetShaderInfoLog(uint32(vs))
		fragmentShaderInfo := s.graphics.context.ctx.GetShaderInfoLog(uint32(fs))
		s.graphics.deleteProgram(p)
		return fmt.Errorf("opengl: program error: %s\nvertex shader error: %s\nvertex shader source: %s\nfragment shader error: %s\nfragment shader source: %s",
			programInfo, vertexShaderInfo, vssrc, fragmentShaderInfo, fssrc)
	}

	// Attach the uniform blocks to their binding points. A block a stage does not read is not
	// emitted, so bind whatever naga reports.
	for _, info := range []glsl.TranslationInfo{vsinfo, fsinfo} {
		for _, u := range info.Uniforms {
			idx := s.graphics.context.ctx.GetUniformBlockIndex(uint32(p), u.BlockName)
			if idx == gl.INVALID_INDEX {
				continue
			}
			switch u.Binding {
			case ggshader.InternalUniformBinding:
				s.graphics.context.ctx.UniformBlockBinding(uint32(p), idx, internalUniformBlockBinding)
			case ggshader.UserUniformBinding:
				s.graphics.context.ctx.UniformBlockBinding(uint32(p), idx, userUniformBlockBinding)
			}
		}
	}

	s.textureNames = make([]string, s.program.TextureCount)
	for i := range s.textureNames {
		name := glslTextureName(i)
		if s.graphics.context.ctx.GetUniformLocation(uint32(p), name) >= 0 {
			s.textureNames[i] = name
		}
	}

	s.p = p
	return nil
}
