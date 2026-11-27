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
	"fmt"
	"runtime"
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
			want: "line 3",
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

func TestUniformBlockLimits(t *testing.T) {
	// Exactly at the untrusted 16 KiB limit (1024 vec4f) must compile under CompileUntrusted.
	exactArr := fmt.Sprintf(`
@group(1) @binding(0) var<uniform> entries: array<vec4f, %d>;
fn fragment(v: Vertex) -> vec4f { return entries[0]; }
`, shader.MaxUntrustedUserUniformBytes/16)
	p, err := shader.CompileUntrusted([]byte(exactArr), 4)
	if err != nil {
		t.Fatalf("exact-limit array failed CompileUntrusted: %v", err)
	}
	if got, want := p.UniformDwordCount, shader.MaxUntrustedUserUniformDwords; got != want {
		t.Errorf("exact-limit UniformDwordCount = %d, want %d", got, want)
	}
	if len(p.Uniforms) != 1 || len(p.Uniforms[0].Slots) != shader.MaxUntrustedUserUniformDwords {
		t.Errorf("exact-limit slots = %d, want %d", len(p.Uniforms[0].Slots), shader.MaxUntrustedUserUniformDwords)
	}

	exactStruct := fmt.Sprintf(`
struct Uniforms {
	a: array<vec4f, %d>,
	b: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f { return u.a[0] + u.b; }
`, shader.MaxUntrustedUserUniformBytes/16-1)
	p, err = shader.CompileUntrusted([]byte(exactStruct), 4)
	if err != nil {
		t.Fatalf("exact-limit struct failed CompileUntrusted: %v", err)
	}
	if got, want := p.UniformDwordCount, shader.MaxUntrustedUserUniformDwords; got != want {
		t.Errorf("exact-limit struct UniformDwordCount = %d, want %d", got, want)
	}

	// HEAD-accepted local case (16,400 bytes = 1025 vec4f): compiles locally via Compile,
	// rejected by CompileUntrusted.
	localOver16K := fmt.Sprintf(`
@group(1) @binding(0) var<uniform> entries: array<vec4f, %d>;
fn fragment(v: Vertex) -> vec4f { return entries[0]; }
`, shader.MaxUntrustedUserUniformBytes/16+1)
	if _, err := shader.Compile([]byte(localOver16K), 4); err != nil {
		t.Fatalf("16,400-byte uniform block must compile locally: %v", err)
	}
	if _, err := shader.CompileUntrusted([]byte(localOver16K), 4); err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("16,400-byte uniform block under CompileUntrusted = %v, want maximum size error", err)
	}

	// Build a deeply nested array of 40 levels of array<..., 1>.
	var nested strings.Builder
	for range 40 {
		nested.WriteString("array<")
	}
	nested.WriteString("vec4f")
	for range 40 {
		nested.WriteString(", 1>")
	}
	deepNestedSrc := fmt.Sprintf(`
@group(1) @binding(0) var<uniform> u: %s;
fn fragment(v: Vertex) -> vec4f { return vec4f(1.0); }
`, nested.String())

	// Build an exponential struct DAG (2^24 leaves) to verify O(|Types|) reflection.
	var dag strings.Builder
	dag.WriteString("struct S0 { a: vec4f, b: vec4f }\n")
	for i := 1; i <= 24; i++ {
		fmt.Fprintf(&dag, "struct S%d { a: S%d, b: S%d }\n", i, i-1, i-1)
	}
	dag.WriteString("@group(1) @binding(0) var<uniform> u: S24;\n")
	dag.WriteString("fn fragment(v: Vertex) -> vec4f { return vec4f(1.0); }\n")

	for _, tc := range []struct {
		name      string
		src       string
		want      string
		untrusted bool
	}{
		{
			name:      "one vec4 over untrusted limit",
			src:       localOver16K,
			want:      "maximum size",
			untrusted: true,
		},
		{
			name: "struct over untrusted limit",
			src: fmt.Sprintf(`
struct Uniforms {
	a: array<vec4f, %d>,
	b: f32,
}
@group(1) @binding(0) var<uniform> u: Uniforms;
fn fragment(v: Vertex) -> vec4f { return u.a[0] * u.b; }`, shader.MaxUntrustedUserUniformBytes/16),
			want:      "maximum size",
			untrusted: true,
		},
		{
			name: "one vec4 over local limit",
			src: fmt.Sprintf(`
@group(1) @binding(0) var<uniform> entries: array<vec4f, %d>;
fn fragment(v: Vertex) -> vec4f { return entries[0]; }`, shader.MaxUserUniformBytes/16+1),
			want: "maximum size",
		},
		{
			name: "huge 1D array",
			src: `
@group(1) @binding(0) var<uniform> entries: array<vec4f, 100000000>;
fn fragment(v: Vertex) -> vec4f { return entries[0]; }`,
			want: "maximum size",
		},
		{
			name: "huge nested array",
			src: `
@group(1) @binding(0) var<uniform> entries: array<array<vec4f, 10000>, 10000>;
fn fragment(v: Vertex) -> vec4f { return entries[0][0]; }`,
			want: "maximum size",
		},
		{
			name: "deeply nested single-element array",
			src:  deepNestedSrc,
			want: "maximum depth",
		},
		{
			name: "exponential struct DAG",
			src:  dag.String(),
			want: "maximum size",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			var err error
			if tc.untrusted {
				_, err = shader.CompileUntrusted([]byte(tc.src), 4)
			} else {
				_, err = shader.Compile([]byte(tc.src), 4)
			}
			runtime.ReadMemStats(&after)
			if err == nil {
				t.Fatal("Compile must fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q must mention %q", err, tc.want)
			}
			// Must reject without allocating proportional to the declared array size.
			if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 8<<20 {
				t.Errorf("Compile allocated %d bytes rejecting oversized uniform block, want <= 8 MiB", alloc)
			}
		})
	}
}

