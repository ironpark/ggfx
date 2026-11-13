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

// Package shader compiles WGSL shaders with naga and describes them to the graphics drivers.
//
// A user shader defines `fn fragment(v: Vertex) -> vec4f` and, optionally, one uniform block at
// group 1 binding 0. The prelude provides everything else. See Prelude.
package shader

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/valid"
	"github.com/gogpu/naga/wgsl"
)

// InternalUniformBinding is where the prelude's uniform block is bound.
var InternalUniformBinding = ir.ResourceBinding{Group: 0, Binding: 0}

// UserUniformBinding is where a user's uniform block must be bound.
var UserUniformBinding = ir.ResourceBinding{Group: 1, Binding: 0}

// TextureBinding returns where source texture i is bound.
func TextureBinding(i int) ir.ResourceBinding {
	return ir.ResourceBinding{Group: 0, Binding: uint32(i + 1)}
}

// Uniform is one member of a user's uniform block. Slots are the dword offsets of the member's
// scalar elements from the start of the block, in the order a Go value flattens to: vectors by
// component, matrices by column then row, arrays by element, structs by member.
type Uniform struct {
	Name  string
	Slots []int
	Kinds []ir.ScalarKind
}

// Program is a compiled shader.
type Program struct {
	// Module is the validated and compacted IR. Backends translate it; nothing mutates it.
	Module *ir.Module

	// TextureCount is how many source textures the prelude declares.
	TextureCount int

	// Uniforms describes the user's uniform block, if any.
	Uniforms []Uniform

	// UniformDwordCount is the size of the user's uniform block in dwords, or 0 without one.
	UniformDwordCount int

	// ReadsDstRegion reports whether the program reads the destination region (dst_origin,
	// dst_size) of the internal uniform block.
	ReadsDstRegion bool

	// ReadsSrcRegions reports whether the program reads the source regions (src_origins,
	// src_sizes) of the internal uniform block.
	ReadsSrcRegions bool

	// FragmentReadsInternalUniforms reports whether the fragment stage reads the internal uniform
	// block at all. The vertex stage reads only the projection.
	FragmentReadsInternalUniforms bool

	// mask is 1 for each dword of the internal uniform block the program reads, 0 otherwise.
	mask []uint32
}

// Compile compiles a user shader. textureCount is how many source textures the prelude declares.
func Compile(src []byte, textureCount int) (*Program, error) {
	prelude := Prelude(textureCount)
	source := prelude + "\n" + string(src)
	preludeLines := strings.Count(prelude, "\n") + 1

	module, _, err := wgsl.Parse(source, wgsl.ParseOptions{})
	if err != nil {
		return nil, fmt.Errorf("shader: %w", locateError(err, source, preludeLines))
	}
	// The zero options allow no optional capability (f64, i64, f16, ...): no driver here has them.
	if _, err := valid.Validate(module, valid.Options{}); err != nil {
		var errs valid.Errors
		if errors.As(err, &errs) && len(errs) > 1 {
			msgs := make([]string, len(errs))
			for i, e := range errs {
				msgs[i] = e.Error()
			}
			return nil, fmt.Errorf("shader: validation failed: %s", strings.Join(msgs, "; "))
		}
		return nil, fmt.Errorf("shader: validation failed: %w", err)
	}

	var hasVertex, hasFragment bool
	for _, ep := range module.EntryPoints {
		switch ep.Name {
		case VertexEntryPoint:
			hasVertex = true
		case FragmentEntryPoint:
			hasFragment = true
		}
	}
	if !hasVertex || !hasFragment {
		return nil, errors.New("shader: the entry points are missing; do not declare @vertex or @fragment functions")
	}

	p := &Program{
		Module:       module,
		TextureCount: textureCount,
	}
	if err := p.reflectUniforms(); err != nil {
		return nil, err
	}

	naga.Compact(module, naga.KeepUnusedNo)
	if err := p.reflectInternalReads(); err != nil {
		return nil, err
	}
	return p, nil
}

