// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"image"

	"golang.org/x/image/math/fixed"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// SvgPath binds a style to a path.
type SvgPath struct {
	PathStyle
	Path rasterx.Path
}

// DrawTransformed draws the compiled SvgPath into the Dasher while applying transform t.
func (svgp *SvgPath) DrawTransformed(r *rasterx.Dasher, opacity float64, t rasterx.Matrix2D) {
	svgp.drawThrough(r, opacity, t, nil)
}

// drawThrough draws the path as DrawTransformed does, its paint let
// through mask where it is not nil; see clip.go. It leaves svgp as it is,
// so that a document can be drawn from goroutines at once.
func (svgp *SvgPath) drawThrough(r *rasterx.Dasher, opacity float64, t rasterx.Matrix2D, mask *image.Alpha) {
	adder := rasterx.MatrixAdder{M: t.Mult(svgp.mAdder.M)}
	rf := &r.Filler
	// box is the fill's extent: a gradient's bounding box, for the stroke
	// too, which SVG leaves out of it.
	var box fixed.Rectangle26_6
	if !svgp.fillerColor.none() {
		r.Clear()
		rf.SetWinding(svgp.UseNonZeroWinding)
		adder.Adder = rf // This allows transformations to be applied
		svgp.Path.AddTo(&adder)
		box = rf.Scanner.GetPathExtent()
		rf.SetPaint(through(svgp.fillerColor.paint(box, svgp.FillOpacity*opacity, adder.M), mask))
		rf.Draw()
		// default is true
		rf.SetWinding(true)
	} else if g := svgp.linerColor.grad; g != nil && g.Units == rasterx.ObjectBoundingBox {
		r.Clear()
		adder.Adder = rf
		svgp.Path.AddTo(&adder)
		box = rf.Scanner.GetPathExtent()
	}
	if !svgp.linerColor.none() {
		r.Clear()
		// A stroke's outline overlaps itself where it turns: non-zero
		// fills it all, whatever the fill's rule.
		r.SetWinding(true)
		adder.Adder = r
		lineGap := svgp.LineGap
		if lineGap == nil {
			lineGap = defaultStyle.LineGap
		}
		lineCap := svgp.LineCap
		if lineCap == nil {
			lineCap = defaultStyle.LineCap
		}
		leadLineCap := lineCap
		if svgp.LeadLineCap != nil {
			leadLineCap = svgp.LeadLineCap
		}
		r.SetStroke(fixed.Int26_6(svgp.LineWidth*64),
			fixed.Int26_6(svgp.MiterLimit*64), leadLineCap, lineCap,
			lineGap, svgp.LineJoin, svgp.Dash, svgp.DashOffset)
		svgp.Path.AddTo(&adder)
		r.SetPaint(through(svgp.linerColor.paint(box, svgp.LineOpacity*opacity, adder.M), mask))
		r.Draw()
	}
}

// paint is p as drawn at opacity for a path whose fill's extent is box,
// which m placed: a gradient in that bounding box, or in the user space m
// maps.
func (p paint) paint(box fixed.Rectangle26_6, opacity float64, m rasterx.Matrix2D) rasterx.Paint {
	if p.grad == nil {
		return rasterx.ColorPaint(rasterx.ApplyOpacity(p.color, opacity))
	}
	g := *p.grad
	if g.Units == rasterx.ObjectBoundingBox {
		if box.Min.X >= box.Max.X || box.Min.Y >= box.Max.Y {
			// A box without width or height, as a line's, has no place
			// for the gradient, which SVG says is then not drawn.
			return rasterx.Paint{}
		}
		fRect := box
		mnx, mny := float64(fRect.Min.X)/64, float64(fRect.Min.Y)/64
		mxx, mxy := float64(fRect.Max.X)/64, float64(fRect.Max.Y)/64
		g.Bounds.X, g.Bounds.Y = mnx, mny
		g.Bounds.W, g.Bounds.H = mxx-mnx, mxy-mny
	}
	return g.GetColorFunctionUS(opacity, m)
}
