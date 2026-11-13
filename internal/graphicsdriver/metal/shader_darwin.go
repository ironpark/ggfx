// Copyright 2019 The Ebiten Authors
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

package metal

import (
	"fmt"

	"github.com/gogpu/naga/backend"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/mtl"
	"github.com/ironpark/ggfx/internal/shader"
)

// The buffer indices the shader's uniform blocks are bound at. Index 0 is the vertex buffer.
const (
	internalUniformBufferIndex = 1
	userUniformBufferIndex     = 2
)

type shaderRpsKey struct {
	blend graphicsdriver.Blend
	pix   mtl.PixelFormat
}

type Shader struct {
	graphics *Graphics

	program *shader.Program
	lib     mtl.Library
	fs      mtl.Function
	// vs is shared (see Graphics.vertexFunction) and must not be released.
	vs   mtl.Function
	rpss map[shaderRpsKey]mtl.RenderPipelineState
}

func newShader(graphics *Graphics, device mtl.Device, program *shader.Program) (*Shader, error) {
	s := &Shader{
		graphics: graphics,
		program:  program,
		rpss:     map[shaderRpsKey]mtl.RenderPipelineState{},
	}
	if err := s.init(device); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Shader) Dispose() {
	for _, rps := range s.rpss {
		rps.Release()
	}
	s.fs.Release()
	s.lib.Release()
}

// mslOptions maps the shader's resource bindings to Metal buffer and texture indices, and selects
// the one entry point to write.
func mslOptions(program *shader.Program, stage ir.ShaderStage, entryPoint string) msl.Options {
	index := func(i int) *uint8 {
		n := uint8(i)
		return &n
	}
	resources := map[ir.ResourceBinding]msl.BindTarget{
		shader.InternalUniformBinding: {Buffer: index(internalUniformBufferIndex)},
		shader.UserUniformBinding:     {Buffer: index(userUniformBufferIndex)},
	}
	for i := range program.TextureCount {
		resources[shader.TextureBinding(i)] = msl.BindTarget{Texture: index(i)}
	}

	o := msl.DefaultOptions()
	o.BoundsCheckPolicies = backend.BoundsCheckPolicies{
		Index:        backend.BoundsCheckReadZeroSkipWrite,
		Buffer:       backend.BoundsCheckReadZeroSkipWrite,
		ImageLoad:    backend.BoundsCheckUnchecked,
		BindingArray: backend.BoundsCheckReadZeroSkipWrite,
	}
	o.PerEntryPointMap = map[string]msl.EntryPointResources{
		entryPoint: {Resources: resources},
	}
	o.EntryPoint = &backend.EntryPointSelector{Stage: stage, Name: entryPoint}
	return o
}

// vertexStage is a vertex function shared by every shader of a texture count: the prelude fixes the
// vertex stage, so one built from shader.VertexProgram stands for all of them.
type vertexStage struct {
	lib mtl.Library
	fn  mtl.Function
}

// vertexFunction returns the shared vertex function for textureCount, building it on the first
// call. It lives as long as g.
func (g *Graphics) vertexFunction(textureCount int) (mtl.Function, error) {
	if vs, ok := g.vertexStages[textureCount]; ok {
		return vs.fn, nil
	}
	vp, err := shader.VertexProgram(textureCount)
	if err != nil {
		return mtl.Function{}, err
	}
	src, info, err := msl.Compile(vp.Module, mslOptions(vp, ir.StageVertex, shader.VertexEntryPoint))
	if err != nil {
		return mtl.Function{}, fmt.Errorf("metal: translating the vertex shader to MSL failed: %w", err)
	}
	lib, err := g.device.NewLibraryWithSource(src, mtl.CompileOptions{})
	if err != nil {
		return mtl.Function{}, fmt.Errorf("metal: device.MakeLibrary for vertex failed: %w, source: %s", err, src)
	}
	fn, err := lib.NewFunctionWithName(info.EntryPointNames[shader.VertexEntryPoint])
	if err != nil {
		lib.Release()
		return mtl.Function{}, fmt.Errorf("metal: lib.MakeFunction for vertex failed: %w, source: %s", err, src)
	}
	if g.vertexStages == nil {
		g.vertexStages = map[int]vertexStage{}
	}
	g.vertexStages[textureCount] = vertexStage{lib: lib, fn: fn}
	return fn, nil
}

