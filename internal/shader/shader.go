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
	"math/bits"
	"strings"
	"sync"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/valid"
	"github.com/gogpu/naga/wgsl"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// InternalUniformBinding is where the prelude's uniform block is bound.
var InternalUniformBinding = ir.ResourceBinding{Group: 0, Binding: 0}

// UserUniformBinding is where a user's uniform block must be bound.
var UserUniformBinding = ir.ResourceBinding{Group: 1, Binding: 0}

const (
	// Local shader compilation limits (Compile / CompileUser). These are anti-pathological
	// bounds that prevent runaway reflection or codegen while allowing every shader accepted by
	// hardware drivers.
	MaxUserUniformBytes        = 1024 * 1024 // 1 MiB
	MaxUserUniformDwords       = MaxUserUniformBytes / 4
	MaxAggregateBytes          = 16 * 1024 * 1024 // 16 MiB per constant-sized aggregate
	MaxAggregateElements       = MaxAggregateBytes
	MaxModuleAggregateBytes    = 64 * 1024 * 1024 // 64 MiB combined across a module
	MaxModuleAggregateElements = MaxModuleAggregateBytes

	// Strict untrusted shader compilation limits (CompileUntrusted, e.g. for remote streams).
	// MaxUntrustedUserUniformBytes (16 KiB, or 1024 vec4f) matches the smallest guaranteed uniform
	// block size across all supported graphics APIs (OpenGL 3.3 / GLES 3.0 / WebGL 2 / Vulkan).
	MaxUntrustedUserUniformBytes        = 16 * 1024
	MaxUntrustedUserUniformDwords       = MaxUntrustedUserUniformBytes / 4
	MaxUntrustedAggregateBytes          = 64 * 1024
	MaxUntrustedAggregateElements       = MaxUntrustedAggregateBytes
	MaxUntrustedModuleAggregateBytes    = 1024 * 1024
	MaxUntrustedModuleAggregateElements = MaxUntrustedModuleAggregateBytes

	// maxUniformTypeDepth bounds composite (array/struct) nesting in a user uniform block and in
	// shader aggregates so type checking stays strictly bounded even for deeply nested arrays.
	// Pointer indirection does not count toward composite nesting depth.
	maxUniformTypeDepth = 32
)

type compileLimits struct {
	maxUserUniformBytes        uint64
	maxUserUniformDwords       uint64
	maxAggregateBytes          uint64
	maxAggregateElements       uint64
	maxModuleAggregateBytes    uint64
	maxModuleAggregateElements uint64
}

var (
	localCompileLimits = compileLimits{
		maxUserUniformBytes:        MaxUserUniformBytes,
		maxUserUniformDwords:       MaxUserUniformDwords,
		maxAggregateBytes:          MaxAggregateBytes,
		maxAggregateElements:       MaxAggregateElements,
		maxModuleAggregateBytes:    MaxModuleAggregateBytes,
		maxModuleAggregateElements: MaxModuleAggregateElements,
	}
	untrustedCompileLimits = compileLimits{
		maxUserUniformBytes:        MaxUntrustedUserUniformBytes,
		maxUserUniformDwords:       MaxUntrustedUserUniformDwords,
		maxAggregateBytes:          MaxUntrustedAggregateBytes,
		maxAggregateElements:       MaxUntrustedAggregateElements,
		maxModuleAggregateBytes:    MaxUntrustedModuleAggregateBytes,
		maxModuleAggregateElements: MaxUntrustedModuleAggregateElements,
	}
)

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

	// Source is the user's source Compile was given, without the prelude. The IR has no wire form,
	// so a driver that runs the program in another process, the remote driver, sends this instead.
	Source []byte

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

	descOnce sync.Once
	desc     graphicsdriver.ShaderDescriptor
}

// CompileUser compiles a WGSL fragment shader with graphics.ShaderSrcImageCount source textures.
func CompileUser(src []byte) (*Program, error) {
	return Compile(src, graphics.ShaderSrcImageCount)
}

// Compile compiles a local user shader. textureCount is how many source textures the prelude
// declares.
func Compile(src []byte, textureCount int) (*Program, error) {
	return compile(src, textureCount, localCompileLimits)
}

// CompileUntrusted compiles an untrusted WGSL shader (such as one received over a remote stream)
// under strict uniform block and constant-sized aggregate limits.
func CompileUntrusted(src []byte, textureCount int) (*Program, error) {
	return compile(src, textureCount, untrustedCompileLimits)
}

