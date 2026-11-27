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

//go:build !playstation5

package opengl

import (
	"slices"
	"testing"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl/gl"
)

type glCall struct {
	op   string
	u0   uint32
	i0   int
	i1   int
	size int
}

// recordingGLContext records the GL calls made by openGLState and Graphics.useProgram.
type recordingGLContext struct {
	gl.Context

	nextBuffer uint32
	calls      []glCall
}

func (r *recordingGLContext) LoadFunctions() error { return nil }
func (r *recordingGLContext) IsES() bool           { return false }
func (r *recordingGLContext) GetError() uint32     { return gl.NO_ERROR }

func (r *recordingGLContext) Enable(uint32)                                    {}
func (r *recordingGLContext) BlendFuncSeparate(uint32, uint32, uint32, uint32) {}
func (r *recordingGLContext) BlendEquationSeparate(uint32, uint32)             {}
func (r *recordingGLContext) GetInteger(uint32) int                            { return 0 }

func (r *recordingGLContext) CreateBuffer() uint32 {
	r.nextBuffer++
	r.calls = append(r.calls, glCall{op: "CreateBuffer", u0: r.nextBuffer})
	return r.nextBuffer
}

func (r *recordingGLContext) DeleteBuffer(b uint32) {
	r.calls = append(r.calls, glCall{op: "DeleteBuffer", u0: b})
}

func (r *recordingGLContext) BindBuffer(target uint32, b uint32) {
	r.calls = append(r.calls, glCall{op: "BindBuffer", u0: target, i0: int(b)})
}

func (r *recordingGLContext) BindBufferBase(target uint32, index uint32, b uint32) {
	r.calls = append(r.calls, glCall{op: "BindBufferBase", u0: target, i0: int(index), i1: int(b)})
}

func (r *recordingGLContext) BufferInit(target uint32, size int, usage uint32) {
	r.calls = append(r.calls, glCall{op: "BufferInit", u0: target, size: size, i0: int(usage)})
}

func (r *recordingGLContext) BufferSubData(target uint32, offset int, data []byte) {
	r.calls = append(r.calls, glCall{op: "BufferSubData", u0: target, i0: offset, size: len(data)})
}

func (r *recordingGLContext) UseProgram(p uint32) {
	r.calls = append(r.calls, glCall{op: "UseProgram", u0: p})
}

func (r *recordingGLContext) ActiveTexture(unit uint32) {
	r.calls = append(r.calls, glCall{op: "ActiveTexture", u0: unit})
}

func (r *recordingGLContext) BindTexture(target uint32, tex uint32) {
	r.calls = append(r.calls, glCall{op: "BindTexture", u0: target, i0: int(tex)})
}

func (r *recordingGLContext) Uniform1i(loc int32, v int32) {
	r.calls = append(r.calls, glCall{op: "Uniform1i", i0: int(loc), i1: int(v)})
}

func (r *recordingGLContext) resetCalls() {
	r.calls = r.calls[:0]
}

func TestSetUniformsOrphaningAndCaching(t *testing.T) {
	rec := &recordingGLContext{}
	g := newGraphics(rec, color.ColorSpaceSRGB)
	if err := g.state.reset(&g.context); err != nil {
		t.Fatal(err)
	}
	rec.resetCalls()

	// Internal block + 16-byte user block (4 uint32s).
	u16 := make([]uint32, graphics.PreservedUniformDwordCount+4)
	g.state.setUniforms(&g.context, u16, graphicsdriver.UniformVersions{Internal: 1, User: 10})

	// Initial creation calls BufferInit once per block via newUniformBuffer, then BufferSubData,
	// without a redundant second BufferInit.
	var initCount, subCount int
	for _, c := range rec.calls {
		switch c.op {
		case "BufferInit":
			initCount++
		case "BufferSubData":
			subCount++
		}
	}
	if initCount != 2 || subCount != 2 {
		t.Fatalf("first upload: got BufferInit=%d BufferSubData=%d, want 2, 2 (calls: %v)", initCount, subCount, rec.calls)
	}

	// Same non-zero versions: no uploads at all.
	rec.resetCalls()
	g.state.setUniforms(&g.context, u16, graphicsdriver.UniformVersions{Internal: 1, User: 10})
	if len(rec.calls) != 0 {
		t.Fatalf("unchanged versions: got %v, want no GL calls", rec.calls)
	}

	// Distinct user version: must orphan the existing UBO before BufferSubData on desktop GL.
	rec.resetCalls()
	g.state.setUniforms(&g.context, u16, graphicsdriver.UniformVersions{Internal: 1, User: 11})
	var want []glCall
	if orphanBuffers {
		want = append(want, glCall{op: "BufferInit", u0: gl.UNIFORM_BUFFER, size: 16, i0: gl.DYNAMIC_DRAW})
	}
	want = append(want, glCall{op: "BufferSubData", u0: gl.UNIFORM_BUFFER, i0: 0, size: 16})
	if !slices.Equal(rec.calls, want) {
		t.Fatalf("user rewrite: got %v, want %v", rec.calls, want)
	}

	// Growing the user block to 64 bytes, then writing a 16-byte block, must orphan at the
	// allocated 64-byte buffer size so a subsequent 64-byte write stays within storage bounds.
	u64 := make([]uint32, graphics.PreservedUniformDwordCount+16)
	g.state.setUniforms(&g.context, u64, graphicsdriver.UniformVersions{Internal: 1, User: 12})
	rec.resetCalls()
	g.state.setUniforms(&g.context, u16, graphicsdriver.UniformVersions{Internal: 1, User: 13})
	if orphanBuffers {
		wantSmall := []glCall{
			{op: "BufferInit", u0: gl.UNIFORM_BUFFER, size: 64, i0: gl.DYNAMIC_DRAW},
			{op: "BufferSubData", u0: gl.UNIFORM_BUFFER, i0: 0, size: 16},
		}
		if !slices.Equal(rec.calls, wantSmall) {
			t.Fatalf("smaller rewrite after grow: got %v, want %v", rec.calls, wantSmall)
		}
	}

	// Version 0 (unknown) must still upload instead of matching a zero cached version.
	g.state.resetLastUniforms()
	rec.resetCalls()
	g.state.setUniforms(&g.context, u16, graphicsdriver.UniformVersions{})
	subCount = 0
	for _, c := range rec.calls {
		if c.op == "BufferSubData" {
			subCount++
		}
	}
	if subCount != 2 {
		t.Fatalf("version 0 upload: got BufferSubData=%d, want 2 (calls: %v)", subCount, rec.calls)
	}
}

