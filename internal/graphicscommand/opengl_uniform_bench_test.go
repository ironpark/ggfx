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

package graphicscommand_test

import (
	"image"
	"math"
	"testing"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/render"
	"github.com/ironpark/ggfx/internal/shader"
)

// BenchmarkFlushDistinctUserUniforms enqueues and flushes 300 draws that each carry a distinct
// 16-byte user uniform block, exercising per-draw uniform buffer uploads.
func BenchmarkFlushDistinctUserUniforms(b *testing.B) {
	const draws = 300
	driver := render.GraphicsDriver()
	dst := graphicscommand.NewImage(256, 256, nil, "")
	ir, err := shader.CompileUser([]byte(`
struct Uniforms {
	color: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;

fn fragment(v: Vertex) -> vec4f {
	return u.color;
}
`))
	if err != nil {
		b.Fatal(err)
	}
	sh := graphicscommand.NewShader(ir, "")
	vs := make([]float32, 4*graphics.VertexFloatCount)
	is := graphics.QuadIndices()

	var uniforms [draws][4]uint32
	for i := range draws {
		uniforms[i] = [4]uint32{
			math.Float32bits(float32(i+1) / float32(draws)),
			math.Float32bits(0.5),
			math.Float32bits(0.25),
			math.Float32bits(1.0),
		}
	}

	flush := func() {
		graphicscommand.EnqueueSyncForTesting()
		if err := graphicscommand.FlushCommands(driver, graphicsdriver.FlushModeEndFrame); err != nil {
			b.Fatal(err)
		}
	}
	b.Cleanup(func() {
		sh.Dispose()
		dst.Dispose()
		flush()
	})
	flush()

	b.ResetTimer()
	for range b.N {
		for i := range draws {
			x, y := float32(i%16*16), float32(i/16%16*16)
			graphics.QuadVerticesFromDstAndSrc(vs, x, y, x+16, y+16, 0, 0, 16, 16, 1, 1, 1, 1)
			dr := image.Rect(int(x), int(y), int(x)+16, int(y)+16)
			dst.DrawTriangles([graphics.ShaderSrcImageCount]*graphicscommand.Image{}, vs, is, graphicsdriver.BlendSourceOver, dr, [graphics.ShaderSrcImageCount]image.Rectangle{}, sh, uniforms[i][:])
		}
		flush()
	}
}
