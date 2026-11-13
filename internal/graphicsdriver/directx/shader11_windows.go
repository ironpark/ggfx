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
	"slices"
	"unsafe"
)

// vertexStage11 is the vertex shader and the input layout that every shader with one texture count
// shares (see vertexShaderBlob). It lives as long as the device.
type vertexStage11 struct {
	vertexShader *_ID3D11VertexShader
	inputLayout  *_ID3D11InputLayout
}

type shader11 struct {
	graphics               *graphics11
	userConstantBufferSize uint32
	pixelShaderBlob        *_ID3DBlob

	// vertexStage is shared and owned by graphics.
	vertexStage *vertexStage11
	pixelShader *_ID3D11PixelShader

	// userConstantBuffer is the constant buffer for the user's uniform block, or nil when the shader
	// has none. The internal block's buffer is shared (see graphics11.internalConstantBuffer).
	userConstantBuffer *_ID3D11Buffer

	// userUniforms is what userConstantBuffer holds, or empty when unknown, so that a draw with the
	// same values skips the upload.
	userUniforms []uint32
}

func (s *shader11) Dispose() {
	if s.pixelShaderBlob != nil {
		s.pixelShaderBlob.Release()
		s.pixelShaderBlob = nil
	}
	if s.pixelShader != nil {
		s.pixelShader.Release()
		s.pixelShader = nil
	}
	if s.userConstantBuffer != nil {
		s.userConstantBuffer.Release()
		s.userConstantBuffer = nil
	}
	s.vertexStage = nil
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

// ensureUserConstantBuffer returns the constant buffer for the user's uniform block, or nil when the
// shader has none.
func (s *shader11) ensureUserConstantBuffer() (*_ID3D11Buffer, error) {
	if s.userConstantBuffer != nil || s.userConstantBufferSize == 0 {
		return s.userConstantBuffer, nil
	}
	cb, err := s.graphics.newConstantBuffer(s.userConstantBufferSize)
	if err != nil {
		return nil, err
	}
	s.userConstantBuffer = cb
	return cb, nil
}

func alignUp16(x uint32) uint32 {
	if x%16 == 0 {
		return x
	}
	return x + 16 - (x % 16)
}

func (g *graphics11) newConstantBuffer(size uint32) (*_ID3D11Buffer, error) {
	return g.device.CreateBuffer(&_D3D11_BUFFER_DESC{
		ByteWidth:      size,
		Usage:          _D3D11_USAGE_DYNAMIC,
		BindFlags:      uint32(_D3D11_BIND_CONSTANT_BUFFER),
		CPUAccessFlags: uint32(_D3D11_CPU_ACCESS_WRITE),
	}, nil)
}

// uploadConstantBuffer writes data to cb unless last, what cb holds, equals it. A buffer keeps its
// contents between draws, so an unchanged block needs no Map(WRITE_DISCARD). No block is empty, so
// an empty last means that what cb holds is unknown.
func (g *graphics11) uploadConstantBuffer(cb *_ID3D11Buffer, data []uint32, last *[]uint32) error {
	if len(*last) > 0 && slices.Equal(*last, data) {
		return nil
	}
	var mapped _D3D11_MAPPED_SUBRESOURCE
	if err := g.deviceContext.Map(unsafe.Pointer(cb), 0, _D3D11_MAP_WRITE_DISCARD, 0, &mapped); err != nil {
		*last = (*last)[:0]
		return err
	}
	copy(unsafe.Slice((*uint32)(mapped.pData), len(data)), data)
	g.deviceContext.Unmap(unsafe.Pointer(cb), 0)
	*last = append((*last)[:0], data...)
	return nil
}

// vertexStage returns the vertex shader and the input layout shared by the shaders with
// textureCount source textures.
func (g *graphics11) vertexStage(textureCount int) (_ *vertexStage11, ferr error) {
	if v, ok := g.vertexStages[textureCount]; ok {
		return v, nil
	}

	blob, err := vertexShaderBlob(textureCount)
	if err != nil {
		return nil, err
	}
	vs, err := g.device.CreateVertexShader(blob.GetBufferPointer(), blob.GetBufferSize(), nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if ferr != nil {
			vs.Release()
		}
	}()
	il, err := g.device.CreateInputLayout(inputElementDescsForDX11, blob.GetBufferPointer(), blob.GetBufferSize())
	if err != nil {
		return nil, err
	}

	v := &vertexStage11{
		vertexShader: vs,
		inputLayout:  il,
	}
	if g.vertexStages == nil {
		g.vertexStages = map[int]*vertexStage11{}
	}
	g.vertexStages[textureCount] = v
	return v, nil
}
