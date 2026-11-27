// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"image"
	"image/color"
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
	svgp.drawThrough(p, opacity, t, clipResult{})
}

// drawThrough draws the path as DrawTransformed does, clipped or masked by
// clip where it is set; see clip.go. It leaves svgp as it is,
// so that a document can be drawn from goroutines at once.
func (svgp *SvgPath) drawThrough(p *Painter, opacity float64, t rasterx.Matrix2D, clip clipResult) {
	svgp.drawThroughTrim(p, opacity, t, clip, trimSpec{}, nil)
}

func (svgp *SvgPath) drawThroughTrim(p *Painter, opacity float64, t rasterx.Matrix2D, clip clipResult, trim trimSpec, lt *lengthTable) {
	if clip.empty {
		return
	}
	r, rf, adder := p.Dasher, &p.Dasher.Filler, &p.adder
	adder.M = t.Mult(svgp.m)
	fill := !svgp.fillerColor.none()
	if clip.set {
		p.Scanner.SetClip(clip.rect)
		p.Scanner.SetAlphaMask(clip.mask)
		defer func() {
			p.Scanner.SetClip(image.Rectangle{})
			p.Scanner.SetAlphaMask(nil)
		}()
	}
	if fill {
		r.Clear()
		adder.Adder = rf
		svgp.Path.AddTo(adder)
		rf.SetWinding(svgp.UseNonZeroWinding)
		rf.SetPaint(svgp.fillerColor.paint(p, svgp.Path, svgp.FillOpacity*opacity, adder.M))
		rf.Draw()
	}
	if !svgp.linerColor.none() && svgp.LineWidth > 0 {
		tMode, trimA, trimB := trim.classify()
		if tMode == trimEmpty {
			return
		}
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
		sq := float64(m.A*m.A) + float64(m.B*m.B) + float64(m.C*m.C) + float64(m.D*m.D)
		det := float64(m.A*m.D) - float64(m.B*m.C)
		scale := math.Sqrt((sq + math.Sqrt(max(float64(sq*sq)-float64(4*det*det), 0))) / 2)
		if scale == 0 {
			return // nothing of the picture is left to see
		}
		boundsOnly := p.Scanner.Dest == nil && p.Scanner.DestAlpha == nil
		dash := svgp.Dash
		if boundsOnly {
			// Measurement pass uses the undashed stroke envelope so dash
			// generation and per-draw transition charging happen only once,
			// during the paint pass.
			dash = nil
		} else if len(dash) > 0 {
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
		if tMode == trimCut {
			if lt == nil {
				lt = buildLengthTable(svgp.Path)
			}
			emitTrimmedStroke(lt, trimA, trimB, adder.M, r, len(r.Dashes) > 0, svgp.DashOffset, scale)
		} else {
			svgp.Path.AddTo(adder)
		}
		r.SetPaint(svgp.linerColor.paint(p, svgp.Path, svgp.LineOpacity*opacity, m))
		r.Draw()
	}
}

// paint is p as drawn at opacity for path, in the user space m maps: a
// gradient in that space or in the path's bounding box there, its fill's,
// for the stroke too, which SVG leaves out of it.
func (p paint) paint(painter *Painter, path rasterx.Path, opacity float64, m rasterx.Matrix2D) rasterx.Paint {
	if p.current {
		c := painter.currentColor
		if c == nil {
			c = color.NRGBA{0, 0, 0, 0xff}
		}
		return rasterx.ColorPaint(rasterx.ApplyOpacity(c, opacity))
	}
	if p.grad == nil {
		return rasterx.ColorPaint(rasterx.ApplyOpacity(p.color, opacity))
	}
	resolved := painter.resolveGrad(p.grad, painter.currentColor)
	g := *resolved
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
	table := painter.gradTable(resolved, opacity)
	return g.FillSpan(&painter.gradSpan, table, opacity, m)
}
