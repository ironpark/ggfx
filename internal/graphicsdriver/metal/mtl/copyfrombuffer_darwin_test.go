// Copyright 2026 The ggfx Authors
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

package mtl_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/mtl"
)

// CopyFromBuffer copies rows tightly packed at an offset in a buffer to a region of a texture, for
// four-byte and one-byte pixels.
func TestCopyFromBuffer(t *testing.T) {
	device, err := mtl.CreateSystemDefaultDevice()
	if err != nil {
		t.Skipf("no Metal device: %v", err)
	}
	for _, tc := range []struct {
		name   string
		format mtl.PixelFormat
		bpp    int
	}{
		{"RGBA8", mtl.PixelFormatRGBA8UNorm, 4},
		{"R8", mtl.PixelFormatR8UNorm, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const w, h = 16, 8
			storage := mtl.StorageModeManaged
			opts := mtl.ResourceStorageModeManaged
			if runtime.GOOS == "ios" {
				storage, opts = mtl.StorageModeShared, mtl.ResourceStorageModeShared
			}
			texture := device.NewTextureWithDescriptor(mtl.TextureDescriptor{
				TextureType: mtl.TextureType2D,
				PixelFormat: tc.format,
				Width:       w,
				Height:      h,
				StorageMode: storage,
				Usage:       mtl.TextureUsageShaderRead,
			})
			defer texture.Release()

			// Write a 5x3 region at (3, 2) from offset 48 of the buffer.
			const offset, rw, rh, x0, y0 = 48, 5, 3, 3, 2
			bpr := tc.bpp * rw
			buf := device.NewBufferWithLength(uintptr(offset+bpr*rh), opts)
			defer buf.Release()
			contents := unsafe.Slice((*byte)(buf.Contents()), offset+bpr*rh)
			for i := range bpr * rh {
				contents[offset+i] = byte(i + 1)
			}
			buf.DidModifyRange(0, uintptr(len(contents)))

			cq := device.NewCommandQueue()
			defer cq.Release()
			cb := cq.CommandBuffer()
			bce := cb.BlitCommandEncoder()
			bce.CopyFromBuffer(buf, offset, bpr, bpr*rh, mtl.Size{Width: rw, Height: rh, Depth: 1}, texture, 0, 0, mtl.Origin{X: x0, Y: y0})
			bce.SynchronizeTexture(texture, 0, 0)
			bce.EndEncoding()
			cb.Commit()
			cb.WaitUntilCompleted()

			got := make([]byte, tc.bpp*w*h)
			if err := texture.GetBytes(got, tc.bpp*w, mtl.Region{Size: mtl.Size{Width: w, Height: h, Depth: 1}}, 0); err != nil {
				t.Fatal(err)
			}
			for y := range h {
				for x := range w {
					for c := range tc.bpp {
						var want byte
						if x >= x0 && x < x0+rw && y >= y0 && y < y0+rh {
							want = byte((y-y0)*bpr + (x-x0)*tc.bpp + c + 1)
						}
						if g := got[(y*w+x)*tc.bpp+c]; g != want {
							t.Fatalf("(%d, %d)[%d]: got %d, want %d", x, y, c, g, want)
						}
					}
				}
			}
		})
	}
}

// Encoding a copy from a buffer, done for every upload, allocates nothing.
func TestCopyFromBufferDoesNotAllocate(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("CopyFromBuffer goes through objc.ID.Send on this architecture")
	}
	device, err := mtl.CreateSystemDefaultDevice()
	if err != nil {
		t.Skipf("no Metal device: %v", err)
	}
	texture := device.NewTextureWithDescriptor(mtl.TextureDescriptor{
		TextureType: mtl.TextureType2D,
		PixelFormat: mtl.PixelFormatRGBA8UNorm,
		Width:       64,
		Height:      64,
		StorageMode: mtl.StorageModePrivate,
		Usage:       mtl.TextureUsageShaderRead,
	})
	defer texture.Release()
	buf := device.NewBufferWithLength(4*64*64, mtl.ResourceStorageModeShared)
	defer buf.Release()
	cq := device.NewCommandQueue()
	defer cq.Release()
	cb := cq.CommandBuffer()
	bce := cb.BlitCommandEncoder()
	defer func() {
		bce.EndEncoding()
		cb.Commit()
		cb.WaitUntilCompleted()
	}()

	var n int
	if allocs := testing.AllocsPerRun(100, func() {
		n = (n + 1) % 64
		bce.CopyFromBuffer(buf, 0, 4*(n+1), 4*(n+1), mtl.Size{Width: n + 1, Height: 1, Depth: 1}, texture, 0, 0, mtl.Origin{X: 0, Y: n})
	}); allocs != 0 {
		t.Errorf("CopyFromBuffer allocates %v times per call, want 0", allocs)
	}
}
