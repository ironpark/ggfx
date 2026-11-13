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

package shader

import (
	"fmt"

	"github.com/gogpu/naga/ir"
)

// InternalUniformLayout is where the members of the prelude's internal uniform block lie, in dwords
// from the start of the block. An array member holds one vec4f per source texture, so source
// texture i's entry is at the member's offset + 4*i.
type InternalUniformLayout struct {
	DstTextureSize  int
	DstOrigin       int
	DstSize         int
	SrcTextureSizes int
	SrcOrigins      int
	SrcSizes        int
	Projection      int

	// DwordCount is the size of the whole block.
	DwordCount int
}

// InternalUniforms returns the layout of the internal uniform block for textureCount source
// textures. It is the one statement of the layout: Compile fails for a prelude that naga lays out
// otherwise, and the Go constants the driver side fills the block by are tested against it.
func InternalUniforms(textureCount int) InternalUniformLayout {
	// The WGSL uniform layout: vec2f is 8-byte aligned, array<vec4f> and mat4x4f 16-byte.
	l := InternalUniformLayout{
		DstTextureSize:  0,
		DstOrigin:       2,
		DstSize:         4,
		SrcTextureSizes: 8,
	}
	l.SrcOrigins = l.SrcTextureSizes + 4*textureCount
	l.SrcSizes = l.SrcOrigins + 4*textureCount
	l.Projection = l.SrcSizes + 4*textureCount
	l.DwordCount = l.Projection + 16
	return l
}

// The name of the internal uniform block the prelude declares.
const internalUniformName = "ggfx_internal"

// dwordRange is the dwords [from, to) of the internal uniform block.
type dwordRange struct{ from, to int }

type internalMember struct {
	name string
	r    dwordRange
}

// members returns the internal block's members in declaration order. Each member runs up to the
// next, so the array members' lengths follow from the layout.
func (l InternalUniformLayout) members() []internalMember {
	type m = internalMember
	return []m{
		{"dst_texture_size", dwordRange{l.DstTextureSize, l.DstTextureSize + 2}},
		{"dst_origin", dwordRange{l.DstOrigin, l.DstOrigin + 2}},
		{"dst_size", dwordRange{l.DstSize, l.DstSize + 2}},
		{"src_texture_sizes", dwordRange{l.SrcTextureSizes, l.SrcOrigins}},
		{"src_origins", dwordRange{l.SrcOrigins, l.SrcSizes}},
		{"src_sizes", dwordRange{l.SrcSizes, l.Projection}},
		{"projection", dwordRange{l.Projection, l.Projection + 16}},
	}
}

// layoutInternalBlock checks naga's layout of the internal block g against InternalUniforms. A
// mismatch would mis-zero uniforms in FilterInternalUniforms and merge draws that differ, so it is
// an error, not a fallback.
func layoutInternalBlock(module *ir.Module, layouter *ir.Layouter, g *ir.GlobalVariable, textureCount int) error {
	if g.Name != internalUniformName {
		return fmt.Errorf("shader: %q is bound where the internal uniform block %q is", g.Name, internalUniformName)
	}
	st, ok := module.Types[g.Type].Inner.(ir.StructType)
	if !ok {
		return fmt.Errorf("shader: the internal uniform block %q is not a struct", g.Name)
	}
	l := InternalUniforms(textureCount)
	want := l.members()
	if len(st.Members) != len(want) {
		return fmt.Errorf("shader: the internal uniform block has %d members, want %d", len(st.Members), len(want))
	}
	for i, m := range st.Members {
		size := layouter.Layout(m.Type).Size
		got := dwordRange{int(m.Offset) / 4, int(m.Offset+size+3) / 4}
		if m.Name != want[i].name || m.Offset%4 != 0 || got != want[i].r {
			return fmt.Errorf("shader: internal uniform member %d is %s at dwords [%d, %d), want %s at [%d, %d)", i, m.Name, got.from, got.to, want[i].name, want[i].r.from, want[i].r.to)
		}
	}
	if span := int(st.Span) / 4; span != l.DwordCount {
		return fmt.Errorf("shader: the internal uniform block is %d dwords, want %d", span, l.DwordCount)
	}
	return nil
}

