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
	"math"
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

var inputElementDescsForDX12 []_D3D12_INPUT_ELEMENT_DESC

func init() {
	for i, f := range vertexAttributeFormats() {
		inputElementDescsForDX12 = append(inputElementDescsForDX12, _D3D12_INPUT_ELEMENT_DESC{
			SemanticName:         vertexAttributeSemanticName,
			SemanticIndex:        uint32(i),
			Format:               f,
			InputSlot:            0,
			AlignedByteOffset:    _D3D12_APPEND_ALIGNED_ELEMENT,
			InputSlotClass:       _D3D12_INPUT_CLASSIFICATION_PER_VERTEX_DATA,
			InstanceDataStepRate: 0,
		})
	}
}

// maxDrawsPerFrame is the worst-case number of draws that rewrite both CBV and SRV tables before
// waiting for the GPU to reuse the frame's descriptors. Each draw uses at most
// numConstantBufferAndSourceTextures descriptors (2 CBVs and 4 SRVs), so the heap holds
// frameCount * maxDescriptorsPerFrame descriptors, well within the 1,000,000 of resource binding tier 1.
const (
	maxDrawsPerFrame       = 4096
	maxDescriptorsPerFrame = maxDrawsPerFrame * numConstantBufferAndSourceTextures
)

type pipelineStates struct {
	rootSignature *_ID3D12RootSignature

	shaderDescriptorHeap *_ID3D12DescriptorHeap
	shaderDescriptorSize uint32

	// shaderDescriptorCPUStart and shaderDescriptorGPUStart are the heap's start, which a draw
	// offsets from without asking the heap again.
	shaderDescriptorCPUStart _D3D12_CPU_DESCRIPTOR_HANDLE
	shaderDescriptorGPUStart _D3D12_GPU_DESCRIPTOR_HANDLE

	// frames are the per-frame uniform rings and descriptor counts. A frame's part is reused only
	// after the GPU completed the commands that read it (see resetFrame).
	frames [frameCount]frameUniforms12
}

// frameUniforms12 is one frame's upload ring for the uniform blocks and its share of the shader
// descriptor heap.
type frameUniforms12 struct {
	// buffer is persistently mapped at data. Each draw takes the next aligned range from offset.
	buffer     *_ID3D12Resource
	gpuAddress _D3D12_GPU_VIRTUAL_ADDRESS
	data       uintptr
	size       uint32
	offset     uint32

	// retired are the buffers the ring outgrew this frame. The GPU might still read them until the
	// frame completes.
	retired []*_ID3D12Resource

	// Cached last-uploaded uniform blocks within this frame. Safe across ring growth because
	// retired buffers stay alive until reset().
	lastInternalVersion uint64
	lastInternalGPU     _D3D12_GPU_VIRTUAL_ADDRESS
	lastInternalSize    uint32
	lastUserVersion     uint64
	lastUserGPU         _D3D12_GPU_VIRTUAL_ADDRESS
	lastUserSize        uint32

	// descriptors is how many descriptors have been taken from this frame's share of the heap.
	descriptors int
}

// minUniformRingSize is the initial size of a frame's uniform ring in bytes: 128 draws without a
// large user block.
const minUniformRingSize = 64 * 1024

// alloc returns the GPU and CPU addresses of size bytes of the ring, growing it when it is full.
// size must be a multiple of constantBufferAlignment, so every range stays aligned.
func (f *frameUniforms12) alloc(device *_ID3D12Device, size uint32) (_D3D12_GPU_VIRTUAL_ADDRESS, uintptr, error) {
	if f.buffer == nil || f.offset+size > f.size {
		// The commands recorded so far might read the old buffer. Keep it until the frame is reset.
		if f.buffer != nil {
			f.retired = append(f.retired, f.buffer)
		}
		newSize := max(f.size*2, size, minUniformRingSize)
		f.buffer, f.gpuAddress, f.data, f.size, f.offset = nil, 0, 0, 0, 0

		b, err := createBuffer(device, uint64(newSize), _D3D12_HEAP_TYPE_UPLOAD)
		if err != nil {
			return 0, 0, err
		}
		// An upload heap resource can stay mapped while the GPU reads it; the CPU only writes it.
		m, err := b.Map(0, &_D3D12_RANGE{Begin: 0, End: 0})
		if err != nil {
			b.Release()
			return 0, 0, err
		}
		if m == 0 {
			b.Release()
			return 0, 0, fmt.Errorf("directx: ID3D12Resource::Map failed")
		}
		f.buffer = b
		f.gpuAddress = b.GetGPUVirtualAddress()
		f.data = m
		f.size = newSize
	}
	off := f.offset
	f.offset += size
	return f.gpuAddress + _D3D12_GPU_VIRTUAL_ADDRESS(off), f.data + uintptr(off), nil
}

