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

package graphicscommand_test

import (
	"image"
	"testing"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

type frameDriver struct {
	graphicsdriver.Graphics
	frames   int
	presents int
}

func (*frameDriver) Begin() error {
	return nil
}

func (g *frameDriver) End(mode graphicsdriver.FlushMode) error {
	if mode != graphicsdriver.FlushModeIntermediate {
		g.frames++
	}
	if mode == graphicsdriver.FlushModePresent {
		g.presents++
	}
	return nil
}

func TestCompleteFramesWithoutPresenting(t *testing.T) {
	var q graphicscommand.CommandQueueForTesting
	var driver frameDriver
	var finalized int
	const frames = 10
	for frame := range frames {
		q.AllocUniformsForTesting(16)
		q.AddFinalizerForTesting(func() { finalized++ })
		if err := q.FlushForTesting(&driver, graphicsdriver.FlushModeIntermediate); err != nil {
			t.Fatal(err)
		}
		if finalized != frame {
			t.Errorf("frame %d after intermediate flush: finalized = %d, want %d", frame, finalized, frame)
		}
		if err := q.FlushForTesting(&driver, graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
		if finalized != frame+1 {
			t.Errorf("frame %d after completion: finalized = %d, want %d", frame, finalized, frame+1)
		}
		if uniforms, finalizers := q.PendingResourcesForTesting(); uniforms != 0 || finalizers != 0 {
			t.Errorf("frame %d after completion: pending uniforms = %d, finalizers = %d; want 0, 0", frame, uniforms, finalizers)
		}
	}
	if driver.frames != frames || driver.presents != 0 {
		t.Errorf("after non-presented frames: frames = %d, presents = %d; want %d, 0", driver.frames, driver.presents, frames)
	}
	if err := q.FlushForTesting(&driver, graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}
	if driver.frames != frames+1 || driver.presents != 1 {
		t.Errorf("after presentation resumes: frames = %d, presents = %d; want %d, 1", driver.frames, driver.presents, frames+1)
	}
}

// A pooled command keeps neither the images it drew nor the room a large
// frame's regions took, or the pool would grow for as long as the app ran.
func TestPooledCommandsHoldNoFrame(t *testing.T) {
	if image, room := graphicscommand.PoolForTesting(8); image || room != 8 {
		t.Errorf("a small command: image kept %v, room for %d regions, want none and 8", image, room)
	}
	if image, room := graphicscommand.PoolForTesting(5000); image || room != 0 {
		t.Errorf("a large command: image kept %v, room for %d regions, want none and 0", image, room)
	}
}

// drawDriver takes vertices and draws, and does nothing with them.
type drawDriver struct {
	frameDriver
}

func (*drawDriver) SetVertices(vertices []float32, indices []uint32) error {
	return nil
}

func (*drawDriver) DrawTriangles(dst graphicsdriver.ImageID, srcs [graphics.ShaderSrcImageCount]graphicsdriver.ImageID, shader graphicsdriver.ShaderID, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32) error {
	return nil
}

// enqueueQuads enqueues n quads on dst, alternating the blend if merge is
// false so that no draw merges into the one before it.
func enqueueQuads(q *graphicscommand.CommandQueueForTesting, dst *graphicscommand.Image, shader *graphicscommand.Shader, n int, merge bool) {
	vs := quadVertices(16, 16)
	is := graphics.QuadIndices()
	dr := image.Rect(0, 0, 16, 16)
	for i := range n {
		blend := graphicsdriver.BlendSourceOver
		if !merge && i%2 == 1 {
			blend = graphicsdriver.BlendCopy
		}
		q.EnqueueDrawTrianglesCommand(dst, [graphics.ShaderSrcImageCount]*graphicscommand.Image{}, vs, is, blend, dr, [graphics.ShaderSrcImageCount]image.Rectangle{}, shader, nil)
	}
}

// A draw merged into the one before it gives back the uniforms it took.
func TestMergedDrawsGiveBackUniforms(t *testing.T) {
	var q graphicscommand.CommandQueueForTesting
	dst := graphicscommand.NewFakeImageForTesting(16, 16)
	shader := nearestFilterShader.FakeForTesting()

	enqueueQuads(&q, dst, shader, 100, true)
	if uniforms, _ := q.PendingResourcesForTesting(); uniforms != graphics.PreservedUniformDwordCount {
		t.Errorf("after 100 merged draws: uniforms = %d, want %d", uniforms, graphics.PreservedUniformDwordCount)
	}
	enqueueQuads(&q, dst, shader, 2, false)
	if uniforms, _ := q.PendingResourcesForTesting(); uniforms != 2*graphics.PreservedUniformDwordCount {
		t.Errorf("after a draw that does not merge: uniforms = %d, want %d", uniforms, 2*graphics.PreservedUniformDwordCount)
	}
}

// After a large frame, the queue gives back the room it took once frames are
// small again, but keeps its room while frames stay large.
func TestQueueShrinksAfterALargeFrame(t *testing.T) {
	var q graphicscommand.CommandQueueForTesting
	var driver drawDriver
	dst := graphicscommand.NewFakeImageForTesting(16, 16)
	shader := nearestFilterShader.FakeForTesting()

	frame := func(n int) {
		t.Helper()
		enqueueQuads(&q, dst, shader, n, false)
		if err := q.FlushForTesting(&driver, graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
	}

	// The uniforms of a frame may span several arrays until one holds a whole frame.
	const large = 20000
	frame(large)
	frame(large)
	v0, i0, u0 := q.BufferCapsForTesting()
	if v0 < large*4*graphics.VertexFloatCount || i0 < large*6 || u0 < large*graphics.PreservedUniformDwordCount {
		t.Fatalf("after a large frame: caps = %d, %d, %d; want room for the frame", v0, i0, u0)
	}
	for range 300 {
		frame(large)
	}
	if v, i, u := q.BufferCapsForTesting(); v != v0 || i != i0 || u != u0 {
		t.Errorf("after steady large frames: caps = %d, %d, %d; want %d, %d, %d", v, i, u, v0, i0, u0)
	}

	for range 300 {
		frame(1)
	}
	const limit = 2 * graphicscommand.MinRetainedElements
	if v, i, u := q.BufferCapsForTesting(); v > limit || i > limit || u > limit {
		t.Errorf("after small frames: caps = %d, %d, %d; want at most %d", v, i, u, limit)
	}
}
