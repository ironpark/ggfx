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
	"runtime"
	"slices"
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl/gl"
	"github.com/ironpark/ggfx/internal/recent"
)

const floatSizeInBytes = 4

// enableVertexAttributes starts using the array buffer, laid out as
// graphicsdriver.VertexAttributeSizes says: attribute i at location i, as the shaders'
// layout(location) qualifiers say. GL_MAX_VERTEX_ATTRIBS is at least 16.
func enableVertexAttributes(context *context) {
	const stride = floatSizeInBytes * graphics.VertexFloatCount
	for i := range graphicsdriver.VertexAttributeSizes {
		context.ctx.EnableVertexAttribArray(uint32(i))
	}
	var offset int
	for i, n := range graphicsdriver.VertexAttributeSizes {
		context.ctx.VertexAttribPointer(uint32(i), int32(n), gl.FLOAT, false, stride, offset)
		offset += floatSizeInBytes * n
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

	// arrayBufferUse and elementArrayBufferUse track the bytes the batches of recent frames sent,
	// so that the buffers give back the room a large batch took.
	arrayBufferUse        recent.Peak
	elementArrayBufferUse recent.Peak

	// uniformBuffers hold the internal and the user's uniform blocks, bound at the binding points
	// internalUniformBlockBinding and userUniformBlockBinding.
	uniformBuffers            [2]buffer
	uniformBufferSizesInBytes [2]int
	lastUniforms              [2][]uint32

	// boundUniformBuffers are the buffers bound at the uniform blocks' binding points, and
	// boundUniformBuffer the one bound at the generic GL_UNIFORM_BUFFER target, which
	// BufferSubData writes. Zero is unknown.
	boundUniformBuffers [2]buffer
	boundUniformBuffer  buffer

	// vertexShaders are the shared vertex shaders by texture count.
	vertexShaders map[int]*vertexShader

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

	// reset runs at the initialization, when there are no vertex shaders, and after a context loss,
	// when their names are stale and must not be deleted.
	s.vertexShaders = nil

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

// orphanVertexBuffers reports whether the vertex and the index buffer are orphaned, given new
// storage of the same size, before they are written. Writing a buffer the GPU may still read from
// a previous draw otherwise waits for that draw. WebGL has no such wait to avoid, and would clear
// the new storage.
const orphanVertexBuffers = runtime.GOOS != "js"

func (s *openGLState) setVertices(context *context, vertices []float32, indices []uint32) {
	if s.vertexArray == 0 {
		s.vertexArray = context.ctx.CreateVertexArray()
	}
	context.ctx.BindVertexArray(s.vertexArray)

	size := len(vertices) * int(unsafe.Sizeof(vertices[0]))
	if c := s.arrayBufferUse.Capacity(s.arrayBufferSizeInBytes, size, graphicsdriver.MinVertexBufferBytes); s.arrayBuffer == 0 || c != s.arrayBufferSizeInBytes {
		if s.arrayBuffer != 0 {
			context.ctx.DeleteBuffer(uint32(s.arrayBuffer))
		}

		// newArrayBuffer calls BindBuffer.
		s.arrayBuffer = context.newArrayBuffer(c)
		s.arrayBufferSizeInBytes = c

		// Reenable the array buffer layout explicitly after resetting the array buffer.
		enableVertexAttributes(context)
	} else if orphanVertexBuffers {
		context.ctx.BufferInit(gl.ARRAY_BUFFER, s.arrayBufferSizeInBytes, gl.DYNAMIC_DRAW)
	}

	size = len(indices) * int(unsafe.Sizeof(indices[0]))
	if c := s.elementArrayBufferUse.Capacity(s.elementArrayBufferSizeInBytes, size, graphicsdriver.MinVertexBufferBytes); s.elementArrayBuffer == 0 || c != s.elementArrayBufferSizeInBytes {
		if s.elementArrayBuffer != 0 {
			context.ctx.DeleteBuffer(uint32(s.elementArrayBuffer))
		}

		// newElementArrayBuffer calls BindBuffer.
		s.elementArrayBuffer = context.newElementArrayBuffer(c)
		s.elementArrayBufferSizeInBytes = c
	} else if orphanVertexBuffers {
		context.ctx.BufferInit(gl.ELEMENT_ARRAY_BUFFER, s.elementArrayBufferSizeInBytes, gl.DYNAMIC_DRAW)
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
	s.boundUniformBuffers = [2]buffer{}
	s.boundUniformBuffer = 0
}

// setUniforms uploads the uniform blocks: the internal block, then the user's block, both laid
// out as the shader expects. A block that has not changed since the last draw is not uploaded, nor
// bound again: the binding points are context state, shared by every program.
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
			newSize := recent.NextPowerOf2(size)
			// newUniformBuffer binds the buffer at the generic target. A deleted buffer's name can be
			// reused, so its binding point is bound again.
			s.uniformBuffers[i] = context.newUniformBuffer(newSize)
			s.uniformBufferSizesInBytes[i] = newSize
			s.lastUniforms[i] = s.lastUniforms[i][:0]
			s.boundUniformBuffers[i] = 0
			s.boundUniformBuffer = s.uniformBuffers[i]
		}
		b := s.uniformBuffers[i]
		if s.boundUniformBuffers[i] != b {
			// BindBufferBase binds the generic target too.
			context.ctx.BindBufferBase(gl.UNIFORM_BUFFER, bindings[i], uint32(b))
			s.boundUniformBuffers[i] = b
			s.boundUniformBuffer = b
		}
		if slices.Equal(s.lastUniforms[i], block) {
			continue
		}
		if s.boundUniformBuffer != b {
			context.ctx.BindBuffer(gl.UNIFORM_BUFFER, uint32(b))
			s.boundUniformBuffer = b
		}
		bs := unsafe.Slice((*byte)(unsafe.Pointer(&block[0])), size)
		context.ctx.BufferSubData(gl.UNIFORM_BUFFER, 0, bs)
		s.lastUniforms[i] = append(s.lastUniforms[i][:0], block...)
	}
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
	if err := s.link(); err != nil {
		return err
	}

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
		if s.samplerLocations[i] < 0 {
			continue
		}

		for _, at := range g.activatedTextures {
			if t.native == at.textureNative {
				s.setSamplerUnit(&g.context, i, at.index)
				continue loop
			}
		}

		g.activatedTextures = append(g.activatedTextures, activatedTexture{
			textureNative: t.native,
			index:         idx,
		})
		s.setSamplerUnit(&g.context, i, idx)
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

// setSamplerUnit points the sampler uniform of the source texture i at the texture unit. The
// program must be in use.
func (s *Shader) setSamplerUnit(context *context, i int, unit int) {
	if s.samplerUnits[i] == int32(unit) {
		return
	}
	context.ctx.Uniform1i(s.samplerLocations[i], int32(unit))
	s.samplerUnits[i] = int32(unit)
}
