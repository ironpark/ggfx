// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package shader_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gogpu/naga/backend"
	"github.com/gogpu/naga/glsl"
	"github.com/gogpu/naga/hlsl"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"

	"github.com/ironpark/ggfx/internal/shader"
)

// User shaders that differ in what the vertex stage could pick up from the module: none of the
// internal block, helpers and a user uniform block, and every varying left unread.
var vertexTestShaders = []string{
	`fn fragment(v: Vertex) -> vec4f { return vec4f(1.0); }`,
	`
struct Foo { a: vec4f, b: f32 }
@group(1) @binding(0) var<uniform> u: Foo;
fn helper(x: vec4f) -> vec4f { return x * u.b + vec4f(dst_origin(), 0.0, 0.0); }
fn fragment(v: Vertex) -> vec4f {
	return helper(src0_at(v.src_pos)) + u.a + src3_at_from_src0(v.src_pos) * v.custom + v.position * f32(front_facing());
}
`,
}

var reGLSLInterface = regexp.MustCompile(`^(layout\(.*\) )?((smooth|flat|noperspective|centroid|sample) )*(in|out|uniform) `)

// glslVertexInterface returns the declarations of a GLSL vertex stage that another stage or the
// driver sees: the struct of the internal block, the uniform blocks, the attributes and the
// varyings.
func glslVertexInterface(t *testing.T, p *shader.Program, version glsl.Version) ([]string, []glsl.UniformInfo) {
	t.Helper()
	o := glsl.DefaultOptions()
	o.LangVersion = version
	o.EntryPoint = &backend.EntryPointSelector{Stage: ir.StageVertex, Name: shader.VertexEntryPoint}
	o.SkipAdjustCoordinateSpace = true
	o.BoundsCheckPolicies.ImageLoad = backend.BoundsCheckUnchecked
	src, info, err := glsl.Compile(p.Module, o)
	if err != nil {
		t.Fatal(err)
	}
	var decls []string
	decls = append(decls, block(t, src, "struct GgfxInternal {", "};"))
	for line := range strings.Lines(src) {
		if reGLSLInterface.MatchString(line) {
			decls = append(decls, strings.TrimSpace(line))
		}
	}
	return decls, info.Uniforms
}

// block returns the text of src from the line starting with start to the next line starting with
// end.
func block(t *testing.T, src, start, end string) string {
	t.Helper()
	src = "\n" + src
	i := strings.Index(src, "\n"+start)
	if i < 0 {
		t.Fatalf("%q is missing in:\n%s", start, src)
	}
	j := strings.Index(src[i+1:], "\n"+end)
	if j < 0 {
		t.Fatalf("%q after %q is missing in:\n%s", end, start, src)
	}
	return src[i+1 : i+1+j+1+len(end)]
}

// naga numbers anonymous types over the whole module, so the internal block's array type is named
// differently once a user shader adds types; the layout is the same.
var reTypeNumber = regexp.MustCompile(`\btype_\d+\b`)

func mslVertexInterface(t *testing.T, p *shader.Program) []string {
	t.Helper()
	index := func(i uint8) *uint8 { return &i }
	resources := map[ir.ResourceBinding]msl.BindTarget{
		shader.InternalUniformBinding: {Buffer: index(0)},
		shader.UserUniformBinding:     {Buffer: index(1)},
	}
	for i := range p.TextureCount {
		resources[shader.TextureBinding(i)] = msl.BindTarget{Texture: index(uint8(i))}
	}
	o := msl.DefaultOptions()
	o.PerEntryPointMap = map[string]msl.EntryPointResources{
		shader.VertexEntryPoint:   {Resources: resources},
		shader.FragmentEntryPoint: {Resources: resources},
	}
	src, info, err := msl.Compile(p.Module, o)
	if err != nil {
		t.Fatal(err)
	}
	name := info.EntryPointNames[shader.VertexEntryPoint]
	return []string{
		name,
		reTypeNumber.ReplaceAllString(block(t, src, "struct GgfxInternal {", "};"), "type_N"),
		block(t, src, "struct "+name+"Input {", "};"),
		block(t, src, "struct "+name+"Output {", "};"),
		block(t, src, "vertex "+name+"Output "+name+"(", ") {"),
	}
}

