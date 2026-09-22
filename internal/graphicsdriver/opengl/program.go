// Copyright 2014 Hajime Hoshi
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
	"fmt"
	"math"
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl/gl"
)

const floatSizeInBytes = 4

// arrayBufferLayoutPart is a part of an array buffer layout.
type arrayBufferLayoutPart struct {
	// TODO: This struct should belong to a program and know it.
	name string
	num  int
}

// arrayBufferLayout is an array buffer layout.
//
// An array buffer in OpenGL is a buffer representing vertices and
// is passed to a vertex shader.
type arrayBufferLayout struct {
	parts []arrayBufferLayoutPart
	total int
}

func (a *arrayBufferLayout) names() []string {
	ns := make([]string, len(a.parts))
	for i, p := range a.parts {
		ns[i] = p.name
	}
	return ns
}

// float32Count returns the total float32 count for one element of the array buffer.
func (a *arrayBufferLayout) float32Count() int {
	if a.total != 0 {
		return a.total
	}
	var t int
	for _, p := range a.parts {
		t += p.num
	}
	a.total = t
	return a.total
}

func (a *arrayBufferLayout) addPart(part arrayBufferLayoutPart) {
	a.parts = append(a.parts, part)
	a.total = 0
}

// enable starts using the array buffer.
func (a *arrayBufferLayout) enable(context *context) {
	for i := range a.parts {
		context.ctx.EnableVertexAttribArray(uint32(i))
	}
	total := a.float32Count()
	var offset int
	for i, p := range a.parts {
		context.ctx.VertexAttribPointer(uint32(i), int32(p.num), gl.FLOAT, false, int32(floatSizeInBytes*total), offset)
		offset += floatSizeInBytes * p.num
	}
}

// disable stops using the array buffer.
func (a *arrayBufferLayout) disable(context *context) {
	// TODO: Disabling should be done in reversed order?
	for i := range a.parts {
		context.ctx.DisableVertexAttribArray(uint32(i))
	}
}

// theArrayBufferLayout is the array buffer layout for Ebitengine.
var theArrayBufferLayout arrayBufferLayout

func init() {
	theArrayBufferLayout = arrayBufferLayout{
		// Note that GL_MAX_VERTEX_ATTRIBS is at least 16.
		parts: []arrayBufferLayoutPart{
			{
				name: "A0",
				num:  2,
			},
			{
				name: "A1",
				num:  2,
			},
			{
				name: "A2",
				num:  4,
			},
		},
	}
	n := theArrayBufferLayout.float32Count()
	diff := graphics.VertexFloatCount - n
	if diff == 0 {
		return
	}
	if diff%4 != 0 {
		panic("opengl: unexpected attribute layout")
	}
	for i := range diff / 4 {
		theArrayBufferLayout.addPart(arrayBufferLayoutPart{
			name: fmt.Sprintf("A%d", i+3),
			num:  4,
		})
	}
}

type openGLState struct {
	vertexArray uint32

	// arrayBuffer is OpenGL's array buffer (vertices data).
	arrayBuffer buffer

	arrayBufferSizeInBytes int

	// elementArrayBuffer is OpenGL's element array buffer (indices data).
	elementArrayBuffer buffer

	elementArrayBufferSizeInBytes int

	// uniformBuffers hold the internal and the user's uniform blocks, bound at the binding points
	// internalUniformBlockBinding and userUniformBlockBinding.
	uniformBuffers            [2]buffer
	uniformBufferSizesInBytes [2]int
	lastUniforms              [2][]uint32

	lastProgram       program
	lastActiveTexture int
}

// reset resets or initializes the OpenGL state.
func (s *openGLState) reset(context *context) error {
	if err := context.reset(); err != nil {
		return err
	}

	s.lastProgram = 0
	context.ctx.UseProgram(0)
	s.resetLastUniforms()

	if s.arrayBuffer != 0 {
		context.ctx.DeleteBuffer(uint32(s.arrayBuffer))
	}
	if s.elementArrayBuffer != 0 {
		context.ctx.DeleteBuffer(uint32(s.elementArrayBuffer))
	}
	if s.vertexArray != 0 {
		context.ctx.DeleteVertexArray(s.vertexArray)
	}
	for i, b := range s.uniformBuffers {
		if b != 0 {
			context.ctx.DeleteBuffer(uint32(b))
		}
		s.uniformBuffers[i] = 0
		s.uniformBufferSizesInBytes[i] = 0
	}

	s.arrayBuffer = 0
	s.arrayBufferSizeInBytes = 0
	s.elementArrayBuffer = 0
	s.elementArrayBufferSizeInBytes = 0
	s.vertexArray = 0

	return nil
}

func pow2(x int) int {
	if x > (math.MaxInt+1)/2 {
		return math.MaxInt
	}

	p2 := 1
	for p2 < x {
		p2 *= 2
	}
	return p2
}

