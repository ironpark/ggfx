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

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/mtl"
)

type shaderRpsKey struct {
	blend graphicsdriver.Blend
	pix   mtl.PixelFormat
}

type Shader struct {
	graphics *Graphics

	desc *graphicsdriver.ShaderDescriptor
	lib  mtl.Library
	fs   mtl.Function
	// vs is shared (see Graphics.vertexFunction) and must not be released.
	vs   mtl.Function
	rpss map[shaderRpsKey]mtl.RenderPipelineState
}

func newShader(graphics *Graphics, device mtl.Device, desc *graphicsdriver.ShaderDescriptor) (*Shader, error) {
	if desc == nil || desc.Source == nil {
		return nil, fmt.Errorf("metal: shader descriptor has no source")
	}
	s := &Shader{
		graphics: graphics,
		desc:     desc,
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

// vertexStage is a vertex function shared by every shader of a texture count: the prelude fixes the
// vertex stage, so one built from ShaderSource.VertexStage stands for all of them.
type vertexStage struct {
	lib mtl.Library
	fn  mtl.Function
}

// vertexFunction returns the shared vertex function for textureCount, building it on the first
// call. It lives as long as g.
func (g *Graphics) vertexFunction(textureCount int, src graphicsdriver.ShaderSource) (mtl.Function, error) {
	if vs, ok := g.vertexStages[textureCount]; ok {
		return vs.fn, nil
	}
	code, err := src.VertexStage(graphicsdriver.ShaderTarget{Language: graphicsdriver.ShaderLanguageMSL})
	if err != nil {
		return mtl.Function{}, fmt.Errorf("metal: translating the vertex shader to MSL failed: %w", err)
	}
	lib, err := g.device.NewLibraryWithSource(string(code.Code), mtl.CompileOptions{})
	if err != nil {
		return mtl.Function{}, fmt.Errorf("metal: device.MakeLibrary for vertex failed: %w, source: %s", err, code.Code)
	}
	fn, err := lib.NewFunctionWithName(code.EntryPoint)
	if err != nil {
		lib.Release()
		return mtl.Function{}, fmt.Errorf("metal: lib.MakeFunction for vertex failed: %w, source: %s", err, code.Code)
	}
	if g.vertexStages == nil {
		g.vertexStages = map[int]vertexStage{}
	}
	g.vertexStages[textureCount] = vertexStage{lib: lib, fn: fn}
	return fn, nil
}

func (s *Shader) init(device mtl.Device) (err error) {
	vs, err := s.graphics.vertexFunction(s.desc.TextureCount, s.desc.Source)
	if err != nil {
		return err
	}

	code, err := s.desc.Source.FragmentStage(graphicsdriver.ShaderTarget{Language: graphicsdriver.ShaderLanguageMSL})
	if err != nil {
		return fmt.Errorf("metal: translating the fragment shader to MSL failed: %w", err)
	}
	lib, err := device.NewLibraryWithSource(string(code.Code), mtl.CompileOptions{})
	if err != nil {
		return fmt.Errorf("metal: device.MakeLibrary failed: %w, source: %s", err, code.Code)
	}
	defer func() {
		if err != nil {
			lib.Release()
		}
	}()

	fs, err := lib.NewFunctionWithName(code.EntryPoint)
	if err != nil {
		return fmt.Errorf("metal: lib.MakeFunction for fragment failed: %w, source: %s", err, code.Code)
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