// reset makes the whole ring and the descriptors available again. The caller must make sure that
// the GPU completed the commands that used them.
func (f *frameUniforms12) reset() {
	for i, b := range f.retired {
		b.Unmap(0, nil)
		b.Release()
		f.retired[i] = nil
	}
	f.retired = f.retired[:0]
	f.offset = 0
	f.descriptors = 0
	f.lastInternalVersion = 0
	f.lastInternalGPU = 0
	f.lastInternalSize = 0
	f.lastUserVersion = 0
	f.lastUserGPU = 0
	f.lastUserSize = 0
}

func (f *frameUniforms12) release() {
	f.reset()
	if f.buffer != nil {
		f.buffer.Unmap(0, nil)
		f.buffer.Release()
	}
	*f = frameUniforms12{}
}

// numConstantBuffers is how many constant buffer views a draw uses: the internal uniform block
// and the user's uniform block.
const numConstantBuffers = 2

const numConstantBufferAndSourceTextures = numConstantBuffers + graphics.ShaderSrcImageCount

// constantBufferAlignment is the alignment a constant buffer view's location and size need.
const constantBufferAlignment = 256

func alignUpConstantBuffer(x uint32) uint32 {
	if x == 0 {
		return 0
	}
	return ((x-1)/constantBufferAlignment + 1) * constantBufferAlignment
}

func (p *pipelineStates) initialize(device *_ID3D12Device) (ferr error) {
	// Create a CBV/SRV/UAV descriptor heap. Each frame owns maxDescriptorsPerFrame contiguous
	// descriptors, allocated linearly as 2-descriptor CBV tables and 4-descriptor SRV tables.
	shaderH, err := device.CreateDescriptorHeap(&_D3D12_DESCRIPTOR_HEAP_DESC{
		Type:           _D3D12_DESCRIPTOR_HEAP_TYPE_CBV_SRV_UAV,
		NumDescriptors: frameCount * maxDescriptorsPerFrame,
		Flags:          _D3D12_DESCRIPTOR_HEAP_FLAG_SHADER_VISIBLE,
		NodeMask:       0,
	})
	if err != nil {
		return err
	}
	p.shaderDescriptorHeap = shaderH
	defer func() {
		if ferr != nil {
			p.release()
		}
	}()
	p.shaderDescriptorSize = device.GetDescriptorHandleIncrementSize(_D3D12_DESCRIPTOR_HEAP_TYPE_CBV_SRV_UAV)
	if p.shaderDescriptorCPUStart, err = shaderH.GetCPUDescriptorHandleForHeapStart(); err != nil {
		return err
	}
	if p.shaderDescriptorGPUStart, err = shaderH.GetGPUDescriptorHandleForHeapStart(); err != nil {
		return err
	}

	return nil
}

// release releases the objects created by initialize, the root signature, and the uniform rings.
// The caller must make sure that the GPU no longer uses them.
func (p *pipelineStates) release() {
	for i := range p.frames {
		p.frames[i].release()
	}
	if p.shaderDescriptorHeap != nil {
		p.shaderDescriptorHeap.Release()
		p.shaderDescriptorHeap = nil
	}
	if p.rootSignature != nil {
		p.rootSignature.Release()
		p.rootSignature = nil
	}
	p.shaderDescriptorSize = 0
	p.shaderDescriptorCPUStart = _D3D12_CPU_DESCRIPTOR_HANDLE{}
	p.shaderDescriptorGPUStart = _D3D12_GPU_DESCRIPTOR_HANDLE{}
}

