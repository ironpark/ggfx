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

package directx

import (
	"bytes"
	"fmt"
	"image"
	"testing"
)

func TestShaderComponentMapping(t *testing.T) {
	// The values d3d12.h's macros expand to.
	if got, want := _D3D12_DEFAULT_SHADER_4_COMPONENT_MAPPING, 0x1688; got != want {
		t.Errorf("D3D12_DEFAULT_SHADER_4_COMPONENT_MAPPING: got %#x, want %#x", got, want)
	}
	if got, want := _D3D12_ENCODE_SHADER_4_COMPONENT_MAPPING_0_0_0_0, 0x1000; got != want {
		t.Errorf("D3D12_ENCODE_SHADER_4_COMPONENT_MAPPING(0, 0, 0, 0): got %#x, want %#x", got, want)
	}
}

// footprint returns a buffer laid out as GetCopyableFootprints lays out a w x h texture of bpp
// bytes a pixel: rows padded to 256 bytes, the last one unpadded. Each pad byte is 0xee.
func footprint(w, h, bpp int) ([]byte, int) {
	rowPitch := (w*bpp + textureDataPitchAlignment - 1) / textureDataPitchAlignment * textureDataPitchAlignment
	buf := bytes.Repeat([]byte{0xee}, rowPitch*(h-1)+w*bpp)
	return buf, rowPitch
}

// pixelValue is the value of byte c of the pixel at (x, y) of a test texture.
func pixelValue(x, y, c int) byte {
	return byte(x*7 + y*13 + c*3 + 1)
}

func TestCopyFootprint(t *testing.T) {
	union := image.Rect(3, 5, 3+300, 5+4)
	for _, bpp := range []int{1, 4} {
		for _, region := range []image.Rectangle{
			union,
			image.Rect(3, 5, 4, 6),
			image.Rect(10, 6, 263, 9),
			image.Rect(302, 8, 303, 9),
			image.Rect(4, 5, 7, 9),
		} {
			t.Run(fmt.Sprintf("bpp=%d/%v", bpp, region), func(t *testing.T) {
				src, rowPitch := footprint(union.Dx(), union.Dy(), bpp)
				for y := union.Min.Y; y < union.Max.Y; y++ {
					for x := union.Min.X; x < union.Max.X; x++ {
						for c := range bpp {
							src[(y-union.Min.Y)*rowPitch+(x-union.Min.X)*bpp+c] = pixelValue(x, y, c)
						}
					}
				}

				var want []byte
				for y := region.Min.Y; y < region.Max.Y; y++ {
					for x := region.Min.X; x < region.Max.X; x++ {
						for c := range bpp {
							want = append(want, pixelValue(x, y, c))
						}
					}
				}

				got := make([]byte, bpp*region.Dx()*region.Dy())
				copyFromFootprint(got, region, src, union.Min, rowPitch, bpp)
				if !bytes.Equal(got, want) {
					t.Errorf("copyFromFootprint: got %v, want %v", got, want)
				}

				// Writing the region back into a blank footprint touches its pixels and no others.
				dst, _ := footprint(union.Dx(), union.Dy(), bpp)
				copyToFootprint(dst, union.Min, rowPitch, want, region, bpp)
				for i, b := range dst {
					x := union.Min.X + i%rowPitch/bpp
					y := union.Min.Y + i/rowPitch
					c := i % rowPitch % bpp
					in := i%rowPitch < union.Dx()*bpp && image.Pt(x, y).In(region)
					if in && b != pixelValue(x, y, c) {
						t.Fatalf("copyToFootprint: byte %d (%d, %d, %d): got %#x, want %#x", i, x, y, c, b, pixelValue(x, y, c))
					}
					if !in && b != 0xee {
						t.Fatalf("copyToFootprint: byte %d outside the region: got %#x, want 0xee", i, b)
					}
				}
			})
		}
	}
}

func TestPlaceFootprint(t *testing.T) {
	// Each region's footprint starts on a 512-byte boundary after the last, with rows 256-byte
	// aligned and the last row unpadded.
	var end uint64
	for _, tc := range []struct {
		w, h, bpp int
		offset    uint64
		pitch     uint32
		end       uint64
	}{
		{10, 14, 1, 0, 256, 256*13 + 10},
		{10, 14, 4, 3584, 256, 3584 + 256*13 + 40},
		{65, 2, 4, 7168, 512, 7168 + 512 + 260},
		{1, 1, 1, 8192, 256, 8193},
	} {
		offset, pitch, newEnd := placeFootprint(end, tc.w, tc.h, tc.bpp)
		if offset != tc.offset || pitch != tc.pitch || newEnd != tc.end {
			t.Errorf("placeFootprint(%d, %d, %d, %d): got (%d, %d, %d), want (%d, %d, %d)", end, tc.w, tc.h, tc.bpp, offset, pitch, newEnd, tc.offset, tc.pitch, tc.end)
		}
		end = newEnd
	}
}
