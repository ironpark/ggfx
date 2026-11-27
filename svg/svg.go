// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

// Package svg draws SVG documents on the CPU with
// [github.com/ironpark/svgx], taking a [ggfx.GeoM] where svgx takes its own
// matrix. It reads a subset of SVG 1.1: paths, shapes, groups, use,
// transforms, gradients, opacity, presentation attributes, stylesheets, clip
// paths, masks and markers, with SMIL and CSS animations. What it leaves out,
// such as filter and image, is listed in svgx's README.
//
// The types that say what to draw, such as [DrawOptions] and [NodeOverride],
// are svgx's own, under the names they have here.
package svg

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"time"

	"github.com/ironpark/svgx"

	"github.com/ironpark/ggfx"
)

// The types of svgx that this package hands on as they are.
type (
	// Rect is an axis-aligned rectangle in document or local user space.
	Rect = svgx.Rect
	// Timeline summarizes the timing structure of an animated SVG document.
	Timeline = svgx.Timeline
	// Node is an opaque, comparable handle to a named element in a Document
	// (not to an individual <use> instance). The zero value is invalid.
	Node = svgx.Node
	// DrawOptions configures an animated or overridden render pass. A nil
	// *DrawOptions draws the document's static base rendering, as Draw does,
	// which is also what the Static field asks for.
	DrawOptions = svgx.DrawOptions
	// Paint specifies an SVG fill or stroke paint override.
	Paint = svgx.Paint
	// Pivot is the point a NodeOverride's rotation, scale and skew turn about.
	Pivot = svgx.Pivot
	// Overrides maps element IDs to programmatic property overrides.
	Overrides = svgx.Overrides
	// NodeOverride is a set of property overrides for one element, built by
	// chaining its methods from the zero value. Its Matrix method takes an
	// svgx.Matrix, which Matrix makes of a ggfx.GeoM.
	NodeOverride = svgx.NodeOverride
)

var (
	// ErrNoBinaryForm is why MarshalBinary has nothing to return for a document.
	ErrNoBinaryForm = svgx.ErrNoBinaryForm
	// ErrNestingDepth is returned by Parse and the drawing methods when
	// element nesting or <use> chaining exceeds the allowed maximum depth.
	ErrNestingDepth = svgx.ErrNestingDepth
	// ErrExpansionLimit is returned by Parse and the drawing methods when the
	// elements produced by <use>, <clipPath>, <mask>, or <marker> expansion
	// exceed the expansion budget.
	ErrExpansionLimit = svgx.ErrExpansionLimit
)

// NoPaint represents SVG "none" (disables fill or stroke).
func NoPaint() Paint { return svgx.NoPaint() }

// ColorPaint paints with c. A nil c is equivalent to NoPaint().
func ColorPaint(c color.Color) Paint { return svgx.ColorPaint(c) }

// CurrentColorPaint paints with the element's currentColor.
func CurrentColorPaint() Paint { return svgx.CurrentColorPaint() }

// InheritPaint paints as the element's parent does.
func InheritPaint() Paint { return svgx.InheritPaint() }

// PivotAt is a pivot at (x, y) in the element's own user space.
func PivotAt(x, y float64) Pivot { return svgx.PivotAt(x, y) }

// PivotBox is a pivot at the fractions (fx, fy) of the element's bounding box.
func PivotBox(fx, fy float64) Pivot { return svgx.PivotBox(fx, fy) }

// Matrix returns geoM as the matrix svgx draws with, for NodeOverride.Matrix.
func Matrix(geoM ggfx.GeoM) svgx.Matrix {
	return svgx.NewMatrix(
		geoM.Element(0, 0), geoM.Element(0, 1),
		geoM.Element(1, 0), geoM.Element(1, 1),
		geoM.Element(0, 2), geoM.Element(1, 2))
}

// static is what a nil *DrawOptions means here: the base rendering.
var static = &DrawOptions{Static: true}

func drawOptions(opts *DrawOptions) *DrawOptions {
	if opts == nil {
		return static
	}
	return opts
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("svg: %w", err)
}

// Document is a parsed SVG document. Its methods can be called from any
// goroutine.
type Document struct {
	doc *svgx.Document
}

// Parse reads an SVG document. It fails on what it cannot draw rather than
// drawing without it.
func Parse(r io.Reader) (*Document, error) {
	doc, err := svgx.Parse(r)
	if err != nil {
		return nil, wrap(err)
	}
	return &Document{doc: doc}, nil
}

func (d *Document) svgx() *svgx.Document {
	if d == nil {
		return nil
	}
	return d.doc
}

// MarshalBinary returns what parsing arrived at, for a document known ahead
// of time, an icon of a library say, to be kept without its source and read
// back by UnmarshalBinary without parsing it. The form is versioned: a later
// ggfx reads what an earlier one wrote.
func (d *Document) MarshalBinary() ([]byte, error) {
	b, err := d.svgx().MarshalBinary()
	return b, wrap(err)
}

