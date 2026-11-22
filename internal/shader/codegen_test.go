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
	"bytes"
	"encoding/binary"
	"strings"
	"sync"
	"testing"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/shader"
)

const codegenFullShader = `
struct Uniforms {
	tint: vec4f,
	scale: f32,
}
@group(1) @binding(0) var<uniform> u: Uniforms;

fn fragment(v: Vertex) -> vec4f {
	let c0 = src0_at(v.src_pos);
	let c1 = src1_at_from_src0(v.src_pos);
	let c2 = src2_at_from_src0(v.src_pos);
	let c3 = src3_at_from_src0(v.src_pos);
	let o = dst_origin();
	return (c0 + c1 + c2 + c3) * u.tint * u.scale + vec4f(o, 0.0, 0.0);
}
`

const codegenSimpleShader = `
fn fragment(v: Vertex) -> vec4f {
	return v.color;
}
`

func TestCodegenAllTargets(t *testing.T) {
	prog, err := shader.CompileUser([]byte(codegenFullShader))
	if err != nil {
		t.Fatal(err)
	}
	desc := prog.Descriptor()
	if desc != prog.Descriptor() {
		t.Error("Descriptor must return the same pointer on repeated calls")
	}
	if desc.TextureCount != prog.TextureCount ||
		desc.UniformDwordCount != prog.UniformDwordCount ||
		desc.FragmentReadsInternalUniforms != prog.FragmentReadsInternalUniforms ||
		!bytes.Equal(desc.WGSL, prog.Source) ||
		desc.Source == nil {
		t.Fatalf("Descriptor metadata mismatch: %+v", desc)
	}

	targets := []graphicsdriver.ShaderTarget{
		{Language: graphicsdriver.ShaderLanguageMSL},
		{Language: graphicsdriver.ShaderLanguageHLSL, Version: 50},
		{Language: graphicsdriver.ShaderLanguageGLSL, Version: 330},
		{Language: graphicsdriver.ShaderLanguageGLSLES, Version: 300},
		{Language: graphicsdriver.ShaderLanguageSPIRV, Version: 10},
	}

	for _, target := range targets {
		t.Run(target.Language.String(), func(t *testing.T) {
			vs, err := desc.Source.VertexStage(target)
			if err != nil {
				t.Fatalf("VertexStage(%+v): %v", target, err)
			}
			if len(vs.Code) == 0 {
				t.Error("empty vertex code")
			}
			if vs.EntryPoint == "" {
				t.Error("empty vertex entry point")
			}

			fs, err := desc.Source.FragmentStage(target)
			if err != nil {
				t.Fatalf("FragmentStage(%+v): %v", target, err)
			}
			if len(fs.Code) == 0 {
				t.Error("empty fragment code")
			}
			if fs.EntryPoint == "" {
				t.Error("empty fragment entry point")
			}

			switch target.Language {
			case graphicsdriver.ShaderLanguageMSL:
				if !strings.Contains(string(vs.Code), "[[buffer(1)]]") {
					t.Errorf("MSL vertex missing [[buffer(1)]]:\n%s", vs.Code)
				}
				if !strings.Contains(string(fs.Code), "[[buffer(1)]]") ||
					!strings.Contains(string(fs.Code), "[[buffer(2)]]") ||
					!strings.Contains(string(fs.Code), "[[texture(0)]]") ||
					!strings.Contains(string(fs.Code), "[[texture(3)]]") {
					t.Errorf("MSL fragment missing expected bindings:\n%s", fs.Code)
				}

			case graphicsdriver.ShaderLanguageHLSL:
				if !strings.Contains(string(vs.Code), "register(b0)") {
					t.Errorf("HLSL vertex missing register(b0):\n%s", vs.Code)
				}
				if !strings.Contains(string(fs.Code), "register(b0)") ||
					!strings.Contains(string(fs.Code), "register(b1)") ||
					!strings.Contains(string(fs.Code), "register(t0)") ||
					!strings.Contains(string(fs.Code), "register(t3)") {
					t.Errorf("HLSL fragment missing expected registers:\n%s", fs.Code)
				}

			case graphicsdriver.ShaderLanguageGLSL, graphicsdriver.ShaderLanguageGLSLES:
				var hasVSInternal bool
				for _, b := range vs.UniformBlocks {
					if b.Kind == graphicsdriver.ShaderUniformBlockInternal && b.Name != "" {
						hasVSInternal = true
					}
				}
				if !hasVSInternal {
					t.Errorf("GLSL vertex missing internal uniform block: %+v", vs.UniformBlocks)
				}
				if len(vs.TextureNames) != 0 {
					t.Errorf("GLSL vertex unexpectedly has texture names: %v", vs.TextureNames)
				}

				var hasFSInternal, hasFSUser bool
				for _, b := range fs.UniformBlocks {
					switch b.Kind {
					case graphicsdriver.ShaderUniformBlockInternal:
						hasFSInternal = b.Name != ""
					case graphicsdriver.ShaderUniformBlockUser:
						hasFSUser = b.Name != ""
					}
				}
				if !hasFSInternal || !hasFSUser {
					t.Errorf("GLSL fragment uniform blocks: got %+v, want internal and user", fs.UniformBlocks)
				}
				if len(fs.TextureNames) != desc.TextureCount {
					t.Fatalf("GLSL fragment texture names len: got %d, want %d", len(fs.TextureNames), desc.TextureCount)
				}
				for i, name := range fs.TextureNames {
					if name == "" || !strings.Contains(string(fs.Code), name) {
						t.Errorf("GLSL fragment missing texture uniform %d (%q) in source", i, name)
					}
				}

			case graphicsdriver.ShaderLanguageSPIRV:
				checkSPIRVHeader(t, "vertex", vs.Code)
				checkSPIRVHeader(t, "fragment", fs.Code)

				vsBindings := scanSPIRVBindings(t, vs.Code)
				wantVS := map[spirvBinding]bool{
					{set: graphicsdriver.VulkanDescriptorSet, binding: graphicsdriver.VulkanInternalUniformBinding}: true,
				}
				checkSPIRVBindings(t, "vertex", vsBindings, wantVS)

				fsBindings := scanSPIRVBindings(t, fs.Code)
				wantFS := map[spirvBinding]bool{
					{set: graphicsdriver.VulkanDescriptorSet, binding: graphicsdriver.VulkanInternalUniformBinding}: true,
					{set: graphicsdriver.VulkanDescriptorSet, binding: graphicsdriver.VulkanTextureBindingBase + 0}: true,
					{set: graphicsdriver.VulkanDescriptorSet, binding: graphicsdriver.VulkanTextureBindingBase + 1}: true,
					{set: graphicsdriver.VulkanDescriptorSet, binding: graphicsdriver.VulkanTextureBindingBase + 2}: true,
					{set: graphicsdriver.VulkanDescriptorSet, binding: graphicsdriver.VulkanTextureBindingBase + 3}: true,
					{set: graphicsdriver.VulkanDescriptorSet, binding: graphicsdriver.VulkanUserUniformBinding}:     true,
				}
				checkSPIRVBindings(t, "fragment", fsBindings, wantFS)
			}
		})
	}
}