// hasRoom reports whether the frame has enough descriptors left for a draw that updates both
// descriptor tables.
func (p *pipelineStates) hasRoom(frameIndex int) bool {
	return p.frames[frameIndex].descriptors+numConstantBufferAndSourceTextures <= maxDescriptorsPerFrame
}

// resetFrame makes the frame's uniform ring and descriptors available again. The caller must make
// sure that the GPU completed the frame's commands submitted so far.
func (p *pipelineStates) resetFrame(frameIndex int) {
	p.frames[frameIndex].reset()
}

// drawListState12 is the state set on the draw command list since its last Reset, which a draw
// need not set again. Reset discards a command list's state, so every Reset must clear this.
type drawListState12 struct {
	// invariantsSet reports whether the root signature, the descriptor heap and the primitive
	// topology, which no draw changes, are set.
	invariantsSet bool

	rtvImage      *image12
	rtvBackIndex  int
	viewportW     int
	viewportH     int
	vertexBuffer  _D3D12_VERTEX_BUFFER_VIEW
	indexBuffer   _D3D12_INDEX_BUFFER_VIEW
	pipelineState *_ID3D12PipelineState
	scissor       _D3D12_RECT
	scissorValid  bool

	// Cached CBV and SRV descriptor table bindings for the current command list. Cleared whenever
	// the command list or descriptor heap is reset.
	cbvValid       bool
	cbvInternalGPU _D3D12_GPU_VIRTUAL_ADDRESS
	cbvInternalSz  uint32
	cbvUserGPU     _D3D12_GPU_VIRTUAL_ADDRESS
	cbvUserSz      uint32

	srvValid     bool
	srvResources [graphics.ShaderSrcImageCount]*_ID3D12Resource
	srvFormats   [graphics.ShaderSrcImageCount]graphicsdriver.PixelFormat
}

func (d *drawListState12) resetDescriptorBindings() {
	d.cbvValid = false
	d.srvValid = false
}

// nullShaderResourceViewDesc describes the view in a source texture's slot that a draw does not use,
// so that no slot keeps a stale view of a released texture.
var nullShaderResourceViewDesc = _D3D12_SHADER_RESOURCE_VIEW_DESC{
	Format:                  _DXGI_FORMAT_R8G8B8A8_UNORM,
	ViewDimension:           _D3D12_SRV_DIMENSION_TEXTURE2D,
	Shader4ComponentMapping: uint32(_D3D12_DEFAULT_SHADER_4_COMPONENT_MAPPING),
	Texture2D: _D3D12_TEX2D_SRV{
		MipLevels: 1,
	},
}

