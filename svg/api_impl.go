// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg

import (
	"fmt"
	"image"
	"math"
	"time"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/oksvg"
	"github.com/ironpark/ggfx/internal/rasterx"
	"github.com/ironpark/ggfx/internal/svganim"
)

func geoMToMatrix(geoM ggfx.GeoM) rasterx.Matrix2D {
	return rasterx.Matrix2D{
		A: geoM.Element(0, 0), C: geoM.Element(0, 1), E: geoM.Element(0, 2),
		B: geoM.Element(1, 0), D: geoM.Element(1, 1), F: geoM.Element(1, 2),
	}
}

func fromAnimRect(r svganim.Rect) Rect {
	return Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}
}

func convertPaint(p Paint) oksvg.PaintOverride {
	switch p.kind {
	case paintNone:
		return oksvg.PaintOverride{Kind: oksvg.PaintOvNone}
	case paintColor:
		return oksvg.PaintOverride{Kind: oksvg.PaintOvColor, Color: p.color}
	case paintCurrentColor:
		return oksvg.PaintOverride{Kind: oksvg.PaintOvCurrentColor}
	case paintInherit:
		return oksvg.PaintOverride{Kind: oksvg.PaintOvInherit}
	default:
		return oksvg.PaintOverride{}
	}
}

func convertNodeOverride(o NodeOverride) oksvg.NodeOverride {
	out := oksvg.NodeOverride{
		HasTranslate:     o.mask&ovTranslate != 0,
		HasRotate:        o.mask&ovRotate != 0,
		HasScale:         o.mask&ovScale != 0,
		HasSkew:          o.mask&ovSkew != 0,
		HasPivot:         o.mask&ovPivot != 0,
		HasMatrix:        o.mask&ovMatrix != 0,
		Tx:               o.tx,
		Ty:               o.ty,
		Rotate:           o.rotate,
		Sx:               o.sx,
		Sy:               o.sy,
		SkewX:            o.skewX,
		SkewY:            o.skewY,
		Pivot:            oksvg.PivotOverride{Box: o.pivot.box, X: o.pivot.x, Y: o.pivot.y},
		HasOpacity:       o.mask&ovOpacity != 0,
		Opacity:          o.opacity,
		HasHidden:        o.mask&ovHidden != 0,
		Hidden:           o.hidden,
		HasVisible:       o.mask&ovVisible != 0,
		Visible:          o.visible,
		HasFill:          o.mask&ovFill != 0,
		Fill:             convertPaint(o.fill),
		HasStroke:        o.mask&ovStroke != 0,
		Stroke:           convertPaint(o.stroke),
		HasColor:         o.mask&ovColor != 0,
		Color:            o.color,
		HasFillOpacity:   o.mask&ovFillOpacity != 0,
		FillOpacity:      o.fillOpacity,
		HasStrokeOpacity: o.mask&ovStrokeOpacity != 0,
		StrokeOpacity:    o.strokeOpacity,
		HasStrokeWidth:   o.mask&ovStrokeWidth != 0,
		StrokeWidth:      o.strokeWidth,
		HasDash:          o.mask&ovDash != 0,
		Dash:             o.dash,
		HasDashOffset:    o.mask&ovDashOffset != 0,
		DashOffset:       o.dashOffset,
		HasTrim:          o.mask&ovTrim != 0,
		TrimStart:        o.trimStart,
		TrimEnd:          o.trimEnd,
		TrimOffset:       o.trimOffset,
	}
	if out.HasMatrix {
		out.Matrix = geoMToMatrix(o.matrix)
	}
	return out
}

func convertEvalOptions(opts *DrawOptions) *oksvg.EvalOptions {
	if opts == nil {
		return &oksvg.EvalOptions{Animate: false}
	}
	eo := &oksvg.EvalOptions{
		Time:         opts.Time.Seconds(),
		CurrentColor: opts.CurrentColor,
		Animate:      true,
	}
	if len(opts.Overrides) > 0 {
		m := make(map[string]oksvg.NodeOverride, len(opts.Overrides))
		for id, o := range opts.Overrides {
			if !o.IsZero() {
				m[id] = convertNodeOverride(o)
			}
		}
		if len(m) > 0 {
			eo.Overrides = m
		}
	}
	return eo
}

func nodeIDImpl(d *Document, i int32) string {
	return d.icon.NodeID(i)
}

func nodeTagImpl(d *Document, i int32) string {
	return d.icon.NodeTag(i)
}

func nodeLocalBoundsImpl(d *Document, i int32) (Rect, bool) {
	r, ok := d.icon.NodeLocalBounds(i)
	return fromAnimRect(r), ok
}

func docAnimatedImpl(d *Document) bool {
	return d.icon.Animated()
}

func docTimelineImpl(d *Document) Timeline {
	tl := d.icon.Timeline()
	return Timeline{
		End:       tl.End,
		Infinite:  tl.Infinite,
		LoopStart: tl.LoopStart,
		Loop:      tl.Loop,
		Span:      tl.Span,
	}
}

func docUsesCurrentColorImpl(d *Document) bool {
	return d.icon.UsesCurrentColor()
}

func docNodeImpl(d *Document, id string) (int32, bool) {
	return d.icon.Node(id)
}

func docNodeIDsImpl(d *Document) []string {
	return d.icon.NodeIDs()
}

func docBoundsImpl(d *Document, id string, opts *DrawOptions) (Rect, bool) {
	r, ok := d.icon.Bounds(id, convertEvalOptions(opts))
	return fromAnimRect(r), ok
}

func docNextChangeImpl(d *Document, t time.Duration) (time.Duration, bool) {
	nextSec, ok := d.icon.NextChange(t.Seconds())
	if !ok || math.IsNaN(nextSec) || math.IsInf(nextSec, 0) {
		return 0, false
	}
	ns := math.Ceil(nextSec * float64(time.Second))
	if ns >= float64(math.MaxInt64) {
		if t >= time.Duration(math.MaxInt64) {
			return 0, false
		}
		return time.Duration(math.MaxInt64), true
	}
	dur := time.Duration(ns)
	if dur <= t {
		if t >= time.Duration(math.MaxInt64) {
			return 0, false
		}
		dur = t + 1
	}
	return dur, true
}

func docDrawWithErrImpl(d *Document, dst *image.RGBA, geoM ggfx.GeoM, opts *DrawOptions) error {
	b := dst.Bounds()
	if b.Empty() {
		return nil
	}
	p := painters.Get()
	p.Scanner.Dest, p.Scanner.Targ = dst, b
	p.Dasher.SetBounds(b.Dx(), b.Dy())
	err := d.icon.DrawWithErr(p, geoMToMatrix(geoM), convertEvalOptions(opts))
	p.Release()
	painters.Put(p)
	if err != nil {
		return fmt.Errorf("svg: %w", err)
	}
	return nil
}

func newRendererImpl(d *Document) any {
	if d.icon == nil {
		return nil
	}
	return d.icon.NewRenderer()
}

func rendererDrawErrImpl(r *Renderer, dst *image.RGBA, geoM ggfx.GeoM, opts *DrawOptions) error {
	ir, _ := r.impl.(*oksvg.Renderer)
	if ir == nil {
		ir = r.doc.icon.NewRenderer()
		r.impl = ir
	}
	if err := ir.DrawErr(dst, geoMToMatrix(geoM), convertEvalOptions(opts)); err != nil {
		return fmt.Errorf("svg: %w", err)
	}
	return nil
}

func rendererReleaseImpl(r *Renderer) {
	if ir, ok := r.impl.(*oksvg.Renderer); ok && ir != nil {
		ir.Release()
	}
}