// locateError puts the source position of a naga parse error in front of its message, counting
// lines so that line 1 is the first line of the user's source rather than of the prelude.
func locateError(err error, source string, preludeLines int) error {
	var perr *wgsl.ParseError
	if !errors.As(err, &perr) {
		return err
	}
	loc, ok := perr.Location(source)
	if !ok {
		return err
	}
	if int(loc.Line) <= preludeLines {
		return fmt.Errorf("%w (in the ggfx prelude)", err)
	}
	return fmt.Errorf("line %d, column %d: %w", int(loc.Line)-preludeLines, loc.Column, err)
}

// reflectUniforms describes the user's uniform block and checks the internal one. It runs before
// compaction, which drops a uniform block nothing reads but which the caller still passes values
// for, and lays the module out once.
func (p *Program) reflectUniforms() error {
	var user, internal *ir.GlobalVariable
	for i := range p.Module.GlobalVariables {
		g := &p.Module.GlobalVariables[i]
		if g.Space == ir.SpaceStorage {
			return fmt.Errorf("shader: storage variable %q is not supported", g.Name)
		}
		if g.Space != ir.SpaceUniform || g.Binding == nil {
			continue
		}
		switch *g.Binding {
		case InternalUniformBinding:
			if internal != nil {
				return fmt.Errorf("shader: %q is bound where the internal uniform block %q is", g.Name, internalUniformName)
			}
			internal = g
		case UserUniformBinding:
			if user != nil {
				return fmt.Errorf("shader: only one uniform variable may be declared at @group(1) @binding(0), found %q and %q", user.Name, g.Name)
			}
			user = g
		default:
			return fmt.Errorf("shader: uniform variable %q must be declared at @group(1) @binding(0)", g.Name)
		}
	}
	if internal == nil {
		return errors.New("shader: the internal uniform block is missing")
	}

	var layouter ir.Layouter
	if err := layouter.Update(p.Module); err != nil {
		return fmt.Errorf("shader: laying out the uniform blocks failed: %w", err)
	}
	if err := layoutInternalBlock(p.Module, &layouter, internal, p.TextureCount); err != nil {
		return err
	}
	if user == nil {
		return nil
	}

	size := int(layouter.Layout(user.Type).Size)
	p.UniformDwordCount = (size + 15) / 16 * 4
	if s, ok := p.Module.Types[user.Type].Inner.(ir.StructType); ok {
		for _, m := range s.Members {
			u := Uniform{Name: m.Name}
			p.collectSlots(&u, m.Type, int(m.Offset))
			p.Uniforms = append(p.Uniforms, u)
		}
		return nil
	}
	u := Uniform{Name: user.Name}
	p.collectSlots(&u, user.Type, 0)
	p.Uniforms = append(p.Uniforms, u)
	return nil
}

func (p *Program) collectSlots(u *Uniform, t ir.TypeHandle, offset int) {
	add := func(byteOffset int, kind ir.ScalarKind) {
		u.Slots = append(u.Slots, byteOffset/4)
		u.Kinds = append(u.Kinds, kind)
	}
	switch inner := p.Module.Types[t].Inner.(type) {
	case ir.ScalarType:
		add(offset, inner.Kind)
	case ir.VectorType:
		for i := range int(inner.Size) {
			add(offset+4*i, inner.Scalar.Kind)
		}
	case ir.MatrixType:
		// A column is a vector; a vec3 column is padded to 16 bytes, as in WGSL's layout rules.
		stride := 8
		if inner.Rows > 2 {
			stride = 16
		}
		for c := range int(inner.Columns) {
			for r := range int(inner.Rows) {
				add(offset+c*stride+4*r, inner.Scalar.Kind)
			}
		}
	case ir.ArrayType:
		if inner.Size.Constant == nil {
			return
		}
		for i := range int(*inner.Size.Constant) {
			p.collectSlots(u, inner.Base, offset+i*int(inner.Stride))
		}
	case ir.StructType:
		for _, m := range inner.Members {
			p.collectSlots(u, m.Type, offset+int(m.Offset))
		}
	}
}
