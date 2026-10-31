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
	"testing"

	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/mtl"
)

// Setting the viewport and the scissor rectangle, done for every draw, allocates nothing.
func TestStructArgsDoNotAllocate(t *testing.T) {
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
		Usage:       mtl.TextureUsageRenderTarget,
	})
	defer texture.Release()
	cq := device.NewCommandQueue()
	cb := cq.CommandBuffer()

	var rpd mtl.RenderPassDescriptor
	rpd.ColorAttachments[0].LoadAction = mtl.LoadActionDontCare
	rpd.ColorAttachments[0].StoreAction = mtl.StoreActionStore
	rpd.ColorAttachments[0].Texture = texture
	rce := cb.RenderCommandEncoderWithDescriptor(rpd)
	defer func() {
		rce.EndEncoding()
		cb.Commit()
		cb.WaitUntilCompleted()
	}()

	var n int
	if allocs := testing.AllocsPerRun(100, func() {
		n = (n + 1) % 64
		rce.SetViewport(mtl.Viewport{Width: float64(n + 1), Height: 64, ZNear: -1, ZFar: 1})
		rce.SetScissorRect(mtl.ScissorRect{X: 0, Y: 0, Width: n + 1, Height: 64})
	}); allocs != 0 {
		t.Errorf("SetViewport and SetScissorRect allocate %v times per call, want 0", allocs)
	}
}
