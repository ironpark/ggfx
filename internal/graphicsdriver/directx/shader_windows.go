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

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

const (
	VertexShaderProfile = "vs_5_0"
	PixelShaderProfile  = "ps_5_0"
)

const internalConstantBufferSize = graphics.PreservedUniformDwordCount * 4

var hlslShaderTarget = graphicsdriver.ShaderTarget{
	Language: graphicsdriver.ShaderLanguageHLSL,
	Version:  50,
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

// vertexShaderBlob returns the compiled vertex shader every program with desc.TextureCount source
// textures shares. The prelude fixes the vertex stage, so it is compiled once per texture count.
// The blob is device independent, lives as long as the process, and must not be released.
func vertexShaderBlob(desc *graphicsdriver.ShaderDescriptor) (*_ID3DBlob, error) {
	if desc == nil || desc.Source == nil {
		return nil, fmt.Errorf("directx: shader descriptor has no source")
	}
	vertexShaderBlobsM.Lock()
	defer vertexShaderBlobsM.Unlock()

	if b, ok := vertexShaderBlobs[desc.TextureCount]; ok {
		return b, nil
	}
	code, err := desc.Source.VertexStage(hlslShaderTarget)
	if err != nil {
		return nil, fmt.Errorf("directx: translating the vertex shader to HLSL failed: %w", err)
	}
	b, err := _D3DCompile(code.Code, "shader", nil, nil, code.EntryPoint, VertexShaderProfile, shaderCompileFlags, 0)
	if err != nil {
		return nil, fmt.Errorf("directx: D3DCompile for the vertex shader failed, original source: %s, %w", code.Code, err)
	}
	vertexShaderBlobs[desc.TextureCount] = b
	return b, nil
}

// compileShader returns the program's pixel shader. The vertex shader is shared (see
// vertexShaderBlob), and D3D links the stages by signature: the pixel shader's input comes from the
// prelude's GgfxVertexOut whatever the user's code reads, so it matches the shared vertex shader's
// output.
func compileShader(desc *graphicsdriver.ShaderDescriptor) (*_ID3DBlob, error) {
	if desc == nil || desc.Source == nil {
		return nil, fmt.Errorf("directx: shader descriptor has no source")
	}
	code, err := desc.Source.FragmentStage(hlslShaderTarget)
	if err != nil {
		return nil, fmt.Errorf("directx: translating the shader to HLSL failed: %w", err)
	}
	psh, err := _D3DCompile(code.Code, "shader", nil, nil, code.EntryPoint, PixelShaderProfile, shaderCompileFlags, 0)
	if err != nil {
		return nil, fmt.Errorf("directx: D3DCompile for the pixel shader failed, original source: %s, %w", code.Code, err)
	}
	return psh, nil
}

// userConstantBufferSize returns the size of the constant buffer for the user's uniform block in
// bytes, which is a multiple of 16, or 0 when the shader has no user uniform block.
func userConstantBufferSize(desc *graphicsdriver.ShaderDescriptor) uint32 {
	return alignUp16(uint32(desc.UniformDwordCount) * 4)
}
