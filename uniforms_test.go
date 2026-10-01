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

package ggfx_test

import (
	"image/color"
	"testing"

	"github.com/ironpark/ggfx"
)

const uniformsTestShader = `
struct Uniforms {
	scale: f32,
	offset: vec2f,
	m: mat3x3f,
	colors: array<vec4f, 2>,
	index: i32,
	count: u32,
	tint: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;

fn fragment(v: Vertex) -> vec4f {
	let c = u.m * vec3f(1.0, 1.0, 0.0);
	let base = u.colors[u.index] * u.scale + vec4f(u.offset, 0.0, 0.0) + vec4f(c, 0.0);
	return base * f32(u.count) / 2.0 * u.tint;
}
`

// TestShaderUniformBlock checks that a Uniforms block lays values out as the map does; the values
// are those of TestShaderUniforms.
func TestShaderUniformBlock(t *testing.T) {
	const w, h = 16, 16

	s, err := ggfx.NewShader([]byte(uniformsTestShader))
	if err != nil {
		t.Fatal(err)
	}

	u := s.NewUniforms()
	u.Set("scale", 0.5)
	u.SetSlice("offset", []float32{0.25, 0.125})
	u.SetSlice("m", []float32{0, 0, 0.5, 0, 0.125, 0, 0, 0, 0})
	u.SetSlice("colors", []float32{1, 1, 1, 1, 0.5, 0.25, 0, 1})
	u.Set("index", 1)
	u.Set("count", uint32(4))
	// An integer converts to a float uniform.
	u.SetSlice("tint", []int{1, 1, 1, 1})

	want := color.RGBA{R: 0xff, G: 0xbf, B: 0xff, A: 0xff}
	check := func(name string, dst *ggfx.Image) {
		t.Helper()
		for j := range h {
			for i := range w {
				got := dst.At(i, j).(color.RGBA)
				if !sameColors(got, want, 1) {
					t.Fatalf("%s: dst.At(%d, %d): got: %v, want: %v", name, i, j, got, want)
				}
			}
		}
	}

	dst := ggfx.NewImage(w, h)
	dst.DrawRectShader(w, h, s, &ggfx.DrawRectShaderOptions{UniformBlock: u})
	check("DrawRectShader", dst)

	dst = ggfx.NewImage(w, h)
	vs := []ggfx.Vertex{
		{DstX: 0, DstY: 0},
		{DstX: w, DstY: 0},
		{DstX: 0, DstY: h},
		{DstX: w, DstY: h},
	}
	dst.DrawTrianglesShader(vs, []uint32{0, 1, 2, 1, 2, 3}, s, &ggfx.DrawTrianglesShaderOptions{UniformBlock: u})
	check("DrawTrianglesShader", dst)
}

// TestShaderUniformBlockIsCopied checks that a draw copies the block, so that changing it after
// the draw does not change what the draw renders, and that values stay until they are set again.
func TestShaderUniformBlockIsCopied(t *testing.T) {
	const w, h = 4, 4

	s, err := ggfx.NewShader([]byte(`
struct Uniforms {
	tint: vec4f,
	on: u32,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f {
	return u.tint * f32(u.on);
}
`))
	if err != nil {
		t.Fatal(err)
	}

	u := s.NewUniforms()
	u.SetSlice("tint", []float32{1, 0, 0, 1})
	u.SetBool("on", true)
	dst0 := ggfx.NewImage(w, h)
	dst0.DrawRectShader(w, h, s, &ggfx.DrawRectShaderOptions{UniformBlock: u})

	u.SetSlice("tint", []float32{0, 1, 0, 1})
	dst1 := ggfx.NewImage(w, h)
	dst1.DrawRectShader(w, h, s, &ggfx.DrawRectShaderOptions{UniformBlock: u})

	u.Reset()
	dst2 := ggfx.NewImage(w, h)
	dst2.Fill(color.White)
	dst2.DrawRectShader(w, h, s, &ggfx.DrawRectShaderOptions{UniformBlock: u, Blend: ggfx.BlendCopy})

	if got, want := dst0.At(0, 0).(color.RGBA), (color.RGBA{R: 0xff, A: 0xff}); got != want {
		t.Errorf("dst0: got: %v, want: %v", got, want)
	}
	if got, want := dst1.At(0, 0).(color.RGBA), (color.RGBA{G: 0xff, A: 0xff}); got != want {
		t.Errorf("dst1: got: %v, want: %v", got, want)
	}
	if got, want := dst2.At(0, 0).(color.RGBA), (color.RGBA{}); got != want {
		t.Errorf("dst2 after Reset: got: %v, want: %v", got, want)
	}
}

func TestShaderUniformBlockPanics(t *testing.T) {
	src := `
struct Uniforms {
	tint: vec4f,
	index: i32,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f {
	return u.tint * f32(u.index);
}
`
	s, err := ggfx.NewShader([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	other, err := ggfx.NewShader([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	dst := ggfx.NewImage(4, 4)

	tests := []struct {
		name string
		f    func()
	}{
		{"unknown name", func() { s.NewUniforms().Set("unknown", 1) }},
		{"scalar for a vector", func() { s.NewUniforms().Set("tint", float32(1)) }},
		{"length mismatch", func() { s.NewUniforms().SetSlice("tint", []float32{1, 1, 1}) }},
		{"float for an integer", func() { s.NewUniforms().Set("index", 1.0) }},
		{"another shader's block", func() {
			dst.DrawRectShader(4, 4, s, &ggfx.DrawRectShaderOptions{UniformBlock: other.NewUniforms()})
		}},
		{"both Uniforms and UniformBlock", func() {
			dst.DrawRectShader(4, 4, s, &ggfx.DrawRectShaderOptions{
				Uniforms:     map[string]any{"index": 1},
				UniformBlock: s.NewUniforms(),
			})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%s must panic", tc.name)
				}
			}()
			tc.f()
		})
	}
}

func benchmarkUniforms(b *testing.B, block bool) {
	const w, h = 16, 16

	s, err := ggfx.NewShader([]byte(uniformsTestShader))
	if err != nil {
		b.Fatal(err)
	}
	dst := ggfx.NewImage(w, h)
	u := s.NewUniforms()
	offset := []float32{0.25, 0.125}
	m := []float32{0, 0, 0.5, 0, 0.125, 0, 0, 0, 0}
	colors := []float32{1, 1, 1, 1, 0.5, 0.25, 0, 1}
	tint := []float32{1, 1, 1, 1}

	b.ReportAllocs()
	for n := range b.N {
		scale := float32(n%2) * 0.5
		if block {
			u.Set("scale", scale)
			u.SetSlice("offset", offset)
			u.SetSlice("m", m)
			u.SetSlice("colors", colors)
			u.Set("index", 1)
			u.Set("count", uint32(4))
			u.SetSlice("tint", tint)
			dst.DrawRectShader(w, h, s, &ggfx.DrawRectShaderOptions{UniformBlock: u})
			continue
		}
		dst.DrawRectShader(w, h, s, &ggfx.DrawRectShaderOptions{Uniforms: map[string]any{
			"scale":  scale,
			"offset": offset,
			"m":      m,
			"colors": colors,
			"index":  1,
			"count":  uint32(4),
			"tint":   tint,
		}})
	}
}

func BenchmarkDrawRectShaderUniformsMap(b *testing.B) {
	benchmarkUniforms(b, false)
}

func BenchmarkDrawRectShaderUniformBlock(b *testing.B) {
	benchmarkUniforms(b, true)
}
