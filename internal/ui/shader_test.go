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

package ui_test

import (
	"slices"
	"testing"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/ui"
)

const uniformsTestShader = `
struct Uniforms {
	scale: f32,
	offset: vec2f,
	m: mat3x3f,
	colors: array<vec4f, 2>,
	index: i32,
	count: u32,
	on: u32,
}
@group(1) @binding(0) var<uniform> u: Uniforms;

fn fragment(v: Vertex) -> vec4f {
	let c = u.m * vec3f(1.0, 1.0, 0.0);
	return u.colors[u.index] * u.scale + vec4f(u.offset, c.x, c.y) * f32(u.count + u.on);
}
`

func newTestShader(tb testing.TB) *ui.Shader {
	tb.Helper()
	program, err := graphics.CompileShader([]byte(uniformsTestShader))
	if err != nil {
		tb.Fatal(err)
	}
	return ui.NewShader(program, "")
}

var (
	testOffset = []float32{0.25, 0.125}
	testM      = []float32{0, 0, 0.5, 0, 0.125, 0, 0, 0, 0}
	testColors = []float32{1, 1, 1, 1, 0.5, 0.25, 0, 1}
)

func putTestUniforms(s *ui.Shader, block []uint32, scale float32) {
	s.PutUniform(block, "scale", scale)
	s.PutUniformSlice(block, "offset", testOffset)
	s.PutUniformSlice(block, "m", testM)
	s.PutUniformSlice(block, "colors", testColors)
	s.PutUniform(block, "index", 1)
	s.PutUniform(block, "count", uint32(4))
	s.PutUniformBool(block, "on", true)
}

func testUniformsMap(scale float32) map[string]any {
	return map[string]any{
		"scale":  scale,
		"offset": testOffset,
		"m":      testM,
		"colors": testColors,
		"index":  1,
		"count":  uint32(4),
		"on":     true,
	}
}

// TestPutUniformMatchesAppendUniforms checks that the setters lay values out as the map does.
func TestPutUniformMatchesAppendUniforms(t *testing.T) {
	s := newTestShader(t)
	want := s.AppendUniforms(nil, testUniformsMap(0.5))
	got := make([]uint32, s.UniformDwordCount())
	putTestUniforms(s, got, 0.5)
	if !slices.Equal(got, want) {
		t.Errorf("got: %v, want: %v", got, want)
	}

	// An integer given for a float uniform converts as the map converts it.
	want = s.AppendUniforms(nil, map[string]any{"scale": -3, "offset": []int{2, -1}})
	got = make([]uint32, s.UniformDwordCount())
	s.PutUniform(got, "scale", -3)
	s.PutUniformSlice(got, "offset", []int{2, -1})
	if !slices.Equal(got, want) {
		t.Errorf("integers: got: %v, want: %v", got, want)
	}
}

func BenchmarkAppendUniforms(b *testing.B) {
	s := newTestShader(b)
	var dst []uint32
	b.ReportAllocs()
	for n := range b.N {
		dst = s.AppendUniforms(dst[:0], testUniformsMap(float32(n%2)))
	}
}

func BenchmarkPutUniform(b *testing.B) {
	s := newTestShader(b)
	block := make([]uint32, s.UniformDwordCount())
	b.ReportAllocs()
	for n := range b.N {
		putTestUniforms(s, block, float32(n%2))
	}
}
