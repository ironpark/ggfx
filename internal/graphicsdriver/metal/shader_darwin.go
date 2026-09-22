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
	blend  graphicsdriver.Blend
	screen bool
}

type Shader struct {
	id       graphicsdriver.ShaderID
	graphics *Graphics

	program *shader.Program
	lib     mtl.Library
	fs      mtl.Function
	vs      mtl.Function
	rpss    map[shaderRpsKey]mtl.RenderPipelineState
}

func newShader(id graphicsdriver.ShaderID, graphics *Graphics, device mtl.Device, program *shader.Program) (*Shader, error) {
	s := &Shader{
		id:       id,
		graphics: graphics,
		program:  program,
		rpss:     map[shaderRpsKey]mtl.RenderPipelineState{},
	}
	if err := s.init(device); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Shader) ID() graphicsdriver.ShaderID {
	return s.id
}

func (s *Shader) Dispose() {
	for _, rps := range s.rpss {
		rps.Release()
	}
	s.vs.Release()
	s.fs.Release()
	s.lib.Release()
	s.graphics.removeShader(s)
}

// mslOptions maps the shader's resource bindings to Metal buffer and texture indices.
func mslOptions(program *shader.Program) msl.Options {
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
	o.BoundsCheckPolicies.Image = msl.BoundsCheckUnchecked
	o.PerEntryPointMap = map[string]msl.EntryPointResources{
		shader.VertexEntryPoint:   {Resources: resources},
		shader.FragmentEntryPoint: {Resources: resources},
	}
	return o
}

func (s *Shader) init(device mtl.Device) (err error) {
	src, info, err := msl.Compile(s.program.Module, mslOptions(s.program))
	if err != nil {
		return fmt.Errorf("metal: translating the shader to MSL failed: %w", err)
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

	vs, err := lib.NewFunctionWithName(info.EntryPointNames[shader.VertexEntryPoint])
	if err != nil {
		return fmt.Errorf("metal: lib.MakeFunction for vertex failed: %w, source: %s", err, src)
	}
	defer func() {
		if err != nil {
			vs.Release()
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

// vertexDescriptor describes ggfx's fixed vertex layout: destination position, source position,
// color and custom values, all float32, in vertex buffer 0.
var vertexDescriptor = &mtl.VertexDescriptor{
	Attributes: []mtl.VertexAttributeDescriptor{
		{Format: mtl.VertexFormatFloat2, Offset: 0, BufferIndex: 0},
		{Format: mtl.VertexFormatFloat2, Offset: 2 * 4, BufferIndex: 0},
		{Format: mtl.VertexFormatFloat4, Offset: 4 * 4, BufferIndex: 0},
		{Format: mtl.VertexFormatFloat4, Offset: 8 * 4, BufferIndex: 0},
	},
	Layouts: []mtl.VertexBufferLayoutDescriptor{
		{Stride: graphics.VertexFloatCount * 4},
	},
}

func (s *Shader) RenderPipelineState(view *view, blend graphicsdriver.Blend, screen bool) (mtl.RenderPipelineState, error) {
	key := shaderRpsKey{
		blend:  blend,
		screen: screen,
	}
	if rps, ok := s.rpss[key]; ok {
		return rps, nil
	}

	rpld := mtl.RenderPipelineDescriptor{
		VertexFunction:   s.vs,
		VertexDescriptor: vertexDescriptor,
		FragmentFunction: s.fs,
	}

	pix := mtl.PixelFormatRGBA8UNorm
	if screen {
		pix = view.colorPixelFormat()
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

	rps, err := view.getMTLDevice().NewRenderPipelineStateWithDescriptor(rpld)
	if err != nil {
		return mtl.RenderPipelineState{}, fmt.Errorf("metal: device.NewRenderPipelineStateWithDescriptor failed: %w", err)
	}

	s.rpss[key] = rps
	return rps, nil
}
