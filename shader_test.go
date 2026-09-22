// Copyright 2020 The Ebiten Authors
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

func TestShaderFill(t *testing.T) {
	const w, h = 16, 16

	dst := ggfx.NewImage(w, h)
	s, err := ggfx.NewShader([]byte(`
fn fragment(v: Vertex) -> vec4f {
	return vec4f(1.0, 0.0, 0.0, 1.0);
}
`))
	if err != nil {
		t.Fatal(err)
	}

	dst.DrawRectShader(w/2, h/2, s, nil)

	for j := range h {
		for i := range w {
			got := dst.At(i, j).(color.RGBA)
			var want color.RGBA
			if i < w/2 && j < h/2 {
				want = color.RGBA{R: 0xff, A: 0xff}
			}
			if got != want {
				t.Errorf("dst.At(%d, %d): got: %v, want: %v", i, j, got, want)
			}
		}
	}
}

func TestShaderCompileError(t *testing.T) {
	if _, err := ggfx.NewShader([]byte(`
fn fragment(v: Vertex) -> vec4f {
	return v.color
}
`)); err == nil {
		t.Error("NewShader must fail on a syntax error")
	}
	if _, err := ggfx.NewShader([]byte(`fn other(v: Vertex) -> vec4f { return v.color; }`)); err == nil {
		t.Error("NewShader must fail without a fragment function")
	}
}

// TestShaderUniforms checks that every uniform type reaches the shader with the layout the
// shader expects: scalars, vectors, mat3x3 (whose columns are padded), arrays, and integers.
func TestShaderUniforms(t *testing.T) {
	const w, h = 16, 16

	dst := ggfx.NewImage(w, h)
	s, err := ggfx.NewShader([]byte(`
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
	// m maps (1, 0, 0) to its first column and (0, 1, 0) to its second column.
	let c = u.m * vec3f(1.0, 1.0, 0.0);
	let base = u.colors[u.index] * u.scale + vec4f(u.offset, 0.0, 0.0) + vec4f(c, 0.0);
	return base * f32(u.count) / 2.0 * u.tint;
}
`))
	if err != nil {
		t.Fatal(err)
	}

	op := &ggfx.DrawRectShaderOptions{}
	op.Uniforms = map[string]any{
		"scale":  0.5,
		"offset": []float32{0.25, 0.125},
		// Column-major: the first column is (0, 0, 0.5), the second (0, 0.125, 0).
		"m":      []float32{0, 0, 0.5, 0, 0.125, 0, 0, 0, 0},
		"colors": []float32{1, 1, 1, 1, 0.5, 0.25, 0, 1},
		"index":  1,
		"count":  uint32(4),
		"tint":   []float32{1, 1, 1, 1},
	}
	dst.DrawRectShader(w, h, s, op)

	// colors[1] * 0.5 = (0.25, 0.125, 0, 0.5); + offset = (0.5, 0.25, 0, 0.5); + c = (0.5, 0.375, 0.5, 0.5); * 2 = (1, 0.75, 1, 1).
	want := color.RGBA{R: 0xff, G: 0xbf, B: 0xff, A: 0xff}
	for j := range h {
		for i := range w {
			got := dst.At(i, j).(color.RGBA)
			if !sameColors(got, want, 1) {
				t.Fatalf("dst.At(%d, %d): got: %v, want: %v", i, j, got, want)
			}
		}
	}
}

func TestShaderUnusedUniformIsIgnored(t *testing.T) {
	const w, h = 4, 4

	dst := ggfx.NewImage(w, h)
	s, err := ggfx.NewShader([]byte(`
@group(1) @binding(0) var<uniform> tint: vec4f;
fn fragment(v: Vertex) -> vec4f {
	return tint;
}
`))
	if err != nil {
		t.Fatal(err)
	}

	op := &ggfx.DrawRectShaderOptions{}
	op.Uniforms = map[string]any{
		"tint":    []float32{0, 1, 0, 1},
		"unknown": 1.0,
	}
	dst.DrawRectShader(w, h, s, op)
	if got, want := dst.At(0, 0).(color.RGBA), (color.RGBA{G: 0xff, A: 0xff}); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestShaderUniformLengthMismatchPanics(t *testing.T) {
	const w, h = 4, 4

	dst := ggfx.NewImage(w, h)
	s, err := ggfx.NewShader([]byte(`
@group(1) @binding(0) var<uniform> tint: vec4f;
fn fragment(v: Vertex) -> vec4f {
	return tint;
}
`))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("DrawRectShader must panic on a uniform length mismatch")
		}
	}()
	op := &ggfx.DrawRectShaderOptions{}
	op.Uniforms = map[string]any{
		"tint": []float32{0, 1, 0},
	}
	dst.DrawRectShader(w, h, s, op)
}

func TestShaderSources(t *testing.T) {
	const w, h = 8, 8

	src0 := ggfx.NewImage(w, h)
	src0.Fill(color.RGBA{R: 0x40, A: 0xff})
	src1 := ggfx.NewImage(w, h)
	src1.Fill(color.RGBA{G: 0x80, A: 0xff})

	dst := ggfx.NewImage(w, h)
	s, err := ggfx.NewShader([]byte(`
fn fragment(v: Vertex) -> vec4f {
	let a = src0_at(v.src_pos);
	let b = src1_at_from_src0(v.src_pos);
	// The regions are given in pixels, so the size is the image size.
	let size = src0_size();
	if (size.x != 8.0 || size.y != 8.0 || dst_size().x != 8.0) {
		return vec4f(1.0, 1.0, 1.0, 1.0);
	}
	return vec4f(a.r, b.g, 0.0, 1.0);
}
`))
	if err != nil {
		t.Fatal(err)
	}

	op := &ggfx.DrawRectShaderOptions{}
	op.Images[0] = src0
	op.Images[1] = src1
	dst.DrawRectShader(w, h, s, op)

	want := color.RGBA{R: 0x40, G: 0x80, A: 0xff}
	for j := range h {
		for i := range w {
			if got := dst.At(i, j).(color.RGBA); !sameColors(got, want, 1) {
				t.Fatalf("dst.At(%d, %d): got: %v, want: %v", i, j, got, want)
			}
		}
	}
}

func TestShaderFrontFacing(t *testing.T) {
	const w, h = 8, 8

	dst := ggfx.NewImage(w, h)
	s, err := ggfx.NewShader([]byte(`
fn fragment(v: Vertex) -> vec4f {
	if (front_facing()) {
		return vec4f(1.0, 0.0, 0.0, 1.0);
	}
	return vec4f(0.0, 0.0, 1.0, 1.0);
}
`))
	if err != nil {
		t.Fatal(err)
	}

	// Clockwise in the destination's coordinates (Y down) is front-facing, as in the vector package.
	vs := []ggfx.Vertex{
		{DstX: 0, DstY: 0},
		{DstX: w, DstY: 0},
		{DstX: 0, DstY: h},
		{DstX: w, DstY: h},
	}
	dst.DrawTrianglesShader(vs, []uint16{0, 1, 2, 1, 3, 2}, s, nil)
	if got, want := dst.At(1, 1).(color.RGBA), (color.RGBA{R: 0xff, A: 0xff}); got != want {
		t.Errorf("clockwise: got: %v, want: %v", got, want)
	}

	dst.Clear()
	dst.DrawTrianglesShader(vs, []uint16{0, 2, 1, 1, 2, 3}, s, nil)
	if got, want := dst.At(1, 1).(color.RGBA), (color.RGBA{B: 0xff, A: 0xff}); got != want {
		t.Errorf("counterclockwise: got: %v, want: %v", got, want)
	}
}
