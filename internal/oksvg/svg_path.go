// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"image"
	"math"

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
		// A stroke is as wide as its transform makes it: user units, not
		// pixels. Where the transform keeps shapes, turning and scaling
		// alike in every direction, the stroke of the placed path at the
		// scaled width is that stroke. Elsewhere the outline itself has to
		// be transformed, being wider along one axis than the other. It is
		// then made of the path scaled alike in both, by as much as the
		// transform stretches where it stretches most, so that the rest
		// of the transform, which places it, only ever shrinks: what the
		// stroker rounds to a 64th stays within a 64th of a pixel, and
		// curves are flattened finely enough for the pixels they cover.
		m := adder.M
		sq := m.A*m.A + m.B*m.B + m.C*m.C + m.D*m.D
		det := m.A*m.D - m.B*m.C
		scale := math.Sqrt((sq + math.Sqrt(max(sq*sq-4*det*det, 0))) / 2)
		if scale == 0 {
			return // nothing of the picture is left to see
		}
		dash := svgp.Dash
		if len(dash) > 0 {
			p.dash = p.dash[:0]
			for _, d := range dash {
				p.dash = append(p.dash, d*scale)
			}
			dash = p.dash
		}
		const near = 1e-9
		turned := math.Abs(m.A-m.D) <= near*scale && math.Abs(m.B+m.C) <= near*scale
		flipped := math.Abs(m.A+m.D) <= near*scale && math.Abs(m.B-m.C) <= near*scale
		if !turned && !flipped {
			adder.M = rasterx.Matrix2D{A: scale, D: scale}
			p.outline.Adder = &r.Filler
			p.outline.M = rasterx.Matrix2D{A: m.A / scale, B: m.B / scale, C: m.C / scale, D: m.D / scale, E: m.E, F: m.F}
			r.Out = &p.outline
			defer func() { r.Out = nil }()
		}
		// The stroker halves the width: rounded to a whole half, it is
		// within a 64th of what was asked for.
		r.SetStroke(2*fixed.Int26_6(math.Round(svgp.LineWidth*scale*32)),
			fixed.Int26_6(svgp.MiterLimit*64), leadLineCap, lineCap,
			lineGap, svgp.LineJoin, dash, svgp.DashOffset*scale)
		svgp.Path.AddTo(adder)
		r.SetPaint(through(svgp.linerColor.paint(svgp.Path, svgp.LineOpacity*opacity, m), mask))
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
