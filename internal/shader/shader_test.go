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

package shader_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/gogpu/naga/ir"

	"github.com/ironpark/ggfx/internal/shader"
)

func TestCompile(t *testing.T) {
	p, err := shader.Compile([]byte(`
fn fragment(v: Vertex) -> vec4f {
	return src0_at(v.src_pos) * v.color + src3_at_from_src0(v.src_pos) * f32(front_facing());
}
`), 4)
	if err != nil {
		t.Fatal(err)
	}
	if p.UniformDwordCount != 0 || len(p.Uniforms) != 0 {
		t.Errorf("a shader without uniforms must have no uniform block: got %d dwords, %d uniforms", p.UniformDwordCount, len(p.Uniforms))
	}
	var names []string
	for _, ep := range p.Module.EntryPoints {
		names = append(names, ep.Name)
	}
	slices.Sort(names)
	if want := []string{shader.FragmentEntryPoint, shader.VertexEntryPoint}; !slices.Equal(names, want) {
		t.Errorf("entry points: got %v, want %v", names, want)
	}
}

func TestCompileErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "no fragment",
			src:  `fn other(v: Vertex) -> vec4f { return v.color; }`,
			want: "fragment",
		},
		{
			name: "syntax error on line 2",
			src:  "\nfn fragment(v: Vertex) -> vec4f { return v.color }",
			want: "line 2",
		},
		{
			name: "unknown member on line 3",
			src:  "\n\nfn fragment(v: Vertex) -> vec4f { return v.colour; }",
			want: "3:",
		},
		{
			name: "two uniform blocks",
			src: `
@group(1) @binding(0) var<uniform> a: vec4f;
@group(1) @binding(1) var<uniform> b: vec4f;
fn fragment(v: Vertex) -> vec4f { return a + b; }`,
			want: "@group(1) @binding(0)",
		},
		{
			name: "storage buffer",
			src: `
@group(1) @binding(0) var<storage> a: array<vec4f>;
fn fragment(v: Vertex) -> vec4f { return a[0]; }`,
			want: "storage",
		},
		{
			name: "own entry point",
			src: `
@fragment fn fragment(@location(0) p: vec2f) -> @location(0) vec4f { return vec4f(p, 0.0, 1.0); }`,
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := shader.Compile([]byte(tc.src), 4)
			if err == nil {
				t.Fatal("Compile must fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q must mention %q", err, tc.want)
			}
		})
	}
}

func TestUniformLayout(t *testing.T) {
	p, err := shader.Compile([]byte(`
struct Uniforms {
	center: vec2f,
	radius: f32,
	tint: vec4f,
	m: mat3x3f,
	arr: array<vec4f, 3>,
	flag: i32,
	m2: mat2x2f,
	count: u32,
	v3: vec3f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f {
	return u.tint * u.radius + vec4f(u.center, 0.0, 0.0) + vec4f(u.m[0], 0.0) + u.arr[u.flag] + vec4f(u.m2[1], 0.0, 0.0) * f32(u.count) + vec4f(u.v3, 0.0);
}
`), 4)
	if err != nil {
		t.Fatal(err)
	}
	// The WGSL uniform layout: mat3x3 columns are 16 bytes apart, arrays of vec4 are 16 bytes per
	// element, mat2x2 columns are 8 bytes apart, vec3 is 16-byte aligned, and the whole block is
	// padded to 16 bytes: 176 bytes.
	if got, want := p.UniformDwordCount, 44; got != want {
		t.Errorf("UniformDwordCount: got %d, want %d", got, want)
	}
	want := []shader.Uniform{
		{Name: "center", Slots: []int{0, 1}, Kinds: []ir.ScalarKind{ir.ScalarFloat, ir.ScalarFloat}},
		{Name: "radius", Slots: []int{2}, Kinds: []ir.ScalarKind{ir.ScalarFloat}},
		{Name: "tint", Slots: []int{4, 5, 6, 7}, Kinds: []ir.ScalarKind{ir.ScalarFloat, ir.ScalarFloat, ir.ScalarFloat, ir.ScalarFloat}},
		{Name: "m", Slots: []int{8, 9, 10, 12, 13, 14, 16, 17, 18}, Kinds: slices.Repeat([]ir.ScalarKind{ir.ScalarFloat}, 9)},
		{Name: "arr", Slots: []int{20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}, Kinds: slices.Repeat([]ir.ScalarKind{ir.ScalarFloat}, 12)},
		{Name: "flag", Slots: []int{32}, Kinds: []ir.ScalarKind{ir.ScalarSint}},
		{Name: "m2", Slots: []int{34, 35, 36, 37}, Kinds: slices.Repeat([]ir.ScalarKind{ir.ScalarFloat}, 4)},
		{Name: "count", Slots: []int{38}, Kinds: []ir.ScalarKind{ir.ScalarUint}},
		{Name: "v3", Slots: []int{40, 41, 42}, Kinds: slices.Repeat([]ir.ScalarKind{ir.ScalarFloat}, 3)},
	}
	if len(p.Uniforms) != len(want) {
		t.Fatalf("Uniforms: got %d, want %d", len(p.Uniforms), len(want))
	}
	for i, u := range p.Uniforms {
		if u.Name != want[i].Name || !slices.Equal(u.Slots, want[i].Slots) || !slices.Equal(u.Kinds, want[i].Kinds) {
			t.Errorf("Uniforms[%d]: got %+v, want %+v", i, u, want[i])
		}
	}
}

func TestNonStructUniform(t *testing.T) {
	p, err := shader.Compile([]byte(`
@group(1) @binding(0) var<uniform> tint: vec4f;
fn fragment(v: Vertex) -> vec4f { return tint; }
`), 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Uniforms) != 1 || p.Uniforms[0].Name != "tint" || !slices.Equal(p.Uniforms[0].Slots, []int{0, 1, 2, 3}) {
		t.Errorf("Uniforms: got %+v", p.Uniforms)
	}
	if p.UniformDwordCount != 4 {
		t.Errorf("UniformDwordCount: got %d, want 4", p.UniformDwordCount)
	}
}

func TestPreludeTextureCount(t *testing.T) {
	if _, err := shader.Compile([]byte(`fn fragment(v: Vertex) -> vec4f { return src1_at(v.src_pos); }`), 2); err != nil {
		t.Errorf("src1_at must exist with 2 textures: %v", err)
	}
	if _, err := shader.Compile([]byte(`fn fragment(v: Vertex) -> vec4f { return src2_at(v.src_pos); }`), 2); err == nil {
		t.Error("src2_at must not exist with 2 textures")
	}
}