// drawTriangles records a draw. internal is the internal uniform block and user the user's block.
// The caller must make sure that hasRoom(frameIndex) holds.
func (p *pipelineStates) drawTriangles(device *_ID3D12Device, commandList *_ID3D12GraphicsCommandList, listState *drawListState12, frameIndex int, rtvFormat _DXGI_FORMAT, srcs [graphics.ShaderSrcImageCount]*image12, shader *shader12, dstRegions []graphicsdriver.DstRegion, internal, user []uint32, versions graphicsdriver.UniformVersions, blend graphicsdriver.Blend, indexOffset int) error {
	f := &p.frames[frameIndex]
	if f.descriptors+numConstantBufferAndSourceTextures > maxDescriptorsPerFrame {
		return fmt.Errorf("directx: too many draws in a frame")
	}

	ps, err := shader.pipelineState(blend, rtvFormat)
	if err != nil {
		return err
	}
	rs, err := p.ensureRootSignature(device)
	if err != nil {
		return err
	}

	// Reuse already-uploaded uniform ring slices when the block version and CBV size match the
	// previous draw in this frame (retired ring buffers remain valid until resetFrame).
	// A shader without a user uniform block still gets a real, unused view rather than a null descriptor.
	internalSize := alignUpConstantBuffer(internalConstantBufferSize)
	userSize := max(alignUpConstantBuffer(shader.userConstantBufferSize), constantBufferAlignment)
	if len(user) > 0 && uint32(len(user)*4) > userSize {
		return fmt.Errorf("directx: user uniform block (%d bytes) exceeds shader size (%d bytes)", len(user)*4, userSize)
	}

	reuseInternal := versions.Internal != 0 && f.lastInternalVersion == versions.Internal && f.lastInternalSize == internalSize
	reuseUser := versions.User != 0 && f.lastUserVersion == versions.User && f.lastUserSize == userSize

	var needSize uint32
	if !reuseInternal {
		needSize += internalSize
	}
	if !reuseUser {
		needSize += userSize
	}

	var allocGPU _D3D12_GPU_VIRTUAL_ADDRESS
	var allocCPU uintptr
	if needSize > 0 {
		allocGPU, allocCPU, err = f.alloc(device, needSize)
		if err != nil {
			return err
		}
	}

	internalGPU := f.lastInternalGPU
	if !reuseInternal {
		internalGPU = allocGPU
		copy(unsafe.Slice((*uint32)(unsafe.Pointer(allocCPU)), len(internal)), internal)
		allocGPU += _D3D12_GPU_VIRTUAL_ADDRESS(internalSize)
		allocCPU += uintptr(internalSize)
		f.lastInternalVersion = versions.Internal
		f.lastInternalGPU = internalGPU
		f.lastInternalSize = internalSize
	}

	userGPU := f.lastUserGPU
	if !reuseUser {
		userGPU = allocGPU
		if len(user) > 0 {
			copy(unsafe.Slice((*uint32)(unsafe.Pointer(allocCPU)), len(user)), user)
		}
		f.lastUserVersion = versions.User
		f.lastUserGPU = userGPU
		f.lastUserSize = userSize
	}

	if !listState.invariantsSet {
		commandList.SetGraphicsRootSignature(rs)
		commandList.SetDescriptorHeaps([]*_ID3D12DescriptorHeap{
			p.shaderDescriptorHeap,
		})
		commandList.IASetPrimitiveTopology(_D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST)
		listState.invariantsSet = true
	}

	// Only write and bind the CBV table when the uniform buffers or sizes differ from the previous
	// draw on this command list.
	if !listState.cbvValid ||
		listState.cbvInternalGPU != internalGPU ||
		listState.cbvInternalSz != internalSize ||
		listState.cbvUserGPU != userGPU ||
		listState.cbvUserSz != userSize {
		cbvOffset := int32(frameIndex*maxDescriptorsPerFrame + f.descriptors)
		f.descriptors += numConstantBuffers

		h := p.shaderDescriptorCPUStart
		h.Offset(cbvOffset, p.shaderDescriptorSize)
		device.CreateConstantBufferView(&_D3D12_CONSTANT_BUFFER_VIEW_DESC{
			BufferLocation: internalGPU,
			SizeInBytes:    internalSize,
		}, h)
		h.Offset(1, p.shaderDescriptorSize)
		device.CreateConstantBufferView(&_D3D12_CONSTANT_BUFFER_VIEW_DESC{
			BufferLocation: userGPU,
			SizeInBytes:    userSize,
		}, h)

		gh := p.shaderDescriptorGPUStart
		gh.Offset(cbvOffset, p.shaderDescriptorSize)
		commandList.SetGraphicsRootDescriptorTable(0, gh)

		listState.cbvValid = true
		listState.cbvInternalGPU = internalGPU
		listState.cbvInternalSz = internalSize
		listState.cbvUserGPU = userGPU
		listState.cbvUserSz = userSize
	}

	// Only write and bind the SRV table when the source textures or formats differ from the
	// previous draw on this command list. Disposed images are not released until the frame
	// completes, so resource pointers remain unique for the life of the command list.
	var srvResources [graphics.ShaderSrcImageCount]*_ID3D12Resource
	var srvFormats [graphics.ShaderSrcImageCount]graphicsdriver.PixelFormat
	for i, src := range srcs {
		if src != nil {
			srvResources[i] = src.resource()
			srvFormats[i] = src.format
		}
	}
	if !listState.srvValid || listState.srvResources != srvResources || listState.srvFormats != srvFormats {
		srvOffset := int32(frameIndex*maxDescriptorsPerFrame + f.descriptors)
		f.descriptors += graphics.ShaderSrcImageCount

		h := p.shaderDescriptorCPUStart
		h.Offset(srvOffset, p.shaderDescriptorSize)
		for i, src := range srcs {
			if i > 0 {
				h.Offset(1, p.shaderDescriptorSize)
			}
			if src == nil {
				device.CreateShaderResourceView(nil, &nullShaderResourceViewDesc, h)
				continue
			}
			desc := src.shaderResourceViewDesc()
			device.CreateShaderResourceView(srvResources[i], &desc, h)
		}

		gh := p.shaderDescriptorGPUStart
		gh.Offset(srvOffset, p.shaderDescriptorSize)
		commandList.SetGraphicsRootDescriptorTable(1, gh)

		listState.srvValid = true
		listState.srvResources = srvResources
		listState.srvFormats = srvFormats
	}

	if listState.pipelineState != ps {
		commandList.SetPipelineState(ps)
		listState.pipelineState = ps
	}

	for _, dstRegion := range dstRegions {
		r := _D3D12_RECT{
			left:   int32(dstRegion.Region.Min.X),
			top:    int32(dstRegion.Region.Min.Y),
			right:  int32(dstRegion.Region.Max.X),
			bottom: int32(dstRegion.Region.Max.Y),
		}
		if !listState.scissorValid || listState.scissor != r {
			commandList.RSSetScissorRects([]_D3D12_RECT{r})
			listState.scissor = r
			listState.scissorValid = true
		}
		commandList.DrawIndexedInstanced(uint32(dstRegion.IndexCount), 1, uint32(indexOffset), 0, 0)
		indexOffset += dstRegion.IndexCount
	}

	return nil
}