// reflectInternalReads records which dwords of the internal uniform block the compacted module
// reads, by member index into InternalUniforms' members, which layoutInternalBlock checked naga's
// layout against before compaction (member indices survive compaction; handles do not). Working
// after compaction means a helper the user never calls does not count.
//
// The point is draw call merging. The internal block carries the destination region and the
// source regions, which differ from one draw to the next; a shader that never reads them, like the
// builtin unsafe-address shaders, would still see different uniforms per draw and never merge.
// FilterInternalUniforms zeroes what the program cannot see so that equal draws compare equal.
func (p *Program) reflectInternalReads() error {
	global, ok := findInternalBlock(p.Module)
	if !ok {
		// The vertex stage reads the projection, so compaction cannot drop the block.
		return fmt.Errorf("shader: the internal uniform block is missing after compaction")
	}
	l := InternalUniforms(p.TextureCount)
	members := l.members()

	scan := func(f *ir.Function, mask []uint32) error {
		isGlobal := func(h ir.ExpressionHandle) bool {
			if int(h) >= len(f.Expressions) {
				return false
			}
			g, ok := f.Expressions[h].Kind.(ir.ExprGlobalVariable)
			return ok && g.Variable == global
		}
		mark := func(r dwordRange) {
			for i := r.from; i < r.to; i++ {
				mask[i] = 1
			}
		}
		for _, e := range f.Expressions {
			switch k := e.Kind.(type) {
			case ir.ExprAccessIndex:
				if isGlobal(k.Base) {
					if int(k.Index) >= len(members) {
						return fmt.Errorf("shader: %s reads member %d of the internal uniform block, which has %d", f.Name, k.Index, len(members))
					}
					mark(members[k.Index].r)
				}
			case ir.ExprAccess:
				if isGlobal(k.Base) {
					mark(dwordRange{0, len(mask)})
				}
			case ir.ExprLoad:
				// The whole block read at once.
				if isGlobal(k.Pointer) {
					mark(dwordRange{0, len(mask)})
				}
			}
		}
		return nil
	}

	// Compaction leaves only functions an entry point calls, and the prelude's vertex entry point
	// calls none, so every function belongs to the fragment stage.
	frag := make([]uint32, l.DwordCount)
	for i := range p.Module.Functions {
		if err := scan(&p.Module.Functions[i], frag); err != nil {
			return err
		}
	}
	all := make([]uint32, l.DwordCount)
	for i := range p.Module.EntryPoints {
		ep := &p.Module.EntryPoints[i]
		mask := all
		if ep.Stage == ir.StageFragment {
			mask = frag
		}
		if err := scan(&ep.Function, mask); err != nil {
			return err
		}
	}
	for i := range all {
		all[i] |= frag[i]
	}

	p.mask = all
	p.ReadsDstRegion = readsAny(all, l.DstOrigin, l.SrcTextureSizes)
	p.ReadsSrcRegions = readsAny(all, l.SrcOrigins, l.Projection)
	p.FragmentReadsInternalUniforms = readsAny(frag, 0, len(frag))
	return nil
}

// findInternalBlock returns the internal uniform block of module, found by its binding.
func findInternalBlock(module *ir.Module) (ir.GlobalVariableHandle, bool) {
	for i := range module.GlobalVariables {
		g := &module.GlobalVariables[i]
		if g.Space == ir.SpaceUniform && g.Binding != nil && *g.Binding == InternalUniformBinding {
			return ir.GlobalVariableHandle(i), true
		}
	}
	return 0, false
}

func readsAny(mask []uint32, from, to int) bool {
	for i := max(from, 0); i < to && i < len(mask); i++ {
		if mask[i] != 0 {
			return true
		}
	}
	return false
}

// FilterInternalUniforms zeroes the dwords of the internal uniform block
// that the program does not read, so that draws which differ only in what
// the shader cannot see can be merged. uniforms is the internal block, or
// a longer slice that starts with it.
func (p *Program) FilterInternalUniforms(uniforms []uint32) {
	for i, m := range p.mask {
		if i >= len(uniforms) {
			return
		}
		uniforms[i] *= m
	}
}