func TestDisallowedShaderGlobals(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "extra texture at group 1",
			src: `
@group(1) @binding(1) var extra_tex: texture_2d<f32>;
fn fragment(v: Vertex) -> vec4f { return textureLoad(extra_tex, vec2i(0, 0), 0); }`,
			want: "disallowed binding",
		},
		{
			name: "extra texture past TextureCount",
			src: `
@group(0) @binding(5) var extra_tex: texture_2d<f32>;
fn fragment(v: Vertex) -> vec4f { return textureLoad(extra_tex, vec2i(0, 0), 0); }`,
			want: "disallowed binding",
		},
		{
			name: "extra sampler",
			src: `
@group(1) @binding(1) var extra_samp: sampler;
fn fragment(v: Vertex) -> vec4f { return textureSample(ggfx_src0, extra_samp, v.src_pos); }`,
			want: "disallowed binding",
		},
		{
			name: "sampler aliasing source texture slot",
			src: `
@group(0) @binding(1) var extra_samp: sampler;
fn fragment(v: Vertex) -> vec4f { return textureSample(ggfx_src1, extra_samp, v.src_pos); }`,
			want: "texture_2d<f32>",
		},
		{
			name: "duplicate texture binding on slot 0",
			src: `
@group(0) @binding(1) var dup_tex: texture_2d<f32>;
fn fragment(v: Vertex) -> vec4f { return src0_at(v.src_pos) + textureLoad(dup_tex, vec2i(0, 0), 0); }`,
			want: "conflict",
		},
		{
			name: "non-f32 texture aliasing slot 0",
			src: `
@group(0) @binding(1) var u32_tex: texture_2d<u32>;
fn fragment(v: Vertex) -> vec4f { return vec4f(textureLoad(u32_tex, vec2i(0, 0), 0)); }`,
			want: "texture_2d<f32>",
		},
		{
			name: "arrayed texture aliasing slot 0",
			src: `
@group(0) @binding(1) var arr_tex: texture_2d_array<f32>;
fn fragment(v: Vertex) -> vec4f { return textureLoad(arr_tex, vec2i(0, 0), 0, 0); }`,
			want: "texture_2d<f32>",
		},
		{
			name: "multisampled texture aliasing slot 0",
			src: `
@group(0) @binding(1) var ms_tex: texture_multisampled_2d<f32>;
fn fragment(v: Vertex) -> vec4f { return textureLoad(ms_tex, vec2i(0, 0), 0); }`,
			want: "texture_2d<f32>",
		},
		{
			name: "storage buffer",
			src: `
@group(1) @binding(1) var<storage, read> buf: array<vec4f>;
fn fragment(v: Vertex) -> vec4f { return buf[0]; }`,
			want: "storage",
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

	// Unbound var<private> globals remain valid.
	if _, err := shader.Compile([]byte(`
var<private> scratch: vec4f = vec4f(1.0);
fn fragment(v: Vertex) -> vec4f {
	scratch = v.color;
	return scratch;
}
`), 4); err != nil {
		t.Errorf("valid private global failed to compile: %v", err)
	}
}

