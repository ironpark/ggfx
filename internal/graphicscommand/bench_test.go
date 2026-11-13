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

package graphicscommand_test

import (
	"image"
	"testing"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/render"
	etesting "github.com/ironpark/ggfx/internal/testing"
)

func BenchmarkPrependPreservedUniforms(b *testing.B) {
	var uniforms [graphics.PreservedUniformDwordCount]uint32
	dst := graphicscommand.NewImage(16, 16, nil, "")
	src := graphicscommand.NewImage(16, 16, nil, "")
	dr := image.Rect(0, 0, 16, 16)
	sr := image.Rect(0, 0, 16, 16)
	for i := 0; i < b.N; i++ {
		graphicscommand.PrependPreservedUniforms(uniforms[:], dst, [graphics.ShaderSrcImageCount]*graphicscommand.Image{src}, dr, [graphics.ShaderSrcImageCount]image.Rectangle{sr})
	}
}

// BenchmarkFlushUnmergedDraws enqueues and flushes draws that do not merge, as their shaders
// and sources alternate, so that the driver's work for each draw is timed with the flush.
func BenchmarkFlushUnmergedDraws(b *testing.B) {
	const draws = 300
	driver := render.GraphicsDriver()
	dst := graphicscommand.NewImage(256, 256, nil, "")
	src := graphicscommand.NewImage(16, 16, nil, "")
	fill := graphicscommand.NewShader(etesting.ShaderProgramFill(0xff, 0, 0, 0xff), "")
	vs := make([]float32, 4*graphics.VertexFloatCount)
	is := graphics.QuadIndices()
	sr := image.Rect(0, 0, 16, 16)
	flush := func() {
		graphicscommand.EnqueueSyncForTesting()
		if err := graphicscommand.FlushCommands(driver, graphicsdriver.FlushModeEndFrame); err != nil {
			b.Fatal(err)
		}
	}
	flush()

	b.ResetTimer()
	for range b.N {
		for i := range draws {
			x, y := float32(i%16*16), float32(i/16%16*16)
			graphics.QuadVerticesFromDstAndSrc(vs, x, y, x+16, y+16, 0, 0, 16, 16, 1, 1, 1, 1)
			dr := image.Rect(int(x), int(y), int(x)+16, int(y)+16)
			if i%2 == 0 {
				dst.DrawTriangles([graphics.ShaderSrcImageCount]*graphicscommand.Image{src}, vs, is, graphicsdriver.BlendSourceOver, dr, [graphics.ShaderSrcImageCount]image.Rectangle{sr}, nearestFilterShader, nil)
			} else {
				dst.DrawTriangles([graphics.ShaderSrcImageCount]*graphicscommand.Image{}, vs, is, graphicsdriver.BlendSourceOver, dr, [graphics.ShaderSrcImageCount]image.Rectangle{}, fill, nil)
			}
		}
		flush()
	}
}
