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
	if !svgp.fillerColor.none() {
		r.Clear()
		rf := &r.Filler
		rf.SetWinding(svgp.UseNonZeroWinding)
		adder.Adder = rf // This allows transformations to be applied
		svgp.Path.AddTo(&adder)
		rf.SetPaint(through(svgp.fillerColor.paint(rf.Scanner, svgp.FillOpacity*opacity, adder.M), mask))
		rf.Draw()
		// default is true
		rf.SetWinding(true)
	}
	if !svgp.linerColor.none() {
		r.Clear()
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
		r.SetPaint(through(svgp.linerColor.paint(r.Scanner, svgp.LineOpacity*opacity, adder.M), mask))
		r.Draw()
	}
}

// paint is p as s draws it, at opacity, for the path s holds, which m
// placed: a gradient in the path's bounding box, or in the user space m
// maps.
func (p paint) paint(s rasterx.Scanner, opacity float64, m rasterx.Matrix2D) rasterx.Paint {
	if p.grad == nil {
		return rasterx.ColorPaint(rasterx.ApplyOpacity(p.color, opacity))
	}
	g := *p.grad
	if g.Units == rasterx.ObjectBoundingBox {
		fRect := s.GetPathExtent()
		mnx, mny := float64(fRect.Min.X)/64, float64(fRect.Min.Y)/64
		mxx, mxy := float64(fRect.Max.X)/64, float64(fRect.Max.Y)/64
		g.Bounds.X, g.Bounds.Y = mnx, mny
		g.Bounds.W, g.Bounds.H = mxx-mnx, mxy-mny
	}
	return g.GetColorFunctionUS(opacity, m)
}
