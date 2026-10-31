// Copyright 2024 The Ebitengine Authors
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

package graphicscommand

import (
	"image"

	"github.com/ironpark/ggfx/internal/debug"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

type WritePixelsCommandArgs = writePixelsCommandArgs

func (i *Image) BufferedWritePixelsArgsForTesting() []WritePixelsCommandArgs {
	return i.bufferedWritePixelsArgs
}

func PrependPreservedUniforms(uniforms []uint32, shader *Shader, dst *Image, srcs [graphics.ShaderSrcImageCount]*Image, dstRegion image.Rectangle, srcRegions [graphics.ShaderSrcImageCount]image.Rectangle) []uint32 {
	return prependPreservedUniforms(uniforms, shader, dst, srcs, dstRegion, srcRegions)
}

type CommandQueueForTesting = commandQueue

func (q *commandQueue) FlushForTesting(driver graphicsdriver.Graphics, mode graphicsdriver.FlushMode) error {
	return q.flush(driver, mode, debug.SwitchFrameLogger())
}

func (q *commandQueue) AddFinalizerForTesting(f func()) {
	q.addFinalizer(f)
}

func (q *commandQueue) AllocUniformsForTesting(n int) {
	q.uint32sBuffer.alloc(n)
}

func (q *commandQueue) PendingResourcesForTesting() (uniforms, finalizers int) {
	return q.uint32sBuffer.used, len(q.finalizers)
}

// PoolForTesting puts a command that drew an image over regions clip
// regions in a pool, and reports what the command the pool hands back
// still holds: an image, and room for how many regions.
func PoolForTesting(regions int) (image bool, room int) {
	var p drawTrianglesCommandPool
	p.put(&drawTrianglesCommand{dst: &Image{}, dstRegions: make([]graphicsdriver.DstRegion, regions)})
	c := p.get()
	return c.dst != nil, cap(c.dstRegions)
}

// MinRetainedElements is the room a reused slice of the queue may always keep.
const MinRetainedElements = minRetainedElements

// BufferCapsForTesting reports the room the queue keeps for vertices,
// indices and uniforms.
func (q *commandQueue) BufferCapsForTesting() (vertices, indices, uniforms int) {
	return cap(q.vertices), cap(q.indices), cap(q.uint32sBuffer.buf)
}

type fakeDriverImage struct{ graphicsdriver.Image }

func (fakeDriverImage) ID() graphicsdriver.ImageID { return 1 }

type fakeDriverShader struct{ graphicsdriver.Shader }

func (fakeDriverShader) ID() graphicsdriver.ShaderID { return 1 }

// NewFakeImageForTesting returns an image as if a driver had made it, for a
// queue flushed to a fake driver.
func NewFakeImageForTesting(width, height int) *Image {
	return &Image{image: fakeDriverImage{}, width: width, height: height}
}

// FakeForTesting returns a copy of s as if a driver had made it, for a
// queue flushed to a fake driver.
func (s *Shader) FakeForTesting() *Shader {
	return &Shader{shader: fakeDriverShader{}, ir: s.ir}
}
