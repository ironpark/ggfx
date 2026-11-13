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

	"github.com/gogpu/naga/backend"
	"github.com/gogpu/naga/glsl"
	"github.com/gogpu/naga/ir"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl/gl"
	ggshader "github.com/ironpark/ggfx/internal/shader"
)

// The uniform buffer binding points the shader's uniform blocks are bound at.
const (
	internalUniformBlockBinding = 0
	userUniformBlockBinding     = 1
)

type Shader struct {
	graphics *Graphics

	program *ggshader.Program
	p       program

	// linked reports whether the link status was checked and the uniform blocks and samplers
	// were set up. That waits for the link, so it is done at the first draw: with
	// KHR_parallel_shader_compile, shaders made one after another compile in parallel until then.
	linked bool

	// vertex is the shared vertex stage attached to p. fs is the fragment stage, and fssrc its
	// source, kept for the error message until linked.
	vertex *vertexShader
	fs     shader
	fssrc  string
	fsinfo glsl.TranslationInfo

	// samplerLocations are the locations of the sampler uniforms for the source textures, -1 for a
	// texture the fragment shader does not read. samplerUnits are the texture units last set to
	// them, -1 for none yet: a program keeps its uniforms, and the units rarely change.
	samplerLocations [graphics.ShaderSrcImageCount]int32
	samplerUnits     [graphics.ShaderSrcImageCount]int32
}

func newShader(graphics *Graphics, program *ggshader.Program) (*Shader, error) {
	s := &Shader{
		graphics: graphics,
		program:  program,
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

// glslOptions returns the options to translate entryPoint of the shader to GLSL.
func glslOptions(version glsl.Version, stage ir.ShaderStage, entryPoint string) glsl.Options {
	o := glsl.DefaultOptions()
	o.LangVersion = version
	o.EntryPoint = &backend.EntryPointSelector{Stage: stage, Name: entryPoint}
	// The projection matrix the driver sets already maps to OpenGL's clip space.
	o.SkipAdjustCoordinateSpace = true
	o.BoundsCheckPolicies.ImageLoad = backend.BoundsCheckUnchecked
	return o
}

// glslTextureName returns the name naga gives the sampler uniform of the source texture i in the
// fragment stage.
func glslTextureName(i int) string {
	b := ggshader.TextureBinding(i)
	return fmt.Sprintf("_group_%d_binding_%d_fs", b.Group, b.Binding)
}

// vertexShader is a vertex shader object shared by every program of a texture count: the prelude
// fixes the vertex stage, so one compiled from ggshader.VertexProgram stands for all of them.
type vertexShader struct {
	shader shader
	src    string
	info   glsl.TranslationInfo
}

// vertexShader returns the shared vertex shader for textureCount, compiling it on the first call.
// The shader objects live as long as the context; openGLState.reset forgets them.
func (g *Graphics) vertexShader(textureCount int) (*vertexShader, error) {
	if vs, ok := g.state.vertexShaders[textureCount]; ok {
		return vs, nil
	}
	vp, err := ggshader.VertexProgram(textureCount)
	if err != nil {
		return nil, err
	}
	src, info, err := glsl.Compile(vp.Module, glslOptions(g.context.glslVersion(), ir.StageVertex, ggshader.VertexEntryPoint))
	if err != nil {
		return nil, fmt.Errorf("opengl: translating the vertex shader to GLSL failed: %w", err)
	}
	sh, err := g.context.newShader(gl.VERTEX_SHADER, src)
	if err != nil {
		return nil, err
	}
	vs := &vertexShader{shader: sh, src: src, info: info}
	if g.state.vertexShaders == nil {
		g.state.vertexShaders = map[int]*vertexShader{}
	}
	g.state.vertexShaders[textureCount] = vs
	return vs, nil
}

func (s *Shader) compile() error {
	vs, err := s.graphics.vertexShader(s.program.TextureCount)
	if err != nil {
		return err
	}

	fssrc, fsinfo, err := glsl.Compile(s.program.Module, glslOptions(s.graphics.context.glslVersion(), ir.StageFragment, ggshader.FragmentEntryPoint))
	if err != nil {
		return fmt.Errorf("opengl: translating the fragment shader to GLSL failed: %w", err)
	}

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
	s.fsinfo = fsinfo
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
	// emitted, so bind whatever naga reports. The vertex stage's blocks are the shared vertex
	// shader's.
	for _, info := range []glsl.TranslationInfo{s.vertex.info, s.fsinfo} {
		for _, u := range info.Uniforms {
			idx := ctx.GetUniformBlockIndex(uint32(p), u.BlockName)
			if idx == gl.INVALID_INDEX {
				continue
			}
			switch u.Binding {
			case ggshader.InternalUniformBinding:
				ctx.UniformBlockBinding(uint32(p), idx, internalUniformBlockBinding)
			case ggshader.UserUniformBinding:
				ctx.UniformBlockBinding(uint32(p), idx, userUniformBlockBinding)
			}
		}
	}

	for i := range s.samplerLocations {
		s.samplerLocations[i] = -1
		s.samplerUnits[i] = -1
		if i < s.program.TextureCount {
			s.samplerLocations[i] = ctx.GetUniformLocation(uint32(p), glslTextureName(i))
		}
	}

	s.linked = true
	s.fssrc = ""
	s.fsinfo = glsl.TranslationInfo{}
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
