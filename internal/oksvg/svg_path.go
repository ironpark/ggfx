// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"image"
	"image/color"

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
// through mask where it is not nil; see clip.go.
func (svgp *SvgPath) drawThrough(r *rasterx.Dasher, opacity float64, t rasterx.Matrix2D, mask *image.Alpha) {
	m := svgp.mAdder.M
	svgp.mAdder.M = t.Mult(m)
	// Restore the untransformed matrix, and let go of r, which holds the
	// destination image.
	defer func() { svgp.mAdder.M, svgp.mAdder.Adder = m, nil }()
	if svgp.fillerColor != nil {
		r.Clear()
		rf := &r.Filler
		rf.SetWinding(svgp.UseNonZeroWinding)
		svgp.mAdder.Adder = rf // This allows transformations to be applied
		svgp.Path.AddTo(&svgp.mAdder)

		switch fillerColor := svgp.fillerColor.(type) {
		case color.Color:
			rf.SetColor(through(rasterx.ApplyOpacity(fillerColor, svgp.FillOpacity*opacity), mask))
		case rasterx.Gradient:
			if fillerColor.Units == rasterx.ObjectBoundingBox {
				fRect := rf.Scanner.GetPathExtent()
				mnx, mny := float64(fRect.Min.X)/64, float64(fRect.Min.Y)/64
				mxx, mxy := float64(fRect.Max.X)/64, float64(fRect.Max.Y)/64
				fillerColor.Bounds.X, fillerColor.Bounds.Y = mnx, mny
				fillerColor.Bounds.W, fillerColor.Bounds.H = mxx-mnx, mxy-mny
			}
			rf.SetColor(through(fillerColor.GetColorFunctionUS(svgp.FillOpacity*opacity, svgp.mAdder.M), mask))
		}
		rf.Draw()
		// default is true
		rf.SetWinding(true)
	}
	if svgp.linerColor != nil {
		r.Clear()
		svgp.mAdder.Adder = r
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
		svgp.Path.AddTo(&svgp.mAdder)
		switch linerColor := svgp.linerColor.(type) {
		case color.Color:
			r.SetColor(through(rasterx.ApplyOpacity(linerColor, svgp.LineOpacity*opacity), mask))
		case rasterx.Gradient:
			if linerColor.Units == rasterx.ObjectBoundingBox {
				fRect := r.Scanner.GetPathExtent()
				mnx, mny := float64(fRect.Min.X)/64, float64(fRect.Min.Y)/64
				mxx, mxy := float64(fRect.Max.X)/64, float64(fRect.Max.Y)/64
				linerColor.Bounds.X, linerColor.Bounds.Y = mnx, mny
				linerColor.Bounds.W, linerColor.Bounds.H = mxx-mnx, mxy-mny
			}
			r.SetColor(through(linerColor.GetColorFunctionUS(svgp.LineOpacity*opacity, svgp.mAdder.M), mask))
		}
		r.Draw()
	}
}
