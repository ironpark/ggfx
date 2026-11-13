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

// DrawTransformed draws the compiled SvgPath with p while applying transform t.
func (svgp *SvgPath) DrawTransformed(p *Painter, opacity float64, t rasterx.Matrix2D) {
	svgp.drawThrough(p, opacity, t, nil)
}

// drawThrough draws the path as DrawTransformed does, its paint let
// through mask where it is not nil; see clip.go. It leaves svgp as it is,
// so that a document can be drawn from goroutines at once.
func (svgp *SvgPath) drawThrough(p *Painter, opacity float64, t rasterx.Matrix2D, mask *image.Alpha) {
	r, rf, adder := p.Dasher, &p.Dasher.Filler, &p.adder
	adder.M = t.Mult(svgp.mAdder.M)
	fill := !svgp.fillerColor.none()
	if mask != nil {
		// Outside the mask, nothing is let through: no need to ask.
		p.Scanner.SetClip(mask.Rect)
		defer p.Scanner.SetClip(image.ZR)
	}
	if fill {
		r.Clear()
		adder.Adder = rf
		svgp.Path.AddTo(adder)
		rf.SetWinding(svgp.UseNonZeroWinding)
		rf.SetPaint(through(svgp.fillerColor.paint(svgp.Path, svgp.FillOpacity*opacity, adder.M), mask))
		rf.Draw()
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
		svgp.Path.AddTo(adder)
		r.SetPaint(through(svgp.linerColor.paint(svgp.Path, svgp.LineOpacity*opacity, adder.M), mask))
		r.Draw()
	}
}

// paint is p as drawn at opacity for path, in the user space m maps: a
// gradient in that space or in the path's bounding box there, its fill's,
// for the stroke too, which SVG leaves out of it.
func (p paint) paint(path rasterx.Path, opacity float64, m rasterx.Matrix2D) rasterx.Paint {
	if p.grad == nil {
		return rasterx.ColorPaint(rasterx.ApplyOpacity(p.color, opacity))
	}
	g := *p.grad
	if g.Units == rasterx.ObjectBoundingBox {
		minX, minY, maxX, maxY := path.Bounds()
		if minX >= maxX || minY >= maxY {
			// A box without width or height, as a line's, has no place
			// for the gradient, which SVG says is then not drawn.
			return rasterx.Paint{}
		}
		g.Bounds.X, g.Bounds.Y = minX, minY
		g.Bounds.W, g.Bounds.H = maxX-minX, maxY-minY
	}
	return g.GetColorFunctionUS(opacity, m)
}
