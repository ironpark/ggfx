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
	"errors"
	"fmt"
	"image"
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

type image12 struct {
	graphics *graphics12
	width    int
	height   int

	// format is the image's pixel format. A screen image is always PixelFormatRGBA8, whatever
	// its swap chain's format is.
	format graphicsdriver.PixelFormat

	// surface is the surface this image is presented on. surface is non-nil iff screen is true.
	surface *surface12
	screen  bool

	states            [frameCount]_D3D12_RESOURCE_STATES
	texture           *_ID3D12Resource
	rtvDescriptorHeap *_ID3D12DescriptorHeap

	uploadingStagingBuffers []*_ID3D12Resource
}

func (i *image12) Dispose() {
	// Dipose the images later as this image might still be used.
	i.graphics.removeImage(i)
}

func (i *image12) disposeImpl() {
	i.releaseUploadingStagingBuffers()
	if i.rtvDescriptorHeap != nil {
		i.rtvDescriptorHeap.Release()
		i.rtvDescriptorHeap = nil
	}
	if i.texture != nil {
		i.texture.Release()
		i.texture = nil
	}
}

func (i *image12) ReadPixels(args []graphicsdriver.PixelsArgs) error {
	if i.screen {
		return errors.New("directx: ReadPixels cannot be called on the screen")
	}

	if err := i.graphics.flushCommandList(i.graphics.drawCommandList); err != nil {
		return err
	}

	var unionRegion image.Rectangle
	for _, a := range args {
		unionRegion = unionRegion.Union(a.Region)
	}

	desc := _D3D12_RESOURCE_DESC{
		Dimension:        _D3D12_RESOURCE_DIMENSION_TEXTURE2D,
		Alignment:        0,
		Width:            uint64(unionRegion.Dx()),
		Height:           uint32(unionRegion.Dy()),
		DepthOrArraySize: 1,
		MipLevels:        0,
		Format:           dxgiFormat(i.format),
		SampleDesc: _DXGI_SAMPLE_DESC{
			Count:   1,
			Quality: 0,
		},
		Layout: _D3D12_TEXTURE_LAYOUT_UNKNOWN,
		Flags:  _D3D12_RESOURCE_FLAG_ALLOW_RENDER_TARGET,
	}
	layouts, _, _, totalBytes := i.graphics.device.GetCopyableFootprints(&desc, 0, 1, 0)
	readingStagingBuffer, err := createBuffer(i.graphics.device, totalBytes, _D3D12_HEAP_TYPE_READBACK)
	if err != nil {
		return err
	}
	defer func() {
		readingStagingBuffer.Release()
	}()

	if rb, ok := i.transiteState(_D3D12_RESOURCE_STATE_COPY_SOURCE); ok {
		i.graphics.copyCommandList.ResourceBarrier([]_D3D12_RESOURCE_BARRIER_Transition{rb})
	}

	m, err := readingStagingBuffer.Map(0, &_D3D12_RANGE{Begin: 0, End: 0})
	if err != nil {
		return err
	}

	dst := _D3D12_TEXTURE_COPY_LOCATION_PlacedFootPrint{
		pResource:       readingStagingBuffer,
		Type:            _D3D12_TEXTURE_COPY_TYPE_PLACED_FOOTPRINT,
		PlacedFootprint: layouts,
	}
	src := _D3D12_TEXTURE_COPY_LOCATION_SubresourceIndex{
		pResource:        i.texture,
		Type:             _D3D12_TEXTURE_COPY_TYPE_SUBRESOURCE_INDEX,
		SubresourceIndex: 0,
	}
	i.graphics.needFlushCopyCommandList = true
	i.graphics.copyCommandList.CopyTextureRegion_PlacedFootPrint_SubresourceIndex(
		&dst, 0, 0, 0, &src, &_D3D12_BOX{
			left:   uint32(unionRegion.Min.X),
			top:    uint32(unionRegion.Min.Y),
			front:  0,
			right:  uint32(unionRegion.Max.X),
			bottom: uint32(unionRegion.Max.Y),
			back:   1,
		})

	if err := i.graphics.flushCommandList(i.graphics.copyCommandList); err != nil {
		return err
	}

	bpp := i.format.BytesPerPixel()
	srcPix := unsafe.Slice((*byte)(unsafe.Pointer(m)), totalBytes)
	for _, a := range args {
		copyFromFootprint(a.Pixels, a.Region, srcPix, unionRegion.Min, int(layouts.Footprint.RowPitch), bpp)
	}

	readingStagingBuffer.Unmap(0, nil)

	return nil
}