func TestVertexStageCaching(t *testing.T) {
	p1, err := shader.CompileUser([]byte(codegenSimpleShader))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := shader.CompileUser([]byte(codegenFullShader))
	if err != nil {
		t.Fatal(err)
	}

	target := graphicsdriver.ShaderTarget{Language: graphicsdriver.ShaderLanguageSPIRV, Version: 10}
	v1, err := p1.VertexStage(target)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := p2.VertexStage(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v1.Code, v2.Code) || v1.EntryPoint != v2.EntryPoint {
		t.Error("VertexStage differs across programs with the same TextureCount")
	}
	if &v1.Code[0] != &v2.Code[0] {
		t.Error("VertexStage did not share cached backing buffer")
	}

	// Concurrent calls must be safe and return identical results.
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := p1.VertexStage(graphicsdriver.ShaderTarget{Language: graphicsdriver.ShaderLanguageMSL})
			if err != nil {
				t.Errorf("concurrent VertexStage: %v", err)
				return
			}
			if len(got.Code) == 0 || got.EntryPoint == "" {
				t.Errorf("concurrent VertexStage returned empty code or entry point")
			}
		}()
	}
	wg.Wait()
}

type spirvBinding struct {
	set     uint32
	binding uint32
}

func checkSPIRVHeader(t *testing.T, label string, bin []byte) {
	t.Helper()
	if len(bin) < 20 || len(bin)%4 != 0 {
		t.Fatalf("%s SPIR-V invalid byte length %d", label, len(bin))
	}
	const spirvMagic = 0x07230203
	if magic := binary.LittleEndian.Uint32(bin[0:4]); magic != spirvMagic {
		t.Errorf("%s SPIR-V magic: got 0x%08x, want 0x%08x", label, magic, spirvMagic)
	}
	if ver := binary.LittleEndian.Uint32(bin[4:8]); ver != 0x00010000 {
		t.Errorf("%s SPIR-V version: got 0x%08x, want 0x00010000 (1.0)", label, ver)
	}
}

func scanSPIRVBindings(t *testing.T, bin []byte) map[spirvBinding]bool {
	t.Helper()
	const (
		opDecorate              = 71
		decorationBinding       = 33
		decorationDescriptorSet = 34
	)
	words := make([]uint32, len(bin)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(bin[4*i:])
	}

	sets := map[uint32]uint32{}
	bindings := map[uint32]uint32{}
	for i := 5; i < len(words); {
		wc := int(words[i] >> 16)
		op := words[i] & 0xffff
		if wc == 0 || i+wc > len(words) {
			t.Fatalf("malformed SPIR-V instruction at word %d (wc=%d, total=%d)", i, wc, len(words))
		}
		if op == opDecorate && wc >= 4 {
			target := words[i+1]
			dec := words[i+2]
			val := words[i+3]
			switch dec {
			case decorationDescriptorSet:
				sets[target] = val
			case decorationBinding:
				bindings[target] = val
			}
		}
		i += wc
	}

	got := map[spirvBinding]bool{}
	for id, b := range bindings {
		s, ok := sets[id]
		if !ok {
			t.Errorf("SPIR-V id %d has Binding=%d without DescriptorSet decoration", id, b)
			continue
		}
		if s != graphicsdriver.VulkanDescriptorSet {
			t.Errorf("SPIR-V id %d has DescriptorSet=%d, want %d", id, s, graphicsdriver.VulkanDescriptorSet)
		}
		got[spirvBinding{set: s, binding: b}] = true
	}
	return got
}

func checkSPIRVBindings(t *testing.T, label string, got, want map[spirvBinding]bool) {
	t.Helper()
	for b := range want {
		if !got[b] {
			t.Errorf("%s SPIR-V missing binding (set=%d, binding=%d); got %v", label, b.set, b.binding, got)
		}
	}
	for b := range got {
		if !want[b] {
			t.Errorf("%s SPIR-V unexpected binding (set=%d, binding=%d)", label, b.set, b.binding)
		}
	}
}
