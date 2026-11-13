// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package shader_test

import (
	"testing"

	"github.com/ironpark/ggfx/internal/builtinshader"
	"github.com/ironpark/ggfx/internal/shader"
)

// The builtin unsafe shader reads only the projection matrix, so the regions
// that differ per draw are zeroed and draws can be merged. The clamp shader
// reads the source regions as well.
func TestFilterInternalUniforms(t *testing.T) {
	l := shader.InternalUniforms(4)
	dwords, projection, srcRegions := l.DwordCount, l.Projection, l.SrcOrigins
	for _, tc := range []struct {
		address builtinshader.Address
		from    int // the first dword that survives
	}{
		{builtinshader.AddressUnsafe, projection},
		{builtinshader.AddressClampToZero, srcRegions},
	} {
		p, err := shader.Compile(builtinshader.ShaderSource(builtinshader.FilterNearest, tc.address), 4)
		if err != nil {
			t.Fatal(err)
		}
		u := make([]uint32, dwords)
		for i := range u {
			u[i] = 1
		}
		p.FilterInternalUniforms(u)
		for i, v := range u {
			want := uint32(0)
			if i >= tc.from {
				want = 1
			}
			if v != want {
				t.Fatalf("address %d: dword %d = %d, want %d", tc.address, i, v, want)
			}
		}
	}
}

func TestInternalUniformReads(t *testing.T) {
	for _, tc := range []struct {
		name            string
		src             string
		dst, srcs, frag bool
	}{
		{"none", `fn fragment(v: Vertex) -> vec4f { return v.color; }`, false, false, false},
		{"unused helper", `
fn unused() -> vec2f { return dst_origin() + src0_origin(); }
fn fragment(v: Vertex) -> vec4f { return v.color; }`, false, false, false},
		{"texture size", `fn fragment(v: Vertex) -> vec4f { return vec4f(dst_texture_size(), 0.0, 1.0); }`, false, false, true},
		{"dst region", `fn fragment(v: Vertex) -> vec4f { return vec4f(dst_size(), 0.0, 1.0); }`, true, false, true},
		{"src regions", `fn fragment(v: Vertex) -> vec4f { return src1_at(v.src_pos); }`, false, true, true},
		{"projection", `fn fragment(v: Vertex) -> vec4f { return ggfx_internal.projection[0]; }`, false, false, true},
		{"whole block", `fn fragment(v: Vertex) -> vec4f { let b = ggfx_internal; return vec4f(b.dst_origin, 0.0, 1.0); }`, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := shader.Compile([]byte(tc.src), 4)
			if err != nil {
				t.Fatal(err)
			}
			if p.ReadsDstRegion != tc.dst || p.ReadsSrcRegions != tc.srcs || p.FragmentReadsInternalUniforms != tc.frag {
				t.Errorf("ReadsDstRegion, ReadsSrcRegions, FragmentReadsInternalUniforms: got %t, %t, %t, want %t, %t, %t",
					p.ReadsDstRegion, p.ReadsSrcRegions, p.FragmentReadsInternalUniforms, tc.dst, tc.srcs, tc.frag)
			}
			// The vertex stage always reads the projection, so filtering keeps it.
			l := shader.InternalUniforms(4)
			u := make([]uint32, l.DwordCount)
			for i := l.Projection; i < l.DwordCount; i++ {
				u[i] = 1
			}
			p.FilterInternalUniforms(u)
			for i := l.Projection; i < l.DwordCount; i++ {
				if u[i] != 1 {
					t.Fatalf("projection dword %d was zeroed; the projection must be read", i)
				}
			}
		})
	}
}

func TestInternalUniformsLayout(t *testing.T) {
	// The prelude compiles for any texture count only if naga lays the block out as stated.
	for n := 1; n <= 8; n++ {
		if _, err := shader.VertexProgram(n); err != nil {
			t.Errorf("%d textures: %v", n, err)
		}
	}
	if got, want := shader.InternalUniforms(4), (shader.InternalUniformLayout{
		DstTextureSize: 0, DstOrigin: 2, DstSize: 4,
		SrcTextureSizes: 8, SrcOrigins: 24, SrcSizes: 40, Projection: 56, DwordCount: 72,
	}); got != want {
		t.Errorf("InternalUniforms(4): got %+v, want %+v", got, want)
	}
}

func TestInternalBlockMisuse(t *testing.T) {
	_, err := shader.Compile([]byte(`
@group(0) @binding(0) var<uniform> other: vec4f;
fn fragment(v: Vertex) -> vec4f { return other; }`), 4)
	if err == nil {
		t.Fatal("Compile must fail for a uniform at the internal block's binding")
	}
}
