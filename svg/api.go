// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg

import (
	"image"
	"image/color"
	"math"
	"slices"
	"time"

	"github.com/ironpark/ggfx"
)

// Rect is an axis-aligned rectangle in document or local user space.
type Rect struct {
	X, Y, W, H float64
}

// Timeline summarizes the timing structure of an animated SVG document.
type Timeline struct {
	// End is the maximum end time over all finite animation tracks (0 if none).
	End time.Duration
	// Infinite reports whether at least one track repeats indefinitely.
	Infinite bool
	// LoopStart is the time from which the picture repeats periodically, valid when Loop > 0.
	LoopStart time.Duration
	// Loop is the periodic cycle length (LCM of indefinite tracks, alternate counts 2×dur),
	// or 0 if no finite periodic loop exists or the LCM exceeds 60s.
	Loop time.Duration
	// Span is the smallest duration by which every track has completed at least one
	// iteration and every finite track has ended (0 if not animated).
	Span time.Duration
}

// Node is an opaque, comparable handle to a named element in a Document
// (not to an individual <use> instance). The zero value is invalid.
type Node struct {
	d *Document
	i int32
}

// Valid reports whether n refers to an element in a Document.
func (n Node) Valid() bool { return n.d != nil && n.i >= 0 }

// ID returns the element's id attribute, or "" if invalid.
func (n Node) ID() string {
	if !n.Valid() {
		return ""
	}
	return nodeIDImpl(n.d, n.i)
}

// Tag returns the element's XML tag name, or "" if invalid.
func (n Node) Tag() string {
	if !n.Valid() {
		return ""
	}
	return nodeTagImpl(n.d, n.i)
}

// LocalBounds returns the element's static fill bounding box in its own local
// user space (for groups, the union of its children's fill boxes).
func (n Node) LocalBounds() (Rect, bool) {
	if !n.Valid() {
		return Rect{}, false
	}
	return nodeLocalBoundsImpl(n.d, n.i)
}

// Animated reports whether d contains any SMIL or CSS animations.
func (d *Document) Animated() bool {
	if d == nil || d.icon == nil {
		return false
	}
	return docAnimatedImpl(d)
}

// Timeline returns the document's summarized animation timeline.
func (d *Document) Timeline() Timeline {
	if d == nil || d.icon == nil {
		return Timeline{}
	}
	return docTimelineImpl(d)
}

// UsesCurrentColor reports whether d references the SVG currentColor keyword.
func (d *Document) UsesCurrentColor() bool {
	if d == nil || d.icon == nil {
		return false
	}
	return docUsesCurrentColorImpl(d)
}

// Node returns a handle to the first element in document order with the given id.
func (d *Document) Node(id string) (Node, bool) {
	if d == nil || d.icon == nil || id == "" {
		return Node{}, false
	}
	i, ok := docNodeImpl(d, id)
	if !ok {
		return Node{}, false
	}
	return Node{d: d, i: i}, true
}

// NodeIDs returns the unique element IDs in document order as a fresh slice.
func (d *Document) NodeIDs() []string {
	if d == nil || d.icon == nil {
		return nil
	}
	return docNodeIDsImpl(d)
}

// Bounds returns the bounding box of the element with id in document user space
// evaluated at opts (nil means static base state).
func (d *Document) Bounds(id string, opts *DrawOptions) (Rect, bool) {
	if d == nil || d.icon == nil || id == "" {
		return Rect{}, false
	}
	return docBoundsImpl(d, id, opts)
}

// NextChange returns the next timestamp strictly greater than t where a discrete,
// delayed, or stepped animation changes state. Returns (0, false) for continuous
// or settled timelines.
func (d *Document) NextChange(t time.Duration) (time.Duration, bool) {
	if d == nil || d.icon == nil {
		return 0, false
	}
	return docNextChangeImpl(d, t)
}

// DrawOptions configures an animated or overridden render pass.
// A nil *DrawOptions draws the document's static base rendering (equivalent to Document.Draw).
type DrawOptions struct {
	// Time is the document animation timestamp to evaluate.
	Time time.Duration
	// CurrentColor resolves SVG currentColor references at the document root (nil = black).
	CurrentColor color.Color
	// Overrides maps element IDs to programmatic property overrides.
	// Must not be mutated concurrently during DrawWith or Renderer.Draw.
	Overrides Overrides
}