// UnmarshalBinary makes d the document MarshalBinary wrote as data, which
// draws as the one it was written from does. On error d is unchanged.
func (d *Document) UnmarshalBinary(data []byte) error {
	doc := new(svgx.Document)
	if err := doc.UnmarshalBinary(data); err != nil {
		return wrap(err)
	}
	d.doc = doc
	return nil
}

// ViewBox returns the rectangle of the document's user space that its
// viewBox attribute names as the picture.
func (d *Document) ViewBox() (x, y, width, height float64) {
	r := d.svgx().ViewBox()
	return r.X, r.Y, r.W, r.H
}

// Animated reports whether d contains any SMIL or CSS animations.
func (d *Document) Animated() bool { return d.svgx().Animated() }

// Timeline returns the document's summarized animation timeline.
func (d *Document) Timeline() Timeline { return d.svgx().Timeline() }

// UsesCurrentColor reports whether d references the SVG currentColor keyword.
func (d *Document) UsesCurrentColor() bool { return d.svgx().UsesCurrentColor() }

// Node returns a handle to the first element in document order with the given id.
func (d *Document) Node(id string) (Node, bool) { return d.svgx().Node(id) }

// NodeIDs returns the unique element IDs in document order as a fresh slice.
func (d *Document) NodeIDs() []string { return d.svgx().NodeIDs() }

// Bounds returns the bounding box of the element with id in document user space
// evaluated at opts (nil means static base state). It returns (Rect{}, false) if
// id is not rendered or if evaluation exceeds ErrNestingDepth or ErrExpansionLimit.
func (d *Document) Bounds(id string, opts *DrawOptions) (Rect, bool) {
	return d.svgx().Bounds(id, drawOptions(opts))
}

// NextChange returns the next timestamp strictly greater than t where a discrete,
// delayed, or stepped animation changes state. Returns (0, false) for continuous
// or settled timelines.
func (d *Document) NextChange(t time.Duration) (time.Duration, bool) {
	return d.svgx().NextChange(t)
}

// Draw draws the document's static base rendering over dst, with geoM
// mapping its user space to dst's pixels. It discards any draw-time limit
// error; use DrawErr to observe ErrNestingDepth or ErrExpansionLimit.
func (d *Document) Draw(dst *image.RGBA, geoM ggfx.GeoM) {
	_ = d.DrawWithErr(dst, geoM, nil)
}

// DrawErr is Draw, returning ErrNestingDepth or ErrExpansionLimit if
// evaluation exceeds a limit; dst is then left untouched.
func (d *Document) DrawErr(dst *image.RGBA, geoM ggfx.GeoM) error {
	return d.DrawWithErr(dst, geoM, nil)
}

// DrawWith draws d over dst with geoM and opts. It mutates nothing in d and is
// safe to call concurrently from multiple goroutines. It discards any draw-time
// limit error; use DrawWithErr to observe ErrNestingDepth or ErrExpansionLimit.
func (d *Document) DrawWith(dst *image.RGBA, geoM ggfx.GeoM, opts *DrawOptions) {
	_ = d.DrawWithErr(dst, geoM, opts)
}

// DrawWithErr draws d over dst with geoM and opts. It mutates nothing in d and
// is safe to call concurrently from multiple goroutines. If evaluation exceeds
// the nesting or expansion limit (returning ErrNestingDepth or ErrExpansionLimit),
// dst is left untouched, Bounds reports false for that state, and subsequent
// under-budget frames render normally.
func (d *Document) DrawWithErr(dst *image.RGBA, geoM ggfx.GeoM, opts *DrawOptions) error {
	if dst == nil {
		return nil
	}
	return wrap(d.svgx().Draw(dst, Matrix(geoM), drawOptions(opts)))
}

// Renderer is a reusable, stateful renderer bound to a single Document.
// It is NOT goroutine-safe; create one per widget/consumer and call Release when done.
type Renderer struct {
	r *svgx.Renderer
}

// NewRenderer creates a reusable Renderer for d.
func (d *Document) NewRenderer() *Renderer {
	return &Renderer{r: d.svgx().NewRenderer()}
}

// Draw draws the renderer's document over dst with geoM and opts. It discards
// any draw-time limit error; use DrawErr to observe ErrNestingDepth or
// ErrExpansionLimit.
func (r *Renderer) Draw(dst *image.RGBA, geoM ggfx.GeoM, opts *DrawOptions) {
	_ = r.DrawErr(dst, geoM, opts)
}

// DrawErr draws the renderer's document over dst with geoM and opts. If
// evaluation exceeds the nesting or expansion limit (returning ErrNestingDepth
// or ErrExpansionLimit), dst is left untouched, Bounds reports false for that
// state, and subsequent under-budget frames render normally.
func (r *Renderer) DrawErr(dst *image.RGBA, geoM ggfx.GeoM, opts *DrawOptions) error {
	if r == nil || r.r == nil || dst == nil {
		return nil
	}
	return wrap(r.r.Draw(dst, Matrix(geoM), drawOptions(opts)))
}

// Release frees cached painters and scratch buffers held by r.
func (r *Renderer) Release() {
	if r != nil && r.r != nil {
		r.r.Release()
	}
}