func (i *image12) WritePixels(args []graphicsdriver.PixelsArgs) error {
	if i.screen {
		return errors.New("directx: WritePixels cannot be called on the screen")
	}

	if err := i.graphics.flushCommandList(i.graphics.drawCommandList); err != nil {
		return err
	}

	// Each region has a copy footprint of its own in the upload buffer, rather than all sharing one
	// of their union, which can be nearly as large as the texture for scattered regions.
	bpp := i.format.BytesPerPixel()
	var totalBytes uint64
	for _, a := range args {
		if a.Region.Empty() {
			continue
		}
		_, _, totalBytes = placeFootprint(totalBytes, a.Region.Dx(), a.Region.Dy(), bpp)
	}
	if totalBytes == 0 {
		return nil
	}
	uploadingStagingBuffer, err := createBuffer(i.graphics.device, totalBytes, _D3D12_HEAP_TYPE_UPLOAD)
	if err != nil {
		return err
	}
	i.uploadingStagingBuffers = append(i.uploadingStagingBuffers, uploadingStagingBuffer)

	if rb, ok := i.transiteState(_D3D12_RESOURCE_STATE_COPY_DEST); ok {
		i.graphics.copyCommandList.ResourceBarrier([]_D3D12_RESOURCE_BARRIER_Transition{rb})
	}

	m, err := uploadingStagingBuffer.Map(0, &_D3D12_RANGE{Begin: 0, End: 0})
	if err != nil {
		return err
	}

	i.graphics.needFlushCopyCommandList = true

	srcBytes := unsafe.Slice((*byte)(unsafe.Pointer(m)), totalBytes)
	var end uint64
	for _, a := range args {
		if a.Region.Empty() {
			continue
		}
		var offset uint64
		var rowPitch uint32
		offset, rowPitch, end = placeFootprint(end, a.Region.Dx(), a.Region.Dy(), bpp)
		copyToFootprint(srcBytes[offset:end], a.Region.Min, int(rowPitch), a.Pixels, a.Region, bpp)

		dst := _D3D12_TEXTURE_COPY_LOCATION_SubresourceIndex{
			pResource:        i.texture,
			Type:             _D3D12_TEXTURE_COPY_TYPE_SUBRESOURCE_INDEX,
			SubresourceIndex: 0,
		}
		src := _D3D12_TEXTURE_COPY_LOCATION_PlacedFootPrint{
			pResource: uploadingStagingBuffer,
			Type:      _D3D12_TEXTURE_COPY_TYPE_PLACED_FOOTPRINT,
			PlacedFootprint: _D3D12_PLACED_SUBRESOURCE_FOOTPRINT{
				Offset: offset,
				Footprint: _D3D12_SUBRESOURCE_FOOTPRINT{
					Format:   dxgiFormat(i.format),
					Width:    uint32(a.Region.Dx()),
					Height:   uint32(a.Region.Dy()),
					Depth:    1,
					RowPitch: rowPitch,
				},
			},
		}
		i.graphics.copyCommandList.CopyTextureRegion_SubresourceIndex_PlacedFootPrint(
			&dst, uint32(a.Region.Min.X), uint32(a.Region.Min.Y), 0, &src, &_D3D12_BOX{
				left:   0,
				top:    0,
				front:  0,
				right:  uint32(a.Region.Dx()),
				bottom: uint32(a.Region.Dy()),
				back:   1,
			})
	}

	uploadingStagingBuffer.Unmap(0, nil)

	return nil
}

// dxgiFormat returns the format of an offscreen image's texture, its views and its copy
// footprints.
func dxgiFormat(format graphicsdriver.PixelFormat) _DXGI_FORMAT {
	switch format {
	case graphicsdriver.PixelFormatRGBA8:
		return _DXGI_FORMAT_R8G8B8A8_UNORM
	case graphicsdriver.PixelFormatA8:
		return _DXGI_FORMAT_R8_UNORM
	}
	panic(fmt.Sprintf("directx: unexpected pixel format %s", format))
}

// renderTargetFormat returns the format of the image's render target view, which a pipeline
// state that draws into the image must be made for.
func (i *image12) renderTargetFormat() _DXGI_FORMAT {
	if i.screen {
		return _DXGI_FORMAT_B8G8R8A8_UNORM
	}
	return dxgiFormat(i.format)
}