func hlslVertexInterface(t *testing.T, p *shader.Program) []string {
	t.Helper()
	o := hlsl.DefaultOptions()
	o.ShaderModel = backend.ShaderModel{Major: 5, Minor: 0}
	o.BindingMap = map[ir.ResourceBinding]hlsl.BindTarget{
		shader.InternalUniformBinding: {Register: 0},
		shader.UserUniformBinding:     {Register: 1},
	}
	for i := range p.TextureCount {
		o.BindingMap[shader.TextureBinding(i)] = hlsl.BindTarget{Register: uint32(i)}
	}
	o.FakeMissingBindings = true
	src, info, err := hlsl.Compile(p.Module, o)
	if err != nil {
		t.Fatal(err)
	}
	name := info.EntryPointNames[shader.VertexEntryPoint]
	var cbuffer string
	for line := range strings.Lines(src) {
		if strings.HasPrefix(line, "cbuffer ggfx_internal ") {
			cbuffer = line
		}
	}
	sig := block(t, src, "VertexOutput_"+name+" "+name+"(", "{")
	return []string{
		name,
		cbuffer,
		block(t, src, "struct GgfxInternal {", "};"),
		block(t, src, "struct GgfxVertexIn {", "};"),
		block(t, src, "struct VertexOutput_"+name+" {", "};"),
		sig,
	}
}

func TestVertexProgram(t *testing.T) {
	for _, textureCount := range []int{4, 2} {
		vp, err := shader.VertexProgram(textureCount)
		if err != nil {
			t.Fatal(err)
		}
		if again, _ := shader.VertexProgram(textureCount); again != vp {
			t.Error("VertexProgram must return one program per texture count")
		}
		if vp.TextureCount != textureCount {
			t.Errorf("TextureCount: got %d, want %d", vp.TextureCount, textureCount)
		}

		versions := []glsl.Version{glsl.Version330, {Major: 3, Minor: 0, ES: true}}
		var wantGLSL [][]string
		var wantUniforms [][]glsl.UniformInfo
		for _, v := range versions {
			d, u := glslVertexInterface(t, vp, v)
			wantGLSL = append(wantGLSL, d)
			wantUniforms = append(wantUniforms, u)
		}
		wantMSL := mslVertexInterface(t, vp)
		wantHLSL := hlslVertexInterface(t, vp)
		// Make sure the extraction found what it compares.
		for _, s := range []struct {
			text, want string
		}{
			{strings.Join(wantGLSL[0], "\n"), "layout(location = 3) in vec4 _p2vs_location3;"},
			{strings.Join(wantGLSL[1], "\n"), "_vs2fs_location2;"},
			{strings.Join(wantGLSL[1], "\n"), "uniform GgfxInternal_block_"},
			{strings.Join(wantMSL, "\n"), "[[buffer(0)]]"},
			{strings.Join(wantMSL, "\n"), "[[user(loc2)"},
			{strings.Join(wantHLSL, "\n"), "register(b0)"},
			{strings.Join(wantHLSL, "\n"), ": LOC2;"},
		} {
			if !strings.Contains(s.text, s.want) {
				t.Errorf("%q is missing in\n%s", s.want, s.text)
			}
		}

		for i, src := range vertexTestShaders {
			if textureCount < 4 {
				src = strings.ReplaceAll(src, "src3_", "src1_")
			}
			p, err := shader.Compile([]byte(src), textureCount)
			if err != nil {
				t.Fatal(err)
			}
			for j, v := range versions {
				d, u := glslVertexInterface(t, p, v)
				if !slices.Equal(d, wantGLSL[j]) {
					t.Errorf("%d textures, shader %d, GLSL %v: vertex interface\n%s\nwant\n%s", textureCount, i, v, strings.Join(d, "\n"), strings.Join(wantGLSL[j], "\n"))
				}
				if !slices.Equal(u, wantUniforms[j]) {
					t.Errorf("%d textures, shader %d, GLSL %v: vertex uniform blocks %v, want %v", textureCount, i, v, u, wantUniforms[j])
				}
			}
			if got := mslVertexInterface(t, p); !slices.Equal(got, wantMSL) {
				t.Errorf("%d textures, shader %d: MSL vertex interface\n%s\nwant\n%s", textureCount, i, strings.Join(got, "\n"), strings.Join(wantMSL, "\n"))
			}
			if got := hlslVertexInterface(t, p); !slices.Equal(got, wantHLSL) {
				t.Errorf("%d textures, shader %d: HLSL vertex interface\n%s\nwant\n%s", textureCount, i, strings.Join(got, "\n"), strings.Join(wantHLSL, "\n"))
			}
		}
	}
}