// ensureRootSignature returns the root signature owned by p, which the caller must not release.
func (p *pipelineStates) ensureRootSignature(device *_ID3D12Device) (*_ID3D12RootSignature, error) {
	if p.rootSignature != nil {
		return p.rootSignature, nil
	}

	cbv := _D3D12_DESCRIPTOR_RANGE{
		RangeType:                         _D3D12_DESCRIPTOR_RANGE_TYPE_CBV, // b0, b1
		NumDescriptors:                    numConstantBuffers,
		BaseShaderRegister:                0,
		RegisterSpace:                     0,
		OffsetInDescriptorsFromTableStart: 0,
	}
	srv := _D3D12_DESCRIPTOR_RANGE{
		RangeType:                         _D3D12_DESCRIPTOR_RANGE_TYPE_SRV, // t0
		NumDescriptors:                    graphics.ShaderSrcImageCount,
		BaseShaderRegister:                0,
		RegisterSpace:                     0,
		OffsetInDescriptorsFromTableStart: 0,
	}

	rootParams := [...]_D3D12_ROOT_PARAMETER{
		{
			ParameterType: _D3D12_ROOT_PARAMETER_TYPE_DESCRIPTOR_TABLE,
			DescriptorTable: _D3D12_ROOT_DESCRIPTOR_TABLE{
				NumDescriptorRanges: 1,
				pDescriptorRanges:   &cbv,
			},
			ShaderVisibility: _D3D12_SHADER_VISIBILITY_ALL,
		},
		{
			ParameterType: _D3D12_ROOT_PARAMETER_TYPE_DESCRIPTOR_TABLE,
			DescriptorTable: _D3D12_ROOT_DESCRIPTOR_TABLE{
				NumDescriptorRanges: 1,
				pDescriptorRanges:   &srv,
			},
			ShaderVisibility: _D3D12_SHADER_VISIBILITY_PIXEL,
		},
	}

	// Create a root signature.
	sig, err := _D3D12SerializeRootSignature(&_D3D12_ROOT_SIGNATURE_DESC{
		NumParameters:     uint32(len(rootParams)),
		pParameters:       &rootParams[0],
		NumStaticSamplers: 0,
		pStaticSamplers:   nil,
		Flags:             _D3D12_ROOT_SIGNATURE_FLAG_ALLOW_INPUT_ASSEMBLER_INPUT_LAYOUT,
	}, _D3D_ROOT_SIGNATURE_VERSION_1_0)
	if err != nil {
		return nil, err
	}
	defer sig.Release()

	rs, err := device.CreateRootSignature(0, sig.GetBufferPointer(), sig.GetBufferSize())
	if err != nil {
		return nil, err
	}

	p.rootSignature = rs

	return p.rootSignature, nil
}