func TestAggregateLimits(t *testing.T) {
	// Valid local aggregate right at MaxUntrustedAggregateBytes (64 KiB = 4096 vec4f).
	validUntrustedLocal := fmt.Sprintf(`
fn fragment(v: Vertex) -> vec4f {
	var a: array<vec4f, %d>;
	a[0] = v.color;
	return a[0];
}
`, shader.MaxUntrustedAggregateBytes/16)
	if _, err := shader.CompileUntrusted([]byte(validUntrustedLocal), 4); err != nil {
		t.Fatalf("local array at MaxUntrustedAggregateBytes failed CompileUntrusted: %v", err)
	}

	// HEAD-accepted local case (65,552 bytes = 4097 vec4f): compiles locally via Compile,
	// rejected by CompileUntrusted.
	localOver64K := fmt.Sprintf(`
fn fragment(v: Vertex) -> vec4f {
	var a: array<vec4f, %d>;
	a[0] = v.color;
	return a[0];
}
`, shader.MaxUntrustedAggregateBytes/16+1)
	if _, err := shader.Compile([]byte(localOver64K), 4); err != nil {
		t.Fatalf("65,552-byte local array must compile locally: %v", err)
	}
	if _, err := shader.CompileUntrusted([]byte(localOver64K), 4); err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("65,552-byte local array under CompileUntrusted = %v, want maximum size error", err)
	}

	// Exponential struct DAG (2^24 * 16 B = 256 MiB) inside a function local.
	var localDAG strings.Builder
	localDAG.WriteString("struct S0 { a: vec4f, b: vec4f }\n")
	for i := 1; i <= 24; i++ {
		fmt.Fprintf(&localDAG, "struct S%d { a: S%d, b: S%d }\n", i, i-1, i-1)
	}
	localDAG.WriteString("fn fragment(v: Vertex) -> vec4f {\n\tvar x: S24;\n\treturn x.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a.a;\n}\n")

	// Multiple 64 KiB locals in one module exceeding MaxUntrustedModuleAggregateBytes (1 MiB).
	var untrustedTotalOverflow strings.Builder
	untrustedTotalOverflow.WriteString("fn fragment(v: Vertex) -> vec4f {\n\tvar acc = vec4f(0.0);\n")
	numUntrustedLocals := shader.MaxUntrustedModuleAggregateBytes/shader.MaxUntrustedAggregateBytes + 1
	for i := range numUntrustedLocals {
		fmt.Fprintf(&untrustedTotalOverflow, "\tvar a%d: array<vec4f, %d>;\n\tacc += a%d[0];\n", i, shader.MaxUntrustedAggregateBytes/16, i)
	}
	untrustedTotalOverflow.WriteString("\treturn acc;\n}\n")

	// Multiple 16 MiB locals exceeding local MaxModuleAggregateBytes (64 MiB).
	var localTotalOverflow strings.Builder
	localTotalOverflow.WriteString("fn fragment(v: Vertex) -> vec4f {\n\tvar acc = vec4f(0.0);\n")
	numLocal := shader.MaxModuleAggregateBytes/shader.MaxAggregateBytes + 1
	for i := range numLocal {
		fmt.Fprintf(&localTotalOverflow, "\tvar a%d: array<vec4f, %d>;\n\tacc += a%d[0];\n", i, shader.MaxAggregateBytes/16, i)
	}
	localTotalOverflow.WriteString("\treturn acc;\n}\n")

	for _, tc := range []struct {
		name      string
		src       string
		want      string
		untrusted bool
	}{
		{
			name: "huge local array",
			src: `
fn fragment(v: Vertex) -> vec4f {
	var a: array<vec4f, 100000000>;
	return a[0];
}`,
			want: "maximum size",
		},
		{
			name: "huge private global array",
			src: `
var<private> a: array<vec4f, 100000000>;
fn fragment(v: Vertex) -> vec4f {
	return a[0];
}`,
			want: "maximum size",
		},
		{
			name: "huge local nested array",
			src: `
fn fragment(v: Vertex) -> vec4f {
	var a: array<array<vec4f, 10000>, 10000>;
	return a[0][0];
}`,
			want: "maximum size",
		},
		{
			name: "huge zero-value let aggregate",
			src: `
fn fragment(v: Vertex) -> vec4f {
	let a = array<vec4f, 100000000>();
	return a[0];
}`,
			want: "maximum size",
		},
		{
			name: "local exponential struct DAG",
			src:  localDAG.String(),
			want: "maximum size",
		},
		{
			name:      "untrusted module total aggregate bytes exceeded",
			src:       untrustedTotalOverflow.String(),
			want:      "total module aggregate",
			untrusted: true,
		},
		{
			name: "local module total aggregate bytes exceeded",
			src:  localTotalOverflow.String(),
			want: "total module aggregate",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			var err error
			if tc.untrusted {
				_, err = shader.CompileUntrusted([]byte(tc.src), 4)
			} else {
				_, err = shader.Compile([]byte(tc.src), 4)
			}
			runtime.ReadMemStats(&after)
			if err == nil {
				t.Fatal("Compile must fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q must mention %q", err, tc.want)
			}
			if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 8<<20 {
				t.Errorf("Compile allocated %d bytes rejecting oversized aggregate, want <= 8 MiB", alloc)
			}
		})
	}
}