func (s *openGLState) setVertices(context *context, vertices []float32, indices []uint32) {
	if s.vertexArray == 0 {
		s.vertexArray = context.ctx.CreateVertexArray()
	}
	context.ctx.BindVertexArray(s.vertexArray)

	if size := len(vertices) * int(unsafe.Sizeof(vertices[0])); s.arrayBufferSizeInBytes < size {
		if s.arrayBuffer != 0 {
			context.ctx.DeleteBuffer(uint32(s.arrayBuffer))
		}

		newSize := pow2(size)
		// newArrayBuffer calls BindBuffer.
		s.arrayBuffer = context.newArrayBuffer(newSize)
		s.arrayBufferSizeInBytes = newSize

		// Reenable the array buffer layout explicitly after resetting the array buffer.
		theArrayBufferLayout.enable(context)
	}

	if size := len(indices) * int(unsafe.Sizeof(indices[0])); s.elementArrayBufferSizeInBytes < size {
		if s.elementArrayBuffer != 0 {
			context.ctx.DeleteBuffer(uint32(s.elementArrayBuffer))
		}

		newSize := pow2(size)
		// newElementArrayBuffer calls BindBuffer.
		s.elementArrayBuffer = context.newElementArrayBuffer(newSize)
		s.elementArrayBufferSizeInBytes = newSize
	}

	// Note that the vertices and the indices passed to BufferSubData is not under GC management in the gl package.
	vs := unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), len(vertices)*int(unsafe.Sizeof(vertices[0])))
	context.ctx.BufferSubData(gl.ARRAY_BUFFER, 0, vs)
	is := unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*int(unsafe.Sizeof(indices[0])))
	context.ctx.BufferSubData(gl.ELEMENT_ARRAY_BUFFER, 0, is)
}

func (s *openGLState) resetLastUniforms() {
	for i := range s.lastUniforms {
		s.lastUniforms[i] = s.lastUniforms[i][:0]
	}
}

// setUniforms uploads the uniform blocks: the internal block, then the user's block, both laid
// out as the shader expects. A block that has not changed since the last draw is not uploaded.
func (s *openGLState) setUniforms(context *context, uniforms []uint32) {
	blocks := [2][]uint32{uniforms[:graphics.PreservedUniformDwordCount], uniforms[graphics.PreservedUniformDwordCount:]}
	bindings := [2]uint32{internalUniformBlockBinding, userUniformBlockBinding}
	for i, block := range blocks {
		if len(block) == 0 {
			continue
		}
		size := len(block) * int(unsafe.Sizeof(block[0]))
		if s.uniformBufferSizesInBytes[i] < size {
			if s.uniformBuffers[i] != 0 {
				context.ctx.DeleteBuffer(uint32(s.uniformBuffers[i]))
			}
			newSize := pow2(size)
			s.uniformBuffers[i] = context.newUniformBuffer(newSize)
			s.uniformBufferSizesInBytes[i] = newSize
			s.lastUniforms[i] = s.lastUniforms[i][:0]
		}
		context.ctx.BindBufferBase(gl.UNIFORM_BUFFER, bindings[i], uint32(s.uniformBuffers[i]))
		if areSameUint32Array(s.lastUniforms[i], block) {
			continue
		}
		bs := unsafe.Slice((*byte)(unsafe.Pointer(&block[0])), size)
		context.ctx.BufferSubData(gl.UNIFORM_BUFFER, 0, bs)
		s.lastUniforms[i] = append(s.lastUniforms[i][:0], block...)
	}
}

// areSameUint32Array returns a boolean indicating if a and b are deeply equal.
func areSameUint32Array(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type textureVariable struct {
	valid  bool
	native textureNative
}

func (g *Graphics) deleteProgram(p program) {
	// A name of a deleted program can be reused for a new program.
	// Reset lastProgram so that useProgram doesn't skip the state updates for such a new program.
	if g.state.lastProgram == p {
		g.state.lastProgram = 0
	}
	g.context.deleteProgram(p)
}

// useProgram uses the program (programTexture).
func (g *Graphics) useProgram(s *Shader, uniforms []uint32, textures [graphics.ShaderSrcImageCount]textureVariable) error {
	program := s.p
	if g.state.lastProgram != program {
		g.context.ctx.UseProgram(uint32(program))

		g.state.lastProgram = program
		g.state.lastActiveTexture = 0
		g.context.ctx.ActiveTexture(gl.TEXTURE0)
		g.context.lastTexture = 0 // Make sure next bindTexture call actually does something.
	}

	g.state.setUniforms(&g.context, uniforms)

	var idx int
loop:
	for i, t := range textures {
		if !t.valid {
			continue
		}

		// If the texture is already bound, set the texture variable to point to the texture.
		// Rebinding the same texture seems problematic (#1193).
		name := s.textureNames[i]
		if name == "" {
			continue
		}

		for _, at := range g.activatedTextures {
			if t.native == at.textureNative {
				g.context.uniformInt(program, name, at.index)
				continue loop
			}
		}

		g.activatedTextures = append(g.activatedTextures, activatedTexture{
			textureNative: t.native,
			index:         idx,
		})
		g.context.uniformInt(program, name, idx)
		if g.state.lastActiveTexture != idx {
			g.context.ctx.ActiveTexture(uint32(gl.TEXTURE0 + idx))
			g.state.lastActiveTexture = idx
			g.context.lastTexture = 0 // Make sure next bindTexture call actually does something.
		}

		// Apparently, a texture must be bound every time. The cache is not used here.
		g.context.bindTexture(t.native)

		idx++
	}

	for i := range g.activatedTextures {
		g.activatedTextures[i] = activatedTexture{}
	}
	g.activatedTextures = g.activatedTextures[:0]

	return nil
}
