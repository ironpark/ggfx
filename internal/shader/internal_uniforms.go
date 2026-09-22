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
	"github.com/gogpu/naga/ir"
)

// The name of the internal uniform block the prelude declares.
const internalUniformName = "ggfx_internal"

// internalUniformMask reports which dwords of the internal uniform block the
// program reads: 1 where a field is reachable from an entry point, 0 where
// it is not. The mask is computed once, after compaction, so a helper the
// user never calls does not count.
//
// The point is draw call merging. The internal block carries the
// destination region and the source regions, which differ from one draw to
// the next; a shader that never reads them, like the builtin unsafe-address
// shaders, would still see different uniforms per draw and never merge.
// FilterInternalUniforms zeroes what the program cannot see so that equal
// draws compare equal.
func (p *Program) internalUniformMask() []uint32 {
	p.maskOnce.Do(func() {
		var global ir.GlobalVariableHandle
		found := false
		for i := range p.Module.GlobalVariables {
			if p.Module.GlobalVariables[i].Name == internalUniformName {
				global, found = ir.GlobalVariableHandle(i), true
				break
			}
		}
		if !found {
			return
		}
		st, ok := p.Module.Types[p.Module.GlobalVariables[global].Type].Inner.(ir.StructType)
		if !ok {
			return
		}
		size := (int(st.Span) + 3) / 4
		mask := make([]uint32, size)
		markAll := func() {
			for i := range mask {
				mask[i] = 1
			}
		}
		markMember := func(idx uint32) {
			if int(idx) >= len(st.Members) {
				markAll()
				return
			}
			m := st.Members[idx]
			from := int(m.Offset) / 4
			to := from + int(ir.TypeSize(p.Module, m.Type)+3)/4
			for i := from; i < to && i < len(mask); i++ {
				mask[i] = 1
			}
		}
		scan := func(f *ir.Function) {
			isGlobal := func(h ir.ExpressionHandle) bool {
				if int(h) >= len(f.Expressions) {
					return false
				}
				g, ok := f.Expressions[h].Kind.(ir.ExprGlobalVariable)
				return ok && g.Variable == global
			}
			for _, e := range f.Expressions {
				switch k := e.Kind.(type) {
				case ir.ExprAccessIndex:
					if isGlobal(k.Base) {
						markMember(k.Index)
					}
				case ir.ExprAccess:
					if isGlobal(k.Base) {
						markAll()
					}
				case ir.ExprLoad:
					// The whole block read at once.
					if isGlobal(k.Pointer) {
						markAll()
					}
				}
			}
		}
		for i := range p.Module.Functions {
			scan(&p.Module.Functions[i])
		}
		for i := range p.Module.EntryPoints {
			scan(&p.Module.EntryPoints[i].Function)
		}
		p.mask = mask
	})
	return p.mask
}

// FilterInternalUniforms zeroes the dwords of the internal uniform block
// that the program does not read, so that draws which differ only in what
// the shader cannot see can be merged. uniforms is the internal block, or
// a longer slice that starts with it.
func (p *Program) FilterInternalUniforms(uniforms []uint32) {
	mask := p.internalUniformMask()
	for i, m := range mask {
		if i >= len(uniforms) {
			return
		}
		uniforms[i] *= m
	}
}