// DrawWith draws d over dst with geoM and opts. It mutates nothing in d and is
// safe to call concurrently from multiple goroutines.
func (d *Document) DrawWith(dst *image.RGBA, geoM ggfx.GeoM, opts *DrawOptions) {
	if d == nil || d.icon == nil || dst == nil {
		return
	}
	if opts == nil {
		d.Draw(dst, geoM)
		return
	}
	docDrawWithImpl(d, dst, geoM, opts)
}

// Renderer is a reusable, stateful renderer bound to a single Document.
// It is NOT goroutine-safe; create one per widget/consumer and call Release when done.
type Renderer struct {
	doc  *Document
	impl any
}

// NewRenderer creates a reusable Renderer for d.
func (d *Document) NewRenderer() *Renderer {
	if d == nil {
		return &Renderer{}
	}
	return &Renderer{doc: d, impl: newRendererImpl(d)}
}

// Draw draws the renderer's document over dst with geoM and opts.
func (r *Renderer) Draw(dst *image.RGBA, geoM ggfx.GeoM, opts *DrawOptions) {
	if r == nil || r.doc == nil || r.doc.icon == nil || dst == nil {
		return
	}
	if opts == nil {
		r.doc.Draw(dst, geoM)
		return
	}
	rendererDrawImpl(r, dst, geoM, opts)
}

// Release frees cached painters and scratch buffers held by r.
func (r *Renderer) Release() {
	if r == nil || r.impl == nil {
		return
	}
	rendererReleaseImpl(r)
	r.impl = nil
}

// Paint specifies an SVG fill or stroke paint override.
type Paint struct {
	kind  paintKind
	color color.Color
}

type paintKind uint8

const (
	paintUnset paintKind = iota
	paintNone
	paintColor
	paintCurrentColor
	paintInherit
)

// NoPaint represents SVG "none" (disables fill or stroke).
func NoPaint() Paint { return Paint{kind: paintNone} }

// ColorPaint paints with c. A nil c is equivalent to NoPaint().
func ColorPaint(c color.Color) Paint {
	if c == nil {
		return NoPaint()
	}
	return Paint{kind: paintColor, color: c}
}

// CurrentColorPaint resolves the element's computed CSS color at draw time.
func CurrentColorPaint() Paint { return Paint{kind: paintCurrentColor} }

// InheritPaint inherits the parent element's computed paint.
func InheritPaint() Paint { return Paint{kind: paintInherit} }

// Equal reports whether p and q represent the same paint.
func (p Paint) Equal(q Paint) bool {
	if p.kind != q.kind {
		return false
	}
	if p.kind != paintColor {
		return true
	}
	return colorsEqual(p.color, q.color)
}

// Pivot specifies the origin for NodeOverride Rotate, Scale, and Skew transforms.
type Pivot struct {
	box  bool
	x, y float64
}

// PivotAt specifies a pivot point in the node's local user space, mapped into parent
// space through the node's static and animated transform.
func PivotAt(x, y float64) Pivot { return Pivot{box: false, x: x, y: y} }

// PivotBox specifies a pivot as fractions of the node's static local fill bounding box
// (e.g. PivotBox(0.5, 0.5) is the center).
func PivotBox(fx, fy float64) Pivot { return Pivot{box: true, x: fx, y: fy} }

// Equal reports whether p and q represent the same pivot.
func (p Pivot) Equal(q Pivot) bool {
	return p.box == q.box && floatEq(p.x, q.x) && floatEq(p.y, q.y)
}

// Overrides maps SVG element IDs to programmatic overrides.
type Overrides map[string]NodeOverride

// Equal reports whether o and p contain the same non-zero overrides.
func (o Overrides) Equal(p Overrides) bool {
	if len(o) == 0 && len(p) == 0 {
		return true
	}
	for k, v := range o {
		if v.IsZero() {
			continue
		}
		if !v.Equal(p[k]) {
			return false
		}
	}
	for k, v := range p {
		if v.IsZero() {
			continue
		}
		if !v.Equal(o[k]) {
			return false
		}
	}
	return true
}

type overrideBits uint32

