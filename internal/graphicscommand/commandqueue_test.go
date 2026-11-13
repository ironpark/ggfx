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
	"math"
	"slices"
	"testing"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
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
	const frames = 10
	for frame := range frames {
		q.AllocUniformsForTesting(16)
		q.AddUploadedBytesForTesting(make([]byte, 16))
		if err := q.FlushForTesting(&driver, graphicsdriver.FlushModeIntermediate); err != nil {
			t.Fatal(err)
		}
		// An intermediate flush keeps the frame's resources.
		if uniforms, uploaded := q.PendingResourcesForTesting(); uniforms == 0 || uploaded != 1 {
			t.Errorf("frame %d after intermediate flush: pending uniforms = %d, uploaded bytes = %d; want > 0, 1", frame, uniforms, uploaded)
		}
		if err := q.FlushForTesting(&driver, graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
		if uniforms, uploaded := q.PendingResourcesForTesting(); uniforms != 0 || uploaded != 0 {
			t.Errorf("frame %d after completion: pending uniforms = %d, uploaded bytes = %d; want 0, 0", frame, uniforms, uploaded)
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

func (*drawDriver) FlipsY() bool {
	return false
}

func (*drawDriver) DrawTriangles(dst graphicsdriver.Image, srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image, shader graphicsdriver.Shader, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32, versions graphicsdriver.UniformVersions) error {
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
		// End the frame, as render.Flush does.
		recent.Advance()
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

// projectionDriver is a drawDriver whose offscreen render targets have their rows go down if
// flipsY is set, and which keeps the projection of the last draw.
type projectionDriver struct {
	drawDriver
	flipsY     bool
	projection []uint32
}

func (g *projectionDriver) FlipsY() bool {
	return g.flipsY
}

func (g *projectionDriver) DrawTriangles(dst graphicsdriver.Image, srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image, shader graphicsdriver.Shader, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32, versions graphicsdriver.UniformVersions) error {
	g.projection = slices.Clone(uniforms[graphics.ProjectionMatrixUniformDwordIndex : graphics.ProjectionMatrixUniformDwordIndex+16])
	return nil
}

// The projection a driver receives maps the destination's top row to clip-space Y = -1, or to
// Y = 1 where its render target has its rows go down, and its bottom to the other end. A screen
// image takes its surface's answer, not the driver's.
func TestProjectionFlipsY(t *testing.T) {
	const w, h = 16, 32
	shader := nearestFilterShader.FakeForTesting()
	for _, tc := range []struct {
		name                      string
		dst                       *graphicscommand.Image
		driverFlipsY, wantFlipped bool
	}{
		{"offscreen", graphicscommand.NewFakeImageForTesting(w, h), false, false},
		{"offscreen, flipping driver", graphicscommand.NewFakeImageForTesting(w, h), true, true},
		{"flipping screen", graphicscommand.NewFakeScreenImageForTesting(w, h, true), false, true},
		{"screen, flipping driver", graphicscommand.NewFakeScreenImageForTesting(w, h, false), true, false},
	} {
		var q graphicscommand.CommandQueueForTesting
		driver := projectionDriver{flipsY: tc.driverFlipsY}
		q.EnqueueDrawTrianglesCommand(tc.dst, [graphics.ShaderSrcImageCount]*graphicscommand.Image{}, quadVertices(w, h), graphics.QuadIndices(), graphicsdriver.BlendSourceOver, image.Rect(0, 0, w, h), [graphics.ShaderSrcImageCount]image.Rectangle{}, shader, nil)
		if err := q.FlushForTesting(&driver, graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}

		p := driver.projection
		// The exact values the drivers have always received.
		sy, ty := 2/float32(h), float32(-1)
		if tc.wantFlipped {
			sy, ty = -sy, 1
		}
		if got := math.Float32frombits(p[5]); got != sy {
			t.Errorf("%s: projection[5] = %v, want %v", tc.name, got, sy)
		}
		if got := math.Float32frombits(p[13]); got != ty {
			t.Errorf("%s: projection[13] = %v, want %v", tc.name, got, ty)
		}
		clipY := func(y float32) float32 {
			return math.Float32frombits(p[5])*y + math.Float32frombits(p[13])
		}
		top, bottom := float32(-1), float32(1)
		if tc.wantFlipped {
			top, bottom = bottom, top
		}
		if got := clipY(0); got != top {
			t.Errorf("%s: the top row's clip-space Y = %v, want %v", tc.name, got, top)
		}
		if got := clipY(h); got != bottom {
			t.Errorf("%s: the bottom row's clip-space Y = %v, want %v", tc.name, got, bottom)
		}
	}
}

// versionDriver is a drawDriver that keeps the uniform blocks and their versions of every draw.
type versionDriver struct {
	drawDriver
	draws []versionedDraw
}

type versionedDraw struct {
	versions       graphicsdriver.UniformVersions
	internal, user []uint32
}

func (g *versionDriver) DrawTriangles(dst graphicsdriver.Image, srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image, shader graphicsdriver.Shader, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32, versions graphicsdriver.UniformVersions) error {
	g.draws = append(g.draws, versionedDraw{
		versions: versions,
		internal: slices.Clone(uniforms[:graphics.PreservedUniformDwordCount]),
		user:     slices.Clone(uniforms[graphics.PreservedUniformDwordCount:]),
	})
	return nil
}

// Blocks of one version are equal as the driver gets them, flipped or not, over flushes, and a
// draw that does not merge keeps the versions of the draw before when its blocks are equal.
func TestUniformVersions(t *testing.T) {
	const w, h = 16, 16
	shader := nearestFilterShader.FakeForTesting()
	offscreen := graphicscommand.NewFakeImageForTesting(w, h)
	screen := graphicscommand.NewFakeScreenImageForTesting(w, h, true)
	var q graphicscommand.CommandQueueForTesting
	var driver versionDriver
	draw := func(dst *graphicscommand.Image, blend graphicsdriver.Blend, user ...uint32) {
		q.EnqueueDrawTrianglesCommand(dst, [graphics.ShaderSrcImageCount]*graphicscommand.Image{}, quadVertices(w, h), graphics.QuadIndices(), blend, image.Rect(0, 0, w, h), [graphics.ShaderSrcImageCount]image.Rectangle{}, shader, user)
	}
	for range 2 {
		draw(offscreen, graphicsdriver.BlendSourceOver, 1, 2)
		draw(offscreen, graphicsdriver.BlendCopy, 1, 2)       // the same blocks
		draw(screen, graphicsdriver.BlendCopy, 1, 2)          // flipped
		draw(screen, graphicsdriver.BlendSourceOver, 3, 4)    // flipped again, another user block
		draw(offscreen, graphicsdriver.BlendSourceOver, 3, 4) // not flipped
		if err := q.FlushForTesting(&driver, graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
	}

	d := driver.draws
	if len(d) != 10 {
		t.Fatalf("draws: %d, want 10", len(d))
	}
	for i := range d {
		for j := range i {
			if d[i].versions.Internal == d[j].versions.Internal && !slices.Equal(d[i].internal, d[j].internal) {
				t.Errorf("draws %d and %d: internal version %d for different blocks", j, i, d[i].versions.Internal)
			}
			if d[i].versions.User == d[j].versions.User && !slices.Equal(d[i].user, d[j].user) {
				t.Errorf("draws %d and %d: user version %d for different blocks", j, i, d[i].versions.User)
			}
		}
		if d[i].versions.Internal == 0 || d[i].versions.User == 0 {
			t.Errorf("draw %d: versions %+v, want nonzero", i, d[i].versions)
		}
	}
	for _, i := range []int{1, 6} {
		if d[i].versions != d[i-1].versions {
			t.Errorf("draw %d: versions %+v, want those of the draw before, %+v", i, d[i].versions, d[i-1].versions)
		}
	}
	for _, i := range []int{2, 7} {
		if d[i].versions.User != d[i-1].versions.User {
			t.Errorf("draw %d: user version %d, want that of the draw before, %d", i, d[i].versions.User, d[i-1].versions.User)
		}
	}
	for _, i := range []int{3, 8} {
		if d[i].versions.Internal != d[i-1].versions.Internal {
			t.Errorf("draw %d: internal version %d, want that of the draw before, %d", i, d[i].versions.Internal, d[i-1].versions.Internal)
		}
	}
	// A new flush does not take the versions of the last.
	if d[5].versions.Internal <= d[4].versions.Internal || d[5].versions.User <= d[4].versions.User {
		t.Errorf("first draw of the second flush: versions %+v, want newer than %+v", d[5].versions, d[4].versions)
	}
}
