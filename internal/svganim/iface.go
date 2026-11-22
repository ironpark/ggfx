// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

// Package svganim parses and evaluates SMIL animations, CSS @keyframes, CSS
// stylesheets, and SVG/CSS property values for internal/oksvg.
package svganim

import (
	"encoding/xml"
	"time"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// ElemID identifies an element in an SVG scene graph. Negative values mean none.
type ElemID int32

// Rect is an axis-aligned rectangle in user space.
type Rect struct {
	X, Y, W, H float64
}

// Timeline summarizes the timing structure of an animated SVG document.
type Timeline struct {
	End       time.Duration
	Infinite  bool
	LoopStart time.Duration
	Loop      time.Duration
	Span      time.Duration
}

// PropID identifies an animatable or styleable SVG/CSS property.
type PropID uint16

const (
	PropNone PropID = iota
	PropFill
	PropStroke
	PropColor
	PropOpacity
	PropFillOpacity
	PropStrokeOpacity
	PropStrokeWidth
	PropStrokeDasharray
	PropStrokeDashoffset
	PropStrokeLinecap
	PropStrokeLinejoin
	PropStrokeMiterlimit
	PropFillRule
	PropClipRule
	PropVisibility
	PropDisplay
	PropTransform
	PropTransformOrigin
	PropTransformBox
	PropX
	PropY
	PropWidth
	PropHeight
	PropRx
	PropRy
	PropR
	PropCx
	PropCy
	PropX1
	PropY1
	PropX2
	PropY2
	PropPoints
	PropD
	// Reserved for future versions; do not reuse.
	PropOffset
	PropStopColor
	PropStopOpacity
	PropMotion
	PropCount
)

// PropMask is a bitset of PropID values.
type PropMask [(PropCount + 63) / 64]uint64

// Set marks p as present in m.
func (m *PropMask) Set(p PropID) {
	if p < PropCount {
		m[p/64] |= 1 << (p % 64)
	}
}

// Has reports whether p is present in m.
func (m PropMask) Has(p PropID) bool {
	return p < PropCount && (m[p/64]&(1<<(p%64))) != 0
}

// Any reports whether any bit in m is set.
func (m PropMask) Any() bool {
	for _, w := range m {
		if w != 0 {
			return true
		}
	}
	return false
}

// Syntax selects SVG presentation attribute syntax vs CSS declaration syntax.
type Syntax uint8

const (
	SyntaxSVGAttr Syntax = iota
	SyntaxCSS
)

// ValueKind tags the active payload of Value.
type ValueKind uint8

const (
	ValNone ValueKind = iota
	ValNumber
	ValLength
	ValColor
	ValPaint
	ValKeyword
	ValNumberList
	ValTransformList
	ValPathData
	ValOrigin
)

// UnitKind identifies a numeric length or angle unit.
type UnitKind uint8

const (
	UnitNone UnitKind = iota
	UnitPx
	UnitPct
	UnitEm
	UnitRem
	UnitDeg
	UnitRad
	UnitGrad
	UnitTurn
)

// PaintKind identifies an SVG paint value.
type PaintKind uint8

const (
	PaintNone PaintKind = iota
	PaintColor
	PaintCurrentColor
	PaintURL
	PaintInherit
)

// TransformOp identifies a 2D transform function.
type TransformOp uint8

const (
	TfTranslate TransformOp = iota
	TfScale
	TfRotate
	TfSkewX
	TfSkewY
	TfMatrix
)

// TransformFn is a single transform function call with up to 6 numeric parameters.
type TransformFn struct {
	Op    TransformOp
	Args  [6]float64
	Units [6]UnitKind
	N     uint8
}

// OriginAxis specifies one axis of transform-origin.
type OriginAxis struct {
	Val  float64
	Unit UnitKind
}

// Value is a tagged union for SVG/CSS property values.
type Value struct {
	Kind    ValueKind
	Num     float64
	Unit    UnitKind
	Color   [4]float64 // straight sRGB RGBA in [0, 1]
	Paint   PaintKind
	URL     string
	Keyword string
	List    []float64
	Xf      []TransformFn
	Origin  [2]OriginAxis
	Raw     string
}

// EvalCtx provides viewport, fill-box, and color context during value evaluation.
type EvalCtx struct {
	Viewport Rect
	FillBox  func() Rect
	Color    [4]float64 // straight sRGB RGBA in [0, 1]
	FontSize float64
}

// Diag records a non-fatal diagnostic for an unsupported or malformed animation/CSS rule.
type Diag struct {
	Elem    ElemID
	Message string
}

// Decl is a single CSS property declaration produced by stylesheet cascade matching.
type Decl struct {
	Name      string
	Value     string
	Important bool
}

// SceneView exposes read-only document structure for CSS selector matching and SMIL compilation.
type SceneView interface {
	Elems() int
	Tag(e ElemID) string
	ID(e ElemID) string
	Classes(e ElemID) []string
	Parent(e ElemID) ElemID
	ByID(id string) []ElemID
	RawAttr(e ElemID, name string) (string, bool)
}

// Builder collects SMIL elements and <style> blocks during XML parsing and compiles them.
type Builder interface {
	SMILElement(se xml.StartElement, parent ElemID, inDefs bool)
	Stylesheet(text string)
	Compile(sv SceneView) (*AnimSet, *Stylesheet, []Diag)
}

// Stylesheet holds compiled CSS rules for static cascade matching.
type Stylesheet struct {
	// Populated by svganim implementation.
	impl any
}

// DeclsFor returns matching CSS declarations for e sorted in cascade order
// (normal rules by specificity/order, then !important rules by specificity/order).
func (s *Stylesheet) DeclsFor(sv SceneView, e ElemID) []Decl {
	if s == nil || s.impl == nil {
		return nil
	}
	return declsForImpl(s, sv, e)
}

// ClassDecls returns legacy single-.class rules for backward-compatible static parsing.
func (s *Stylesheet) ClassDecls() map[string][]Decl {
	if s == nil || s.impl == nil {
		return nil
	}
	return classDeclsImpl(s)
}

// AnimSet holds compiled immutable SMIL and CSS animations for a Document.
type AnimSet struct {
	impl any
}

// Animated reports whether any SMIL or CSS animation was parsed.
func (a *AnimSet) Animated() bool {
	if a == nil || a.impl == nil {
		return false
	}
	return animatedImpl(a)
}

// AnimatedProps returns the bitmask of properties animated on element e.
func (a *AnimSet) AnimatedProps(e ElemID) PropMask {
	if a == nil || a.impl == nil {
		return PropMask{}
	}
	return animatedPropsImpl(a, e)
}

// Eval evaluates all active SMIL and CSS animations on (e, p) at document time t (seconds).
// base is the element's computed static/inherited value for p.
// Returns (value, true) if an animation overrides/modifies base, or (_, false) if inactive.
func (a *AnimSet) Eval(e ElemID, p PropID, t float64, base Value, ctx *EvalCtx) (Value, bool) {
	if a == nil || a.impl == nil {
		return Value{}, false
	}
	return evalImpl(a, e, p, t, base, ctx)
}

// Timeline returns the document's summarized animation timeline.
func (a *AnimSet) Timeline() Timeline {
	if a == nil || a.impl == nil {
		return Timeline{}
	}
	return timelineImpl(a)
}

// NextChange returns the next timestamp > t where discrete/delayed/stepped animations change.
func (a *AnimSet) NextChange(t float64) (float64, bool) {
	if a == nil || a.impl == nil {
		return 0, false
	}
	return nextChangeImpl(a, t)
}

// UsesCurrentColor reports whether any compiled animation track references currentColor.
func (a *AnimSet) UsesCurrentColor() bool {
	if a == nil || a.impl == nil {
		return false
	}
	return usesCurrentColorImpl(a)
}

// ParseDecls parses a CSS inline style declaration list respecting quotes, comments, and parentheses.
func ParseDecls(raw string) []Decl {
	return parseDeclsImpl(raw)
}

// IsSMILTag reports whether tag is an SVG SMIL animation element name.
func IsSMILTag(tag string) bool {
	switch tag {
	case "animate", "set", "animateTransform", "animateMotion", "animateColor", "mpath", "discard":
		return true
	default:
		return false
	}
}

// PropByName maps an SVG/CSS attribute or property name (case-sensitive for camelCase
// SVG attributes, case-insensitive fallback for CSS names) to its PropID.
func PropByName(name string) PropID {
	return propByNameImpl(name)
}

// NewBuilder creates a new Builder for parsing SMIL elements and CSS stylesheets.
func NewBuilder() Builder {
	return newBuilderImpl()
}

// ParseValue parses a raw SVG attribute or CSS declaration string for property p.
func ParseValue(p PropID, raw string, syn Syntax) (Value, error) {
	return parseValueImpl(p, raw, syn)
}

// ParseColor parses an SVG/CSS color string (hex #rgb/#rgba/#rrggbb/#rrggbbaa,
// rgb/rgba/hsl/hsla comma & space syntax, named colors, transparent, currentColor).
func ParseColor(raw string) (col [4]float64, kind PaintKind, err error) {
	return parseColorImpl(raw)
}

// ToMatrix converts a ValTransformList value to a rasterx.Matrix2D in the given context.
func (v Value) ToMatrix(ctx *EvalCtx) rasterx.Matrix2D {
	return toMatrixImpl(v, ctx)
}