const (
	ovTranslate overrideBits = 1 << iota
	ovRotate
	ovScale
	ovSkew
	ovPivot
	ovMatrix
	ovOpacity
	ovHidden
	ovVisible
	ovFill
	ovStroke
	ovColor
	ovFillOpacity
	ovStrokeOpacity
	ovStrokeWidth
	ovDash
	ovDashOffset
	ovTrim
)

// NodeOverride describes programmatic overrides for an SVG element (and all its <use> instances).
// The zero value leaves the element unchanged. All setters return a modified copy.
type NodeOverride struct {
	mask overrideBits

	tx, ty        float64
	rotate        float64
	sx, sy        float64
	skewX, skewY  float64
	pivot         Pivot
	matrix        ggfx.GeoM
	opacity       float64
	hidden        bool
	visible       bool
	fill          Paint
	stroke        Paint
	color         color.Color
	fillOpacity   float64
	strokeOpacity float64
	strokeWidth   float64
	dash          []float64
	dashOffset    float64
	trimStart     float64
	trimEnd       float64
	trimOffset    float64
}

// IsZero reports whether no override properties are set.
func (o NodeOverride) IsZero() bool { return o.mask == 0 }

// Translate adds a parent-space translation (dx, dy). Clears Matrix if set.
func (o NodeOverride) Translate(dx, dy float64) NodeOverride {
	o.mask = (o.mask &^ ovMatrix) | ovTranslate
	o.tx, o.ty = dx, dy
	return o
}

// Rotate sets clockwise rotation in radians around Pivot in parent user space. Clears Matrix if set.
func (o NodeOverride) Rotate(rad float64) NodeOverride {
	o.mask = (o.mask &^ ovMatrix) | ovRotate
	o.rotate = rad
	return o
}

// Scale sets non-uniform scale around Pivot in parent user space. Clears Matrix if set.
func (o NodeOverride) Scale(sx, sy float64) NodeOverride {
	o.mask = (o.mask &^ ovMatrix) | ovScale
	o.sx, o.sy = sx, sy
	return o
}

// Skew sets skew angles in radians around Pivot in parent user space. Clears Matrix if set.
func (o NodeOverride) Skew(radX, radY float64) NodeOverride {
	o.mask = (o.mask &^ ovMatrix) | ovSkew
	o.skewX, o.skewY = radX, radY
	return o
}

// Pivot sets the transform origin for Rotate, Scale, and Skew.
func (o NodeOverride) Pivot(p Pivot) NodeOverride {
	o.mask |= ovPivot
	o.pivot = p
	return o
}

// Matrix sets an explicit parent-space transform matrix, replacing Translate/Rotate/Scale/Skew.
func (o NodeOverride) Matrix(m ggfx.GeoM) NodeOverride {
	o.mask = (o.mask &^ (ovTranslate | ovRotate | ovScale | ovSkew)) | ovMatrix
	o.matrix = m
	return o
}

// Opacity multiplies the node's evaluated opacity by a.
func (o NodeOverride) Opacity(a float64) NodeOverride {
	o.mask |= ovOpacity
	o.opacity = a
	return o
}

// Hidden sets display:none on the node (skips subtree, bounds, and markers).
func (o NodeOverride) Hidden(h bool) NodeOverride {
	o.mask |= ovHidden
	o.hidden = h
	return o
}

// Visible sets inherited CSS visibility on the node.
func (o NodeOverride) Visible(v bool) NodeOverride {
	o.mask |= ovVisible
	o.visible = v
	return o
}

// Fill overrides the node's fill paint (inherited by descendants without their own fill).
func (o NodeOverride) Fill(p Paint) NodeOverride {
	o.mask |= ovFill
	o.fill = p
	return o
}

// Stroke overrides the node's stroke paint.
func (o NodeOverride) Stroke(p Paint) NodeOverride {
	o.mask |= ovStroke
	o.stroke = p
	return o
}

// Color overrides CSS color (currentColor) for the node's subtree.
func (o NodeOverride) Color(c color.Color) NodeOverride {
	o.mask |= ovColor
	o.color = c
	return o
}

// FillOpacity overrides the node's fill-opacity.
func (o NodeOverride) FillOpacity(a float64) NodeOverride {
	o.mask |= ovFillOpacity
	o.fillOpacity = a
	return o
}