// shaderResourceViewDesc returns the description of the view a shader samples the image through.
//
// An A8 image is an R8 texture whose view maps all four components to the red one, so that it
// samples as (v, v, v, v), as graphicsdriver.PixelFormatA8 requires.
func (i *image12) shaderResourceViewDesc() _D3D12_SHADER_RESOURCE_VIEW_DESC {
	mapping := uint32(_D3D12_DEFAULT_SHADER_4_COMPONENT_MAPPING)
	if i.format == graphicsdriver.PixelFormatA8 {
		mapping = _D3D12_ENCODE_SHADER_4_COMPONENT_MAPPING_0_0_0_0
	}
	return _D3D12_SHADER_RESOURCE_VIEW_DESC{
		Format:                  dxgiFormat(i.format),
		ViewDimension:           _D3D12_SRV_DIMENSION_TEXTURE2D,
		Shader4ComponentMapping: mapping,
		Texture2D: _D3D12_TEX2D_SRV{
			MipLevels: 1,
		},
	}
}

func (i *image12) resource() *_ID3D12Resource {
	if i.screen {
		return i.surface.renderTargets[i.surface.backBufferIndex]
	}
	return i.texture
}

func (i *image12) state() _D3D12_RESOURCE_STATES {
	if i.screen {
		return i.states[i.surface.backBufferIndex]
	}
	return i.states[0]
}

func (i *image12) setState(newState _D3D12_RESOURCE_STATES) {
	if i.screen {
		i.states[i.surface.backBufferIndex] = newState
		return
	}
	i.states[0] = newState
}

func (i *image12) transiteState(newState _D3D12_RESOURCE_STATES) (_D3D12_RESOURCE_BARRIER_Transition, bool) {
	if i.state() == newState {
		return _D3D12_RESOURCE_BARRIER_Transition{}, false
	}
	oldState := i.state()
	i.setState(newState)

	return _D3D12_RESOURCE_BARRIER_Transition{
		Type:  _D3D12_RESOURCE_BARRIER_TYPE_TRANSITION,
		Flags: _D3D12_RESOURCE_BARRIER_FLAG_NONE,
		Transition: _D3D12_RESOURCE_TRANSITION_BARRIER{
			pResource:   i.resource(),
			Subresource: _D3D12_RESOURCE_BARRIER_ALL_SUBRESOURCES,
			StateBefore: oldState,
			StateAfter:  newState,
		},
	}, true
}

func (i *image12) internalSize() (int, int) {
	if i.screen {
		return i.width, i.height
	}
	return graphics.InternalImageSize(i.width), graphics.InternalImageSize(i.height)
}

func (i *image12) setAsRenderTarget(drawCommandList *_ID3D12GraphicsCommandList, device *_ID3D12Device) error {
	if err := i.ensureRenderTargetView(device); err != nil {
		return err
	}

	if i.screen {
		rtv, err := i.surface.rtvDescriptorHeap.GetCPUDescriptorHandleForHeapStart()
		if err != nil {
			return err
		}
		rtv.Offset(int32(i.surface.backBufferIndex), i.graphics.rtvDescriptorSize)
		drawCommandList.OMSetRenderTargets([]_D3D12_CPU_DESCRIPTOR_HANDLE{rtv}, false, nil)
		return nil
	}

	rtv, err := i.rtvDescriptorHeap.GetCPUDescriptorHandleForHeapStart()
	if err != nil {
		return err
	}

	drawCommandList.OMSetRenderTargets([]_D3D12_CPU_DESCRIPTOR_HANDLE{rtv}, false, nil)
	return nil
}

func (i *image12) ensureRenderTargetView(device *_ID3D12Device) error {
	if i.screen {
		return nil
	}

	if i.rtvDescriptorHeap != nil {
		return nil
	}

	h, err := device.CreateDescriptorHeap(&_D3D12_DESCRIPTOR_HEAP_DESC{
		Type:           _D3D12_DESCRIPTOR_HEAP_TYPE_RTV,
		NumDescriptors: 1,
		Flags:          _D3D12_DESCRIPTOR_HEAP_FLAG_NONE,
		NodeMask:       0,
	})
	if err != nil {
		return err
	}
	i.rtvDescriptorHeap = h

	rtv, err := i.rtvDescriptorHeap.GetCPUDescriptorHandleForHeapStart()
	if err != nil {
		return err
	}
	device.CreateRenderTargetView(i.texture, nil, rtv)

	return nil
}

func (i *image12) releaseUploadingStagingBuffers() {
	for idx, buf := range i.uploadingStagingBuffers {
		buf.Release()
		i.uploadingStagingBuffers[idx] = nil
	}
	i.uploadingStagingBuffers = i.uploadingStagingBuffers[:0]
}
