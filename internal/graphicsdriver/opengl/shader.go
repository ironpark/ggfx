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

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl/gl"
)

type Shader struct {
	graphics *Graphics

	desc *graphicsdriver.ShaderDescriptor
	p    program

	// linked reports whether the link status was checked and the uniform blocks and samplers
	// were set up. That waits for the link, so it is done at the first draw: with
	// KHR_parallel_shader_compile, shaders made one after another compile in parallel until then.
	linked bool

	// vertex is the shared vertex stage attached to p. fs is the fragment stage, and fssrc its
	// source, kept for the error message until linked.
	vertex         *vertexShader
	fs             shader
	fssrc          string
	fsBlocks       []graphicsdriver.ShaderUniformBlock
	fsTextureNames []string

	// samplerLocations are the locations of the sampler uniforms for the source textures, -1 for a
	// texture the fragment shader does not read. samplerUnits are the texture units last set to
	// them, -1 for none yet: a program keeps its uniforms, and the units rarely change.
	samplerLocations [graphics.ShaderSrcImageCount]int32
	samplerUnits     [graphics.ShaderSrcImageCount]int32
}

func newShader(graphics *Graphics, desc *graphicsdriver.ShaderDescriptor) (*Shader, error) {
	if desc == nil || desc.Source == nil {
		return nil, fmt.Errorf("opengl: shader descriptor has no source")
	}
	s := &Shader{
		graphics: graphics,
		desc:     desc,
	}
	if err := s.compile(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Shader) Dispose() {
	s.deleteFragmentShader()
	s.graphics.deleteProgram(s.p)
}

// vertexShader is a vertex shader object shared by every program of a texture count: the prelude
// fixes the vertex stage, so one compiled from ShaderSource.VertexStage stands for all of them.
type vertexShader struct {
	shader shader
	src    string
	blocks []graphicsdriver.ShaderUniformBlock
}

// vertexShader returns the shared vertex shader for textureCount, compiling it on the first call.
// The shader objects live as long as the context; openGLState.reset forgets them.
func (g *Graphics) vertexShader(textureCount int, src graphicsdriver.ShaderSource) (*vertexShader, error) {
	if vs, ok := g.state.vertexShaders[textureCount]; ok {
		return vs, nil
	}
	code, err := src.VertexStage(g.context.shaderTarget())
	if err != nil {
		return nil, fmt.Errorf("opengl: translating the vertex shader to GLSL failed: %w", err)
	}
	vssrc := string(code.Code)
	sh, err := g.context.newShader(gl.VERTEX_SHADER, vssrc)
	if err != nil {
		return nil, err
	}
	vs := &vertexShader{shader: sh, src: vssrc, blocks: code.UniformBlocks}
	if g.state.vertexShaders == nil {
		g.state.vertexShaders = map[int]*vertexShader{}
	}
	g.state.vertexShaders[textureCount] = vs
	return vs, nil
}

func (s *Shader) compile() error {
	vs, err := s.graphics.vertexShader(s.desc.TextureCount, s.desc.Source)
	if err != nil {
		return err
	}

	fscode, err := s.desc.Source.FragmentStage(s.graphics.context.shaderTarget())
	if err != nil {
		return fmt.Errorf("opengl: translating the fragment shader to GLSL failed: %w", err)
	}
	fssrc := string(fscode.Code)

	// Without the extension enabled, a browser may compile in the background all the same, but
	// need not.
	s.graphics.context.enableParallelShaderCompile()

	fs, err := s.graphics.context.newShader(gl.FRAGMENT_SHADER, fssrc)
	if err != nil {
		return err
	}

	p, err := s.graphics.context.newProgram([]shader{vs.shader, fs})
	if err != nil {
		s.graphics.context.ctx.DeleteShader(uint32(fs))
		return err
	}

	s.p = p
	s.vertex = vs
	s.fs = fs
	s.fssrc = fssrc
	s.fsBlocks = fscode.UniformBlocks
	s.fsTextureNames = fscode.TextureNames
	return nil
}

// link checks the link status of the program, and attaches its uniform blocks and finds its
// samplers. It waits for the program to be linked.
func (s *Shader) link() error {
	if s.linked {
		return nil
	}
	ctx := s.graphics.context.ctx
	p := s.p

	if ctx.GetProgrami(uint32(p), gl.LINK_STATUS) == gl.FALSE {
		programInfo := ctx.GetProgramInfoLog(uint32(p))
		vertexShaderInfo := ctx.GetShaderInfoLog(uint32(s.vertex.shader))
		fragmentShaderInfo := ctx.GetShaderInfoLog(uint32(s.fs))
		return fmt.Errorf("opengl: program error: %s\nvertex shader error: %s\nvertex shader source: %s\nfragment shader error: %s\nfragment shader source: %s",
			programInfo, vertexShaderInfo, s.vertex.src, fragmentShaderInfo, s.fssrc)
	}
	s.deleteFragmentShader()

	// Attach the uniform blocks to their binding points. A block a stage does not read is not
	// emitted, so bind whatever the stage reports. The vertex stage's blocks are the shared vertex
	// shader's.
	for _, blocks := range [][]graphicsdriver.ShaderUniformBlock{s.vertex.blocks, s.fsBlocks} {
		for _, u := range blocks {
			idx := ctx.GetUniformBlockIndex(uint32(p), u.Name)
			if idx == gl.INVALID_INDEX {
				continue
			}
			switch u.Kind {
			case graphicsdriver.ShaderUniformBlockInternal:
				ctx.UniformBlockBinding(uint32(p), idx, graphicsdriver.GLInternalUniformBlockBinding)
			case graphicsdriver.ShaderUniformBlockUser:
				ctx.UniformBlockBinding(uint32(p), idx, graphicsdriver.GLUserUniformBlockBinding)
			}
		}
	}

	for i := range s.samplerLocations {
		s.samplerLocations[i] = -1
		s.samplerUnits[i] = -1
		if i < len(s.fsTextureNames) && s.fsTextureNames[i] != "" {
			s.samplerLocations[i] = ctx.GetUniformLocation(uint32(p), s.fsTextureNames[i])
		}
	}

	s.linked = true
	s.fssrc = ""
	s.fsBlocks = nil
	s.fsTextureNames = nil
	return nil
}

// deleteFragmentShader deletes the fragment shader object. Attached to the program, it lives as
// long as the program.
func (s *Shader) deleteFragmentShader() {
	if s.fs == 0 {
		return
	}
	s.graphics.context.ctx.DeleteShader(uint32(s.fs))
	s.fs = 0
}