// TestPointerDoesNotChargeCompositeDepth verifies R6: a 16-byte local nested to the exact
// maximum depth (32) compiles both by value and when passed through ptr<function, ...>, while
// 33 levels fail in both forms.
func TestPointerDoesNotChargeCompositeDepth(t *testing.T) {
	nestedType := func(levels int) string {
		var b strings.Builder
		for range levels {
			b.WriteString("array<")
		}
		b.WriteString("vec4f")
		for range levels {
			b.WriteString(", 1>")
		}
		return b.String()
	}
	indexChain := func(levels int) string {
		return strings.Repeat("[0]", levels)
	}

	for _, levels := range []int{32, 33} {
		ty := nestedType(levels)
		idx := indexChain(levels)
		byVal := fmt.Sprintf(`
fn read_val(a: %s) -> vec4f { return a%s; }
fn fragment(v: Vertex) -> vec4f {
	var a: %s;
	return read_val(a);
}
`, ty, idx, ty)
		byPtr := fmt.Sprintf(`
fn read_ptr(p: ptr<function, %s>) -> vec4f { return (*p)%s; }
fn fragment(v: Vertex) -> vec4f {
	var a: %s;
	return read_ptr(&a);
}
`, ty, idx, ty)

		wantOK := levels == 32
		for _, mode := range []struct {
			name string
			src  string
		}{
			{fmt.Sprintf("depth_%d_by_value", levels), byVal},
			{fmt.Sprintf("depth_%d_by_ptr", levels), byPtr},
		} {
			t.Run(mode.name, func(t *testing.T) {
				_, errLocal := shader.Compile([]byte(mode.src), 4)
				_, errUntrusted := shader.CompileUntrusted([]byte(mode.src), 4)
				if wantOK {
					if errLocal != nil {
						t.Errorf("Compile(%s) failed: %v", mode.name, errLocal)
					}
					if errUntrusted != nil {
						t.Errorf("CompileUntrusted(%s) failed: %v", mode.name, errUntrusted)
					}
				} else {
					if errLocal == nil || !strings.Contains(errLocal.Error(), "maximum depth") {
						t.Errorf("Compile(%s) = %v, want maximum depth error", mode.name, errLocal)
					}
					if errUntrusted == nil || !strings.Contains(errUntrusted.Error(), "maximum depth") {
						t.Errorf("CompileUntrusted(%s) = %v, want maximum depth error", mode.name, errUntrusted)
					}
				}
			})
		}
	}
}