func compile(src []byte, textureCount int, lim compileLimits) (*Program, error) {
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
		default:
			return nil, fmt.Errorf("shader: unexpected entry point %q; do not declare @vertex, @fragment or @compute functions", ep.Name)
		}
	}
	if !hasVertex || !hasFragment || len(module.EntryPoints) != 2 {
		return nil, errors.New("shader: the entry points are missing; do not declare @vertex or @fragment functions")
	}

	p := &Program{
		Module:       module,
		TextureCount: textureCount,
		Source:       append([]byte(nil), src...),
	}
	if err := p.reflectUniforms(lim); err != nil {
		return nil, err
	}

	naga.Compact(module, naga.KeepUnusedNo)
	if err := p.validateGlobals(); err != nil {
		return nil, err
	}
	if err := p.validateModuleTypes(lim); err != nil {
		return nil, err
	}
	if err := p.reflectInternalReads(); err != nil {
		return nil, err
	}
	return p, nil
}

// validateGlobals checks that every global variable reachable in the compacted module conforms to
// the ggfx shader contract: handle-space globals must be texture_2d<f32> at prelude source texture
// bindings 0..p.TextureCount-1 without duplicates, uniform-space globals must be the internal or
// user uniform block without duplicates, private globals must have no binding, and no other address
// space is permitted.
func (p *Program) validateGlobals() error {
	var seenInternal, seenUser bool
	seenTextures := make([]bool, p.TextureCount)
	for i := range p.Module.GlobalVariables {
		g := &p.Module.GlobalVariables[i]
		switch g.Space {
		case ir.SpacePrivate:
			if g.Binding != nil {
				return fmt.Errorf("shader: private global %q must not have a resource binding", g.Name)
			}
		case ir.SpaceUniform:
			if g.Binding == nil {
				return fmt.Errorf("shader: uniform global %q must have a resource binding", g.Name)
			}
			switch *g.Binding {
			case InternalUniformBinding:
				if seenInternal {
					return fmt.Errorf("shader: duplicate internal uniform binding on %q", g.Name)
				}
				seenInternal = true
			case UserUniformBinding:
				if seenUser {
					return fmt.Errorf("shader: duplicate user uniform binding on %q", g.Name)
				}
				seenUser = true
			default:
				return fmt.Errorf("shader: uniform global %q has disallowed binding (%d, %d)", g.Name, g.Binding.Group, g.Binding.Binding)
			}
		case ir.SpaceHandle:
			if g.Binding == nil {
				return fmt.Errorf("shader: handle global %q must have a resource binding", g.Name)
			}
			slot := -1
			for k := range p.TextureCount {
				if *g.Binding == TextureBinding(k) {
					slot = k
					break
				}
			}
			if slot < 0 {
				return fmt.Errorf("shader: resource %q has disallowed binding (%d, %d); extra textures and samplers are not supported", g.Name, g.Binding.Group, g.Binding.Binding)
			}
			if seenTextures[slot] {
				return fmt.Errorf("shader: duplicate texture binding on %q for slot %d", g.Name, slot)
			}
			seenTextures[slot] = true
			if int(g.Type) >= len(p.Module.Types) {
				return fmt.Errorf("shader: resource %q has invalid type handle", g.Name)
			}
			imgType, ok := p.Module.Types[g.Type].Inner.(ir.ImageType)
			if !ok || imgType.Dim != ir.Dim2D || imgType.Arrayed || imgType.Multisampled ||
				imgType.Class != ir.ImageClassSampled || imgType.SampledKind != ir.ScalarFloat {
				return fmt.Errorf("shader: resource %q at slot %d must be texture_2d<f32>", g.Name, slot)
			}
		default:
			return fmt.Errorf("shader: global %q uses unsupported address space %d", g.Name, g.Space)
		}
	}
	return nil
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
func (p *Program) reflectUniforms(lim compileLimits) error {
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

	checker := uniformTypeChecker{
		types:       p.Module.Types,
		metrics:     make([]uniformTypeMetrics, len(p.Module.Types)),
		maxBytes:    lim.maxUserUniformBytes,
		maxElements: lim.maxUserUniformDwords,
		uniformOnly: true,
	}
	size, scalars, _, err := checker.measure(user.Type, 0)
	if err != nil {
		return err
	}
	if ls := uint64(layouter.Layout(user.Type).Size); ls > size {
		size = ls
	}
	paddedSize := (size + 15) / 16 * 16
	if paddedSize > lim.maxUserUniformBytes {
		return fmt.Errorf("shader: user uniform block size %d bytes exceeds maximum %d bytes", paddedSize, lim.maxUserUniformBytes)
	}
	p.UniformDwordCount = int(paddedSize / 4)
	maxBytes := int(paddedSize)

	if s, ok := p.Module.Types[user.Type].Inner.(ir.StructType); ok {
		p.Uniforms = make([]Uniform, 0, len(s.Members))
		for _, m := range s.Members {
			ms := int(checker.metrics[m.Type].scalars)
			u := Uniform{
				Name:  m.Name,
				Slots: make([]int, 0, ms),
				Kinds: make([]ir.ScalarKind, 0, ms),
			}
			p.collectSlots(&u, m.Type, int(m.Offset), maxBytes)
			p.Uniforms = append(p.Uniforms, u)
		}
		return nil
	}
	u := Uniform{
		Name:  user.Name,
		Slots: make([]int, 0, int(scalars)),
		Kinds: make([]ir.ScalarKind, 0, int(scalars)),
	}
	p.collectSlots(&u, user.Type, 0, maxBytes)
	p.Uniforms = append(p.Uniforms, u)
	return nil
}

type uniformTypeMetrics struct {
	state   uint8 // 0 = unseen, 1 = visiting, 2 = measured
	height  uint8
	size    uint64
	scalars uint64
}

type uniformTypeChecker struct {
	types       []ir.Type
	metrics     []uniformTypeMetrics
	maxBytes    uint64
	maxElements uint64
	uniformOnly bool
}

// measure computes the byte size, leaf element count, and composite nesting height of t using
// overflow-checked arithmetic, memoized per type handle so checking is O(|Types|) even for
// exponential struct DAGs or giant fixed-size arrays.
func (c *uniformTypeChecker) measure(t ir.TypeHandle, depth int) (size, scalars uint64, height uint8, err error) {
	if depth > maxUniformTypeDepth {
		return 0, 0, 0, fmt.Errorf("shader: composite type nesting exceeds maximum depth %d", maxUniformTypeDepth)
	}
	if int(t) >= len(c.types) {
		return 0, 0, 0, fmt.Errorf("shader: invalid type handle %d", t)
	}
	m := &c.metrics[t]
	switch m.state {
	case 1:
		return 0, 0, 0, errors.New("shader: cyclic type")
	case 2:
		if depth+int(m.height) > maxUniformTypeDepth {
			return 0, 0, 0, fmt.Errorf("shader: composite type nesting exceeds maximum depth %d", maxUniformTypeDepth)
		}
		return m.size, m.scalars, m.height, nil
	}
	m.state = 1

	switch inner := c.types[t].Inner.(type) {
	case ir.ScalarType:
		if c.uniformOnly && inner.Width != 4 {
			return 0, 0, 0, fmt.Errorf("shader: unsupported scalar width %d in uniform block", inner.Width)
		}
		w := max(uint64(inner.Width), 1)
		size, scalars, height = w, 1, 0
	case ir.VectorType:
		if c.uniformOnly && inner.Scalar.Width != 4 {
			return 0, 0, 0, fmt.Errorf("shader: unsupported vector scalar width %d in uniform block", inner.Scalar.Width)
		}
		w := max(uint64(inner.Scalar.Width), 1)
		count := uint64(inner.Size)
		size, scalars, height = w*count, count, 0
	case ir.MatrixType:
		if c.uniformOnly && inner.Scalar.Width != 4 {
			return 0, 0, 0, fmt.Errorf("shader: unsupported matrix scalar width %d in uniform block", inner.Scalar.Width)
		}
		w := max(uint64(inner.Scalar.Width), 1)
		colStride := 2 * w
		if inner.Rows > 2 {
			colStride = 4 * w
		}
		cols, rows := uint64(inner.Columns), uint64(inner.Rows)
		size, scalars, height = cols*colStride, cols*rows, 0
	case ir.AtomicType:
		if c.uniformOnly {
			return 0, 0, 0, errors.New("shader: atomic type is not allowed in uniform block")
		}
		w := max(uint64(inner.Scalar.Width), 1)
		size, scalars, height = w, 1, 0
	case ir.ImageType, ir.SamplerType:
		if c.uniformOnly {
			return 0, 0, 0, fmt.Errorf("shader: unsupported type %T in uniform block", inner)
		}
		size, scalars, height = 0, 0, 0
	case ir.PointerType:
		if c.uniformOnly {
			return 0, 0, 0, errors.New("shader: pointer type is not allowed in uniform block")
		}
		// Measuring the pointee validates its size and cycle-freeness without charging pointer
		// indirection against composite struct/array nesting depth.
		if _, _, _, err := c.measure(inner.Base, depth); err != nil {
			return 0, 0, 0, err
		}
		size, scalars, height = 8, 1, 0
	case ir.ValuePointerType:
		if c.uniformOnly {
			return 0, 0, 0, errors.New("shader: pointer type is not allowed in uniform block")
		}
		size, scalars, height = 8, 1, 0
	case ir.ArrayType:
		if inner.Size.Constant == nil {
			return 0, 0, 0, errors.New("shader: runtime-sized array is not supported")
		}
		n := uint64(*inner.Size.Constant)
		if n == 0 {
			if c.uniformOnly {
				return 0, 0, 0, errors.New("shader: zero-length array is not allowed in uniform block")
			}
			size, scalars, height = 0, 0, 1
			break
		}
		elemSize, elemScalars, elemHeight, err := c.measure(inner.Base, depth+1)
		if err != nil {
			return 0, 0, 0, err
		}
		if int(elemHeight)+1 > maxUniformTypeDepth {
			return 0, 0, 0, fmt.Errorf("shader: composite type nesting exceeds maximum depth %d", maxUniformTypeDepth)
		}
		stride := uint64(inner.Stride)
		if c.uniformOnly {
			if stride == 0 || stride < elemSize {
				return 0, 0, 0, fmt.Errorf("shader: invalid array stride %d for element size %d in uniform block", stride, elemSize)
			}
		} else if stride < elemSize {
			stride = elemSize
		}
		hi, lo := bits.Mul64(n, stride)
		if hi != 0 || lo > c.maxBytes {
			return 0, 0, 0, fmt.Errorf("shader: type size exceeds maximum size of %d bytes", c.maxBytes)
		}
		sHi, sLo := bits.Mul64(n, elemScalars)
		if sHi != 0 || sLo > c.maxElements {
			return 0, 0, 0, fmt.Errorf("shader: type element count exceeds maximum %d elements", c.maxElements)
		}
		size, scalars, height = lo, sLo, elemHeight+1
	case ir.StructType:
		if len(inner.Members) == 0 {
			if c.uniformOnly {
				return 0, 0, 0, errors.New("shader: empty struct is not allowed in uniform block")
			}
			size, scalars, height = 0, 0, 1
			break
		}
		var maxEnd, sumSize, totalScalars uint64
		var maxChildHeight uint8
		for _, mem := range inner.Members {
			mSize, mScalars, mHeight, err := c.measure(mem.Type, depth+1)
			if err != nil {
				return 0, 0, 0, err
			}
			if mHeight > maxChildHeight {
				maxChildHeight = mHeight
			}
			end, carry := bits.Add64(uint64(mem.Offset), mSize, 0)
			if carry != 0 || end > c.maxBytes {
				return 0, 0, 0, fmt.Errorf("shader: type size exceeds maximum size of %d bytes", c.maxBytes)
			}
			sumSize, carry = bits.Add64(sumSize, mSize, 0)
			if carry != 0 || sumSize > c.maxBytes {
				return 0, 0, 0, fmt.Errorf("shader: type size exceeds maximum size of %d bytes", c.maxBytes)
			}
			totalScalars, carry = bits.Add64(totalScalars, mScalars, 0)
			if carry != 0 || totalScalars > c.maxElements {
				return 0, 0, 0, fmt.Errorf("shader: type element count exceeds maximum %d elements", c.maxElements)
			}
			if end > maxEnd {
				maxEnd = end
			}
		}
		if int(maxChildHeight)+1 > maxUniformTypeDepth {
			return 0, 0, 0, fmt.Errorf("shader: composite type nesting exceeds maximum depth %d", maxUniformTypeDepth)
		}
		if sumSize > maxEnd {
			maxEnd = sumSize
		}
		if span := uint64(inner.Span); span > maxEnd {
			if span > c.maxBytes {
				return 0, 0, 0, fmt.Errorf("shader: type size exceeds maximum size of %d bytes", c.maxBytes)
			}
			maxEnd = span
		}
		size, scalars, height = maxEnd, totalScalars, maxChildHeight+1
	default:
		return 0, 0, 0, fmt.Errorf("shader: unsupported type %T", inner)
	}

	if size > c.maxBytes {
		return 0, 0, 0, fmt.Errorf("shader: type size exceeds maximum size of %d bytes", c.maxBytes)
	}
	if scalars > c.maxElements {
		return 0, 0, 0, fmt.Errorf("shader: type element count exceeds maximum %d elements", c.maxElements)
	}
	m.state = 2
	m.height = height
	m.size = size
	m.scalars = scalars
	return size, scalars, height, nil
}

// validateModuleTypes bounds every constant-sized aggregate in the compacted module (private
// globals, function locals, arguments, return values, constants, overrides, and zero/composite
// expressions) against lim.maxAggregateBytes / lim.maxAggregateElements individually and
// lim.maxModuleAggregateBytes / lim.maxModuleAggregateElements in total.
func (p *Program) validateModuleTypes(lim compileLimits) error {
	checker := uniformTypeChecker{
		types:       p.Module.Types,
		metrics:     make([]uniformTypeMetrics, len(p.Module.Types)),
		maxBytes:    lim.maxAggregateBytes,
		maxElements: lim.maxAggregateElements,
	}

	// Bound every reachable type in the compacted type arena individually first.
	for i := range p.Module.Types {
		if _, _, _, err := checker.measure(ir.TypeHandle(i), 0); err != nil {
			return err
		}
	}

	var totalBytes, totalElements uint64
	account := func(t ir.TypeHandle) error {
		sz, el, _, err := checker.measure(t, 0)
		if err != nil {
			return err
		}
		var carry uint64
		totalBytes, carry = bits.Add64(totalBytes, sz, 0)
		if carry != 0 || totalBytes > lim.maxModuleAggregateBytes {
			return fmt.Errorf("shader: total module aggregate size exceeds maximum size of %d bytes", lim.maxModuleAggregateBytes)
		}
		totalElements, carry = bits.Add64(totalElements, el, 0)
		if carry != 0 || totalElements > lim.maxModuleAggregateElements {
			return fmt.Errorf("shader: total module aggregate element count exceeds maximum %d elements", lim.maxModuleAggregateElements)
		}
		return nil
	}

	accountExprs := func(exprs []ir.Expression) error {
		for i := range exprs {
			switch kind := exprs[i].Kind.(type) {
			case ir.ExprZeroValue:
				if err := account(kind.Type); err != nil {
					return err
				}
			case ir.ExprCompose:
				if err := account(kind.Type); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for i := range p.Module.GlobalVariables {
		g := &p.Module.GlobalVariables[i]
		if g.Space == ir.SpacePrivate {
			if err := account(g.Type); err != nil {
				return err
			}
		}
	}
	for i := range p.Module.Constants {
		if err := account(p.Module.Constants[i].Type); err != nil {
			return err
		}
	}
	for i := range p.Module.Overrides {
		if err := account(p.Module.Overrides[i].Ty); err != nil {
			return err
		}
	}
	if err := accountExprs(p.Module.GlobalExpressions); err != nil {
		return err
	}

	checkFunc := func(fn *ir.Function) error {
		for i := range fn.Arguments {
			if err := account(fn.Arguments[i].Type); err != nil {
				return err
			}
		}
		if fn.Result != nil {
			if err := account(fn.Result.Type); err != nil {
				return err
			}
		}
		for i := range fn.LocalVars {
			if err := account(fn.LocalVars[i].Type); err != nil {
				return err
			}
		}
		return accountExprs(fn.Expressions)
	}
	for i := range p.Module.Functions {
		if err := checkFunc(&p.Module.Functions[i]); err != nil {
			return err
		}
	}
	for i := range p.Module.EntryPoints {
		if err := checkFunc(&p.Module.EntryPoints[i].Function); err != nil {
			return err
		}
	}
	return nil
}

func (p *Program) collectSlots(u *Uniform, t ir.TypeHandle, offset, maxBytes int) {
	add := func(byteOffset int, kind ir.ScalarKind) {
		if byteOffset < 0 || byteOffset >= maxBytes || byteOffset%4 != 0 {
			panic(fmt.Sprintf("shader: uniform slot byte offset %d out of bounds [0, %d)", byteOffset, maxBytes))
		}
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
			p.collectSlots(u, inner.Base, offset+i*int(inner.Stride), maxBytes)
		}
	case ir.StructType:
		for _, m := range inner.Members {
			p.collectSlots(u, m.Type, offset+int(m.Offset), maxBytes)
		}
	}
}