// newPipelineState creates a pipeline state that draws into a render target view of rtvFormat.
//
// An R8 target (an A8 image) blends its red channel by the color part of blend. It has no alpha
// channel, so a destination-alpha factor reads 1, as graphicsdriver.PixelFormatA8 documents;
// the alpha part of blend is unused. R8_UNORM is a required render target and blendable format
// at feature level 11_0, the lowest Direct3D 12 runs at.
func (p *pipelineStates) newPipelineState(device *_ID3D12Device, vsh, psh *_ID3DBlob, blend graphicsdriver.Blend, rtvFormat _DXGI_FORMAT) (*_ID3D12PipelineState, error) {
	rootSignature, err := p.ensureRootSignature(device)
	if err != nil {
		return nil, err
	}

	writeMask := uint8(_D3D12_COLOR_WRITE_ENABLE_ALL)

	// Create a pipeline state.
	psoDesc := _D3D12_GRAPHICS_PIPELINE_STATE_DESC{
		pRootSignature: rootSignature,
		VS: _D3D12_SHADER_BYTECODE{
			pShaderBytecode: vsh.GetBufferPointer(),
			BytecodeLength:  vsh.GetBufferSize(),
		},
		PS: _D3D12_SHADER_BYTECODE{
			pShaderBytecode: psh.GetBufferPointer(),
			BytecodeLength:  psh.GetBufferSize(),
		},
		BlendState: _D3D12_BLEND_DESC{
			AlphaToCoverageEnable:  0,
			IndependentBlendEnable: 0,
			RenderTarget: [8]_D3D12_RENDER_TARGET_BLEND_DESC{
				{
					BlendEnable:           1,
					LogicOpEnable:         0,
					SrcBlend:              blendFactorToD3D[_D3D12_BLEND](blend.BlendFactorSourceRGB, false),
					DestBlend:             blendFactorToD3D[_D3D12_BLEND](blend.BlendFactorDestinationRGB, false),
					BlendOp:               blendOperationToD3D[_D3D12_BLEND_OP](blend.BlendOperationRGB),
					SrcBlendAlpha:         blendFactorToD3D[_D3D12_BLEND](blend.BlendFactorSourceAlpha, true),
					DestBlendAlpha:        blendFactorToD3D[_D3D12_BLEND](blend.BlendFactorDestinationAlpha, true),
					BlendOpAlpha:          blendOperationToD3D[_D3D12_BLEND_OP](blend.BlendOperationAlpha),
					LogicOp:               _D3D12_LOGIC_OP_NOOP,
					RenderTargetWriteMask: writeMask,
				},
			},
		},
		SampleMask: math.MaxUint32,
		RasterizerState: _D3D12_RASTERIZER_DESC{
			FillMode:              _D3D12_FILL_MODE_SOLID,
			CullMode:              _D3D12_CULL_MODE_NONE,
			FrontCounterClockwise: 0,
			DepthBias:             _D3D12_DEFAULT_DEPTH_BIAS,
			DepthBiasClamp:        _D3D12_DEFAULT_DEPTH_BIAS_CLAMP,
			SlopeScaledDepthBias:  _D3D12_DEFAULT_SLOPE_SCALED_DEPTH_BIAS,
			DepthClipEnable:       0,
			MultisampleEnable:     0,
			AntialiasedLineEnable: 0,
			ForcedSampleCount:     0,
			ConservativeRaster:    _D3D12_CONSERVATIVE_RASTERIZATION_MODE_OFF,
		},
		InputLayout: _D3D12_INPUT_LAYOUT_DESC{
			pInputElementDescs: &inputElementDescsForDX12[0],
			NumElements:        uint32(len(inputElementDescsForDX12)),
		},
		PrimitiveTopologyType: _D3D12_PRIMITIVE_TOPOLOGY_TYPE_TRIANGLE,
		NumRenderTargets:      1,
		RTVFormats: [8]_DXGI_FORMAT{
			rtvFormat,
		},
		DSVFormat: _DXGI_FORMAT_UNKNOWN,
		SampleDesc: _DXGI_SAMPLE_DESC{
			Count:   1,
			Quality: 0,
		},
	}

	s, err := device.CreateGraphicsPipelineState(&psoDesc)
	if err != nil {
		return nil, err
	}
	return s, nil
}
