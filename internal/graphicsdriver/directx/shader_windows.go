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

	"github.com/gogpu/naga/hlsl"
	"golang.org/x/sync/errgroup"

	"github.com/ironpark/ggfx/internal/graphics"
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
func hlslOptions(program *shader.Program) *hlsl.Options {
	o := hlsl.DefaultOptions()
	o.ShaderModel = hlsl.ShaderModel5_0
	o.BindingMap = map[hlsl.ResourceBinding]hlsl.BindTarget{
		{Group: shader.InternalUniformBinding.Group, Binding: shader.InternalUniformBinding.Binding}: {Register: internalConstantBufferRegister},
		{Group: shader.UserUniformBinding.Group, Binding: shader.UserUniformBinding.Binding}:         {Register: userConstantBufferRegister},
	}
	for i := range program.TextureCount {
		b := shader.TextureBinding(i)
		o.BindingMap[hlsl.ResourceBinding{Group: b.Group, Binding: b.Binding}] = hlsl.BindTarget{Register: uint32(i)}
	}
	return o
}

var vertexShaderCache = map[string]*_ID3DBlob{}

func compileShader(program *shader.Program) (_, _ *_ID3DBlob, ferr error) {
	var vsh, psh *_ID3DBlob
	defer func() {
		if ferr == nil {
			return
		}
		if vsh != nil {
			vsh.Release()
		}
		if psh != nil {
			psh.Release()
		}
	}()

	src, info, err := hlsl.Compile(program.Module, hlslOptions(program))
	if err != nil {
		return nil, nil, fmt.Errorf("directx: translating the shader to HLSL failed: %w", err)
	}
	vsEntry := info.EntryPointNames[shader.VertexEntryPoint]
	psEntry := info.EntryPointNames[shader.FragmentEntryPoint]

	var flag uint32 = uint32(_D3DCOMPILE_OPTIMIZATION_LEVEL3)

	var wg errgroup.Group

	// Vertex shaders are likely the same. If so, reuse the same _ID3DBlob.
	if v, ok := vertexShaderCache[src]; ok {
		// Increment the reference count not to release this object unexpectedly.
		// The value will be removed when the count reached 0.
		// See (*Shader).disposeImpl.
		v.AddRef()
		vsh = v
	} else {
		defer func() {
			if ferr == nil {
				vertexShaderCache[src] = vsh
			}
		}()
		wg.Go(func() error {
			v, err := _D3DCompile([]byte(src), "shader", nil, nil, vsEntry, VertexShaderProfile, flag, 0)
			if err != nil {
				return fmt.Errorf("directx: D3DCompile for the vertex shader failed, original source: %s, %w", src, err)
			}
			vsh = v
			return nil
		})
	}
	wg.Go(func() error {
		p, err := _D3DCompile([]byte(src), "shader", nil, nil, psEntry, PixelShaderProfile, flag, 0)
		if err != nil {
			return fmt.Errorf("directx: D3DCompile for the pixel shader failed, original source: %s, %w", src, err)
		}
		psh = p
		return nil
	})

	if err := wg.Wait(); err != nil {
		return nil, nil, err
	}

	return vsh, psh, nil
}

// userConstantBufferSize returns the size of the constant buffer for the user's uniform block in
// bytes, which is a multiple of 16, or 0 when the shader has no user uniform block.
func userConstantBufferSize(program *shader.Program) uint32 {
	return alignUp16(uint32(program.UniformDwordCount) * 4)
}
