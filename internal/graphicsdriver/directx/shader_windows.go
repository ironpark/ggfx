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

package directx

import (
	"fmt"
	"sync"

	"github.com/gogpu/naga/backend"
	"github.com/gogpu/naga/hlsl"
	"github.com/gogpu/naga/ir"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/shader"
)

const (
	VertexShaderProfile = "vs_5_0"
	PixelShaderProfile  = "ps_5_0"
)

// The constant buffer registers the shader's uniform blocks are bound at, and the size of the
// internal block's constant buffer in bytes.
const (
	internalConstantBufferRegister = 0
	userConstantBufferRegister     = 1

	internalConstantBufferSize = graphics.PreservedUniformDwordCount * 4
)

// hlslOptions maps the shader's resource bindings to HLSL registers.
func hlslOptions(program *shader.Program) hlsl.Options {
	o := hlsl.DefaultOptions()
	o.ShaderModel = backend.ShaderModel{Major: 5, Minor: 0}
	o.BindingMap = map[ir.ResourceBinding]hlsl.BindTarget{
		shader.InternalUniformBinding: {Register: internalConstantBufferRegister},
		shader.UserUniformBinding:     {Register: userConstantBufferRegister},
	}
	for i := range program.TextureCount {
		o.BindingMap[shader.TextureBinding(i)] = hlsl.BindTarget{Register: uint32(i)}
	}
	// A resource the map leaves out goes where its @group and @binding say, as before naga 0.21.
	o.FakeMissingBindings = true
	return o
}

// vertexAttributeSemanticName is the semantic of every vertex attribute: naga's HLSL output names
// attribute i LOC with semantic index i.
var vertexAttributeSemanticName = &([]byte("LOC\000"))[0]

// vertexAttributeFormats returns the formats of the vertex attributes in order (see
// graphicsdriver.VertexAttributeSizes).
func vertexAttributeFormats() []_DXGI_FORMAT {
	var formats []_DXGI_FORMAT
	for _, n := range graphicsdriver.VertexAttributeSizes {
		switch n {
		case 2:
			formats = append(formats, _DXGI_FORMAT_R32G32_FLOAT)
		case 4:
			formats = append(formats, _DXGI_FORMAT_R32G32B32A32_FLOAT)
		default:
			panic(fmt.Sprintf("directx: unexpected vertex attribute size %d", n))
		}
	}
	return formats
}

const shaderCompileFlags = uint32(_D3DCOMPILE_OPTIMIZATION_LEVEL3)

var (
	vertexShaderBlobsM sync.Mutex
	vertexShaderBlobs  = map[int]*_ID3DBlob{}
)

// vertexShaderBlob returns the compiled vertex shader every program with textureCount source
// textures shares. The prelude fixes the vertex stage, so it is compiled once from
// shader.VertexProgram rather than from each program's HLSL, which holds the user's fragment code.
// The blob is device independent, lives as long as the process, and must not be released.
func vertexShaderBlob(textureCount int) (*_ID3DBlob, error) {
	vertexShaderBlobsM.Lock()
	defer vertexShaderBlobsM.Unlock()

	if b, ok := vertexShaderBlobs[textureCount]; ok {
		return b, nil
	}
	vp, err := shader.VertexProgram(textureCount)
	if err != nil {
		return nil, err
	}
	o := hlslOptions(vp)
	o.EntryPoint = &backend.EntryPointSelector{Stage: ir.StageVertex, Name: shader.VertexEntryPoint}
	src, info, err := hlsl.Compile(vp.Module, o)
	if err != nil {
		return nil, fmt.Errorf("directx: translating the vertex shader to HLSL failed: %w", err)
	}
	b, err := _D3DCompile([]byte(src), "shader", nil, nil, info.EntryPointNames[shader.VertexEntryPoint], VertexShaderProfile, shaderCompileFlags, 0)
	if err != nil {
		return nil, fmt.Errorf("directx: D3DCompile for the vertex shader failed, original source: %s, %w", src, err)
	}
	vertexShaderBlobs[textureCount] = b
	return b, nil
}

// compileShader returns the program's pixel shader. The vertex shader is shared (see
// vertexShaderBlob), and D3D links the stages by signature: the pixel shader's input comes from the
// prelude's GgfxVertexOut whatever the user's code reads, so it matches the shared vertex shader's
// output.
func compileShader(program *shader.Program) (*_ID3DBlob, error) {
	// Only the fragment entry point: the program's vertex entry point would be parsed and discarded.
	o := hlslOptions(program)
	o.EntryPoint = &backend.EntryPointSelector{Stage: ir.StageFragment, Name: shader.FragmentEntryPoint}
	src, info, err := hlsl.Compile(program.Module, o)
	if err != nil {
		return nil, fmt.Errorf("directx: translating the shader to HLSL failed: %w", err)
	}
	psh, err := _D3DCompile([]byte(src), "shader", nil, nil, info.EntryPointNames[shader.FragmentEntryPoint], PixelShaderProfile, shaderCompileFlags, 0)
	if err != nil {
		return nil, fmt.Errorf("directx: D3DCompile for the pixel shader failed, original source: %s, %w", src, err)
	}
	return psh, nil
}

// userConstantBufferSize returns the size of the constant buffer for the user's uniform block in
// bytes, which is a multiple of 16, or 0 when the shader has no user uniform block.
func userConstantBufferSize(program *shader.Program) uint32 {
	return alignUp16(uint32(program.UniformDwordCount) * 4)
}