// StrokeOpacity overrides the node's stroke-opacity.
func (o NodeOverride) StrokeOpacity(a float64) NodeOverride {
	o.mask |= ovStrokeOpacity
	o.strokeOpacity = a
	return o
}

// StrokeWidth overrides the node's stroke-width.
func (o NodeOverride) StrokeWidth(w float64) NodeOverride {
	o.mask |= ovStrokeWidth
	o.strokeWidth = w
	return o
}

// Dash overrides stroke-dasharray (a defensive copy of d is stored). Passing no arguments clears dashing.
func (o NodeOverride) Dash(d ...float64) NodeOverride {
	o.mask |= ovDash
	if len(d) == 0 {
		o.dash = nil
	} else {
		o.dash = slices.Clone(d)
	}
	return o
}

// DashOffset overrides stroke-dashoffset.
func (o NodeOverride) DashOffset(v float64) NodeOverride {
	o.mask |= ovDashOffset
	o.dashOffset = v
	return o
}

// Trim trims the stroked geometry to the normalized arc-length window [start, end] shifted by offset.
// Passing NaN for start or end leaves Trim unset.
func (o NodeOverride) Trim(start, end, offset float64) NodeOverride {
	if math.IsNaN(start) || math.IsNaN(end) {
		o.mask &^= ovTrim
		return o
	}
	if math.IsNaN(offset) {
		offset = 0
	}
	o.mask |= ovTrim
	o.trimStart, o.trimEnd, o.trimOffset = start, end, offset
	return o
}

// Equal reports whether o and p are identical overrides.
func (o NodeOverride) Equal(p NodeOverride) bool {
	if o.mask != p.mask {
		return false
	}
	if o.mask == 0 {
		return true
	}
	if o.mask&ovTranslate != 0 && (!floatEq(o.tx, p.tx) || !floatEq(o.ty, p.ty)) {
		return false
	}
	if o.mask&ovRotate != 0 && !floatEq(o.rotate, p.rotate) {
		return false
	}
	if o.mask&ovScale != 0 && (!floatEq(o.sx, p.sx) || !floatEq(o.sy, p.sy)) {
		return false
	}
	if o.mask&ovSkew != 0 && (!floatEq(o.skewX, p.skewX) || !floatEq(o.skewY, p.skewY)) {
		return false
	}
	if o.mask&ovPivot != 0 && !o.pivot.Equal(p.pivot) {
		return false
	}
	if o.mask&ovMatrix != 0 {
		for r := range 2 {
			for c := range 3 {
				if !floatEq(o.matrix.Element(r, c), p.matrix.Element(r, c)) {
					return false
				}
			}
		}
	}
	if o.mask&ovOpacity != 0 && !floatEq(o.opacity, p.opacity) {
		return false
	}
	if o.mask&ovHidden != 0 && o.hidden != p.hidden {
		return false
	}
	if o.mask&ovVisible != 0 && o.visible != p.visible {
		return false
	}
	if o.mask&ovFill != 0 && !o.fill.Equal(p.fill) {
		return false
	}
	if o.mask&ovStroke != 0 && !o.stroke.Equal(p.stroke) {
		return false
	}
	if o.mask&ovColor != 0 && !colorsEqual(o.color, p.color) {
		return false
	}
	if o.mask&ovFillOpacity != 0 && !floatEq(o.fillOpacity, p.fillOpacity) {
		return false
	}
	if o.mask&ovStrokeOpacity != 0 && !floatEq(o.strokeOpacity, p.strokeOpacity) {
		return false
	}
	if o.mask&ovStrokeWidth != 0 && !floatEq(o.strokeWidth, p.strokeWidth) {
		return false
	}
	if o.mask&ovDash != 0 && !slices.EqualFunc(o.dash, p.dash, floatEq) {
		return false
	}
	if o.mask&ovDashOffset != 0 && !floatEq(o.dashOffset, p.dashOffset) {
		return false
	}
	if o.mask&ovTrim != 0 && (!floatEq(o.trimStart, p.trimStart) || !floatEq(o.trimEnd, p.trimEnd) || !floatEq(o.trimOffset, p.trimOffset)) {
		return false
	}
	return true
}

func floatEq(a, b float64) bool {
	return a == b || (math.IsNaN(a) && math.IsNaN(b))
}

func colorsEqual(a, b color.Color) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}