func (s *Shader) init(device mtl.Device) (err error) {
	vs, err := s.graphics.vertexFunction(s.program.TextureCount)
	if err != nil {
		return err
	}

	src, info, err := msl.Compile(s.program.Module, mslOptions(s.program, ir.StageFragment, shader.FragmentEntryPoint))
	if err != nil {
		return fmt.Errorf("metal: translating the fragment shader to MSL failed: %w", err)
	}
	lib, err := device.NewLibraryWithSource(src, mtl.CompileOptions{})
	if err != nil {
		return fmt.Errorf("metal: device.MakeLibrary failed: %w, source: %s", err, src)
	}
	defer func() {
		if err != nil {
			lib.Release()
		}
	}()

	fs, err := lib.NewFunctionWithName(info.EntryPointNames[shader.FragmentEntryPoint])
	if err != nil {
		return fmt.Errorf("metal: lib.MakeFunction for fragment failed: %w, source: %s", err, src)
	}

	s.lib = lib
	s.fs = fs
	s.vs = vs
	return nil
}

// vertexDescriptor describes ggfx's fixed vertex layout (see graphicsdriver.VertexAttributeSizes),
// all float32, in vertex buffer 0.
var vertexDescriptor = newVertexDescriptor()

func newVertexDescriptor() *mtl.VertexDescriptor {
	d := &mtl.VertexDescriptor{
		Layouts: []mtl.VertexBufferLayoutDescriptor{
			{Stride: graphics.VertexFloatCount * 4},
		},
	}
	var offset int
	for _, n := range graphicsdriver.VertexAttributeSizes {
		var format mtl.VertexFormat
		switch n {
		case 2:
			format = mtl.VertexFormatFloat2
		case 4:
			format = mtl.VertexFormatFloat4
		default:
			panic(fmt.Sprintf("metal: unexpected vertex attribute size %d", n))
		}
		d.Attributes = append(d.Attributes, mtl.VertexAttributeDescriptor{Format: format, Offset: offset, BufferIndex: 0})
		offset += n * 4
	}
	return d
}

func (s *Shader) RenderPipelineState(pix mtl.PixelFormat, blend graphicsdriver.Blend) (mtl.RenderPipelineState, error) {
	key := shaderRpsKey{
		blend: blend,
		pix:   pix,
	}
	if rps, ok := s.rpss[key]; ok {
		return rps, nil
	}

	rpld := mtl.RenderPipelineDescriptor{
		VertexFunction:   s.vs,
		VertexDescriptor: vertexDescriptor,
		FragmentFunction: s.fs,
	}

	rpld.ColorAttachments[0].PixelFormat = pix
	rpld.ColorAttachments[0].BlendingEnabled = true

	rpld.ColorAttachments[0].DestinationAlphaBlendFactor = blendFactorToMetalBlendFactor(blend.BlendFactorDestinationAlpha)
	rpld.ColorAttachments[0].DestinationRGBBlendFactor = blendFactorToMetalBlendFactor(blend.BlendFactorDestinationRGB)
	rpld.ColorAttachments[0].SourceAlphaBlendFactor = blendFactorToMetalBlendFactor(blend.BlendFactorSourceAlpha)
	rpld.ColorAttachments[0].SourceRGBBlendFactor = blendFactorToMetalBlendFactor(blend.BlendFactorSourceRGB)
	rpld.ColorAttachments[0].AlphaBlendOperation = blendOperationToMetalBlendOperation(blend.BlendOperationAlpha)
	rpld.ColorAttachments[0].RGBBlendOperation = blendOperationToMetalBlendOperation(blend.BlendOperationRGB)
	rpld.ColorAttachments[0].WriteMask = mtl.ColorWriteMaskAll

	rps, err := s.graphics.device.NewRenderPipelineStateWithDescriptor(rpld)
	if err != nil {
		return mtl.RenderPipelineState{}, fmt.Errorf("metal: device.NewRenderPipelineStateWithDescriptor failed: %w", err)
	}

	s.rpss[key] = rps
	return rps, nil
}
