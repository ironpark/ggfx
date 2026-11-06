// Copyright 2026 The Ebitengine Authors
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

// This file has no platform suffix so that its arithmetic is tested on every platform.

package directx

import (
	"image"
)

// The D3D12_SHADER_COMPONENT_MAPPING encoding of d3d12.h. A mapping is four 3-bit fields, the
// source of the red, green, blue and alpha a shader reads, and a bit that is always set.
const (
	_D3D12_SHADER_COMPONENT_MAPPING_MASK                                     = 0x7
	_D3D12_SHADER_COMPONENT_MAPPING_SHIFT                                    = 3
	_D3D12_SHADER_COMPONENT_MAPPING_ALWAYS_SET_BIT_AVOIDING_ZEROMEM_MISTAKES = 1 << (_D3D12_SHADER_COMPONENT_MAPPING_SHIFT * 4)

	// _D3D12_ENCODE_SHADER_4_COMPONENT_MAPPING_0_1_2_3 is D3D12_ENCODE_SHADER_4_COMPONENT_MAPPING(0, 1, 2, 3):
	// each component reads itself.
	_D3D12_ENCODE_SHADER_4_COMPONENT_MAPPING_0_1_2_3 = (0 & _D3D12_SHADER_COMPONENT_MAPPING_MASK) |
		((1 & _D3D12_SHADER_COMPONENT_MAPPING_MASK) << _D3D12_SHADER_COMPONENT_MAPPING_SHIFT) |
		((2 & _D3D12_SHADER_COMPONENT_MAPPING_MASK) << (_D3D12_SHADER_COMPONENT_MAPPING_SHIFT * 2)) |
		((3 & _D3D12_SHADER_COMPONENT_MAPPING_MASK) << (_D3D12_SHADER_COMPONENT_MAPPING_SHIFT * 3)) |
		_D3D12_SHADER_COMPONENT_MAPPING_ALWAYS_SET_BIT_AVOIDING_ZEROMEM_MISTAKES

	// _D3D12_ENCODE_SHADER_4_COMPONENT_MAPPING_0_0_0_0 is D3D12_ENCODE_SHADER_4_COMPONENT_MAPPING(0, 0, 0, 0):
	// every component reads the first one, so an R8 texture samples as (r, r, r, r).
	_D3D12_ENCODE_SHADER_4_COMPONENT_MAPPING_0_0_0_0 = _D3D12_SHADER_COMPONENT_MAPPING_ALWAYS_SET_BIT_AVOIDING_ZEROMEM_MISTAKES

	_D3D12_DEFAULT_SHADER_4_COMPONENT_MAPPING = _D3D12_ENCODE_SHADER_4_COMPONENT_MAPPING_0_1_2_3
)

// copyFromFootprint copies the pixels of region out of src into dst, tightly packed.
//
// src is a copy footprint, as GetCopyableFootprints lays one out: its first row is the row
// origin.Y of the image starting at column origin.X, and its rows are rowPitch bytes apart. The
// last row of a footprint is not padded, so src may end right after region's last pixel.
func copyFromFootprint(dst []byte, region image.Rectangle, src []byte, origin image.Point, rowPitch, bytesPerPixel int) {
	rowBytes := bytesPerPixel * region.Dx()
	offset := bytesPerPixel*(region.Min.X-origin.X) + rowPitch*(region.Min.Y-origin.Y)
	if offset == 0 && rowPitch == rowBytes {
		n := rowBytes * region.Dy()
		copy(dst[:n], src[:n])
		return
	}
	for j := range region.Dy() {
		s := offset + j*rowPitch
		copy(dst[j*rowBytes:(j+1)*rowBytes], src[s:s+rowBytes])
	}
}

// copyToFootprint copies the tightly packed pixels of region from src into dst, a copy footprint
// laid out as copyFromFootprint describes.
func copyToFootprint(dst []byte, origin image.Point, rowPitch int, src []byte, region image.Rectangle, bytesPerPixel int) {
	rowBytes := bytesPerPixel * region.Dx()
	offset := bytesPerPixel*(region.Min.X-origin.X) + rowPitch*(region.Min.Y-origin.Y)
	for j := range region.Dy() {
		d := offset + j*rowPitch
		copy(dst[d:d+rowBytes], src[j*rowBytes:(j+1)*rowBytes])
	}
}
