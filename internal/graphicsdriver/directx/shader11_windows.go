// Copyright 2023 The Ebitengine Authors
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
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

type shader11 struct {
	graphics               *graphics11
	id                     graphicsdriver.ShaderID
	userConstantBufferSize uint32
	vertexShaderBlob       *_ID3DBlob
	pixelShaderBlob        *_ID3DBlob

	inputLayout  *_ID3D11InputLayout
	vertexShader *_ID3D11VertexShader
	pixelShader  *_ID3D11PixelShader

	// constantBuffers are the constant buffers for the internal and the user's uniform blocks. The
	// latter is nil when the shader has no user uniform block.
	constantBuffers [2]*_ID3D11Buffer
}

func (s *shader11) ID() graphicsdriver.ShaderID {
	return s.id
}

func (s *shader11) Dispose() {
	s.graphics.removeShader(s)
}

func (s *shader11) disposeImpl() {
	if s.pixelShaderBlob != nil {
		s.pixelShaderBlob.Release()
		s.pixelShaderBlob = nil
	}
	if s.vertexShaderBlob != nil {
		count := s.vertexShaderBlob.Release()
		if count == 0 {
			for k, v := range vertexShaderCache {
				if v == s.vertexShaderBlob {
					delete(vertexShaderCache, k)
				}
			}
		}
		s.vertexShaderBlob = nil
	}
	if s.inputLayout != nil {
		s.inputLayout.Release()
		s.inputLayout = nil
	}
	if s.vertexShader != nil {
		s.vertexShader.Release()
		s.vertexShader = nil
	}
	if s.pixelShader != nil {
		s.pixelShader.Release()
		s.pixelShader = nil
	}
	for i, cb := range s.constantBuffers {
		if cb != nil {
			cb.Release()
			s.constantBuffers[i] = nil
		}
	}
}

func (s *shader11) use(uniforms []uint32, srcs [graphics.ShaderSrcImageCount]*image11) error {
	vs, err := s.ensureVertexShader()
	if err != nil {
		return err
	}
	s.graphics.deviceContext.VSSetShader(vs, nil)

	ps, err := s.ensurePixelShader()
	if err != nil {
		return err
	}
	s.graphics.deviceContext.PSSetShader(ps, nil)

	il, err := s.ensureInputLayout()
	if err != nil {
		return err
	}
	s.graphics.deviceContext.IASetInputLayout(il)

	cbs, err := s.ensureConstantBuffers()
	if err != nil {
		return err
	}
	s.graphics.deviceContext.VSSetConstantBuffers(0, cbs)
	s.graphics.deviceContext.PSSetConstantBuffers(0, cbs)

	// The internal uniform block comes first, then the user's block. Both are already laid out as
	// the shader expects.
	blocks := [][]uint32{uniforms[:graphics.PreservedUniformDwordCount], uniforms[graphics.PreservedUniformDwordCount:]}
	for i, cb := range cbs {
		var mapped _D3D11_MAPPED_SUBRESOURCE
		if err := s.graphics.deviceContext.Map(unsafe.Pointer(cb), 0, _D3D11_MAP_WRITE_DISCARD, 0, &mapped); err != nil {
			return err
		}
		copy(unsafe.Slice((*uint32)(mapped.pData), len(blocks[i])), blocks[i])
		s.graphics.deviceContext.Unmap(unsafe.Pointer(cb), 0)
	}

	var srvs [graphics.ShaderSrcImageCount]*_ID3D11ShaderResourceView
	for i, src := range srcs {
		if src == nil {
			continue
		}
		srv, err := src.getShaderResourceView()
		if err != nil {
			return err
		}
		srvs[i] = srv
	}
	s.graphics.deviceContext.PSSetShaderResources(0, srvs[:])

	return nil
}

func (s *shader11) ensureInputLayout() (*_ID3D11InputLayout, error) {
	if s.inputLayout != nil {
		return s.inputLayout, nil
	}

	i, err := s.graphics.device.CreateInputLayout(inputElementDescsForDX11, s.vertexShaderBlob.GetBufferPointer(), s.vertexShaderBlob.GetBufferSize())
	if err != nil {
		return nil, err
	}
	s.inputLayout = i
	return i, nil
}

func (s *shader11) ensureVertexShader() (*_ID3D11VertexShader, error) {
	if s.vertexShader != nil {
		return s.vertexShader, nil
	}

	vs, err := s.graphics.device.CreateVertexShader(s.vertexShaderBlob.GetBufferPointer(), s.vertexShaderBlob.GetBufferSize(), nil)
	if err != nil {
		return nil, err
	}
	s.vertexShader = vs
	return vs, nil
}

func (s *shader11) ensurePixelShader() (*_ID3D11PixelShader, error) {
	if s.pixelShader != nil {
		return s.pixelShader, nil
	}

	ps, err := s.graphics.device.CreatePixelShader(s.pixelShaderBlob.GetBufferPointer(), s.pixelShaderBlob.GetBufferSize(), nil)
	if err != nil {
		return nil, err
	}
	s.pixelShader = ps
	return ps, nil
}

func alignUp16(x uint32) uint32 {
	if x%16 == 0 {
		return x
	}
	return x + 16 - (x % 16)
}

func (s *shader11) ensureConstantBuffers() ([]*_ID3D11Buffer, error) {
	sizes := []uint32{internalConstantBufferSize, s.userConstantBufferSize}
	for i, size := range sizes {
		if s.constantBuffers[i] != nil || size == 0 {
			continue
		}
		cb, err := s.graphics.device.CreateBuffer(&_D3D11_BUFFER_DESC{
			ByteWidth:      size,
			Usage:          _D3D11_USAGE_DYNAMIC,
			BindFlags:      uint32(_D3D11_BIND_CONSTANT_BUFFER),
			CPUAccessFlags: uint32(_D3D11_CPU_ACCESS_WRITE),
		}, nil)
		if err != nil {
			return nil, err
		}
		s.constantBuffers[i] = cb
	}
	if s.constantBuffers[1] == nil {
		return s.constantBuffers[:1], nil
	}
	return s.constantBuffers[:], nil
}