func makeLinkedShader(g *Graphics, p program, samplerLocs [graphics.ShaderSrcImageCount]int32) *Shader {
	s := &Shader{
		graphics:         g,
		p:                p,
		linked:           true,
		samplerLocations: samplerLocs,
	}
	for i := range s.samplerUnits {
		s.samplerUnits[i] = -1
	}
	return s
}

func TestUseProgramPreservesTextureBindings(t *testing.T) {
	rec := &recordingGLContext{}
	g := newGraphics(rec, color.ColorSpaceSRGB)
	if err := g.state.reset(&g.context); err != nil {
		t.Fatal(err)
	}

	shaderA := makeLinkedShader(g, 1, [graphics.ShaderSrcImageCount]int32{10, -1, -1, -1})
	shaderNoTex := makeLinkedShader(g, 2, [graphics.ShaderSrcImageCount]int32{-1, -1, -1, -1})
	shaderB := makeLinkedShader(g, 3, [graphics.ShaderSrcImageCount]int32{20, -1, -1, -1})
	shaderMulti := makeLinkedShader(g, 4, [graphics.ShaderSrcImageCount]int32{30, 31, -1, -1})

	uniforms := make([]uint32, graphics.PreservedUniformDwordCount)
	versions := graphicsdriver.UniformVersions{Internal: 1}
	tex1 := [graphics.ShaderSrcImageCount]textureVariable{{valid: true, native: 100}}
	texNone := [graphics.ShaderSrcImageCount]textureVariable{}
	tex12 := [graphics.ShaderSrcImageCount]textureVariable{
		{valid: true, native: 100},
		{valid: true, native: 200},
	}

	// First draw with shaderA + tex1 binds tex1 on TEXTURE0 and sets shaderA's sampler uniform.
	if err := g.useProgram(shaderA, uniforms, versions, tex1); err != nil {
		t.Fatal(err)
	}

	// Switching to a textureless shader and back to shaderA with the same texture must not call
	// ActiveTexture or BindTexture again.
	rec.resetCalls()
	if err := g.useProgram(shaderNoTex, uniforms, versions, texNone); err != nil {
		t.Fatal(err)
	}
	if err := g.useProgram(shaderA, uniforms, versions, tex1); err != nil {
		t.Fatal(err)
	}
	want := []glCall{
		{op: "UseProgram", u0: 2},
		{op: "UseProgram", u0: 1},
	}
	if !slices.Equal(rec.calls, want) {
		t.Fatalf("switch through textureless shader: got %v, want %v", rec.calls, want)
	}

	// Switching to shaderB with the same texture must still assign shaderB's sampler uniform,
	// without redundant ActiveTexture or BindTexture calls.
	rec.resetCalls()
	if err := g.useProgram(shaderB, uniforms, versions, tex1); err != nil {
		t.Fatal(err)
	}
	want = []glCall{
		{op: "UseProgram", u0: 3},
		{op: "Uniform1i", i0: 20, i1: 0},
	}
	if !slices.Equal(rec.calls, want) {
		t.Fatalf("switch to shaderB with same texture: got %v, want %v", rec.calls, want)
	}

	// Multi-texture draw activates TEXTURE1 for tex2; switching back to shaderA with tex1 must
	// switch back to TEXTURE0 and rebind tex1.
	if err := g.useProgram(shaderMulti, uniforms, versions, tex12); err != nil {
		t.Fatal(err)
	}
	rec.resetCalls()
	if err := g.useProgram(shaderA, uniforms, versions, tex1); err != nil {
		t.Fatal(err)
	}
	want = []glCall{
		{op: "UseProgram", u0: 1},
		{op: "ActiveTexture", u0: gl.TEXTURE0},
		{op: "BindTexture", u0: gl.TEXTURE_2D, i0: 100},
	}
	if !slices.Equal(rec.calls, want) {
		t.Fatalf("switch back from multi-texture: got %v, want %v", rec.calls, want)
	}
}
