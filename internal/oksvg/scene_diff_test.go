// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"golang.org/x/image/math/fixed"

	"github.com/ironpark/ggfx/internal/rasterx"
)

func TestArcLengthLinesAndCurves(t *testing.T) {
	// 1. Straight 3-4-5 triangle (open and closed).
	var p rasterx.Path
	p.Start(rasterx.ToFixedP(0, 0))
	p.Line(rasterx.ToFixedP(3, 0))
	p.Line(rasterx.ToFixedP(3, 4))
	p.Stop(true)
	lt := buildLengthTable(p)
	if math.Abs(lt.total-12.0) > 1e-3 {
		t.Errorf("3-4-5 triangle length = %v, want 12", lt.total)
	}
	if len(lt.subpaths) != 1 || !lt.subpaths[0].closed || len(lt.subpaths[0].segs) != 3 {
		t.Errorf("unexpected subpath structure: %+v", lt.subpaths)
	}

	// 2. Circle of radius 20 -> circumference = 2*pi*20 = 125.6637...
	var cp rasterx.Path
	buildEllipse(&cp, 50, 50, 20, 20)
	clt := buildLengthTable(cp)
	wantCirc := 2 * math.Pi * 20
	if math.Abs(clt.total-wantCirc) > 0.05 {
		t.Errorf("circle r=20 arc length = %v, want ~%v", clt.total, wantCirc)
	}

	// 3. Quadratic Bezier: verify inversion s->t and de Casteljau splitting.
	var qp rasterx.Path
	qp.Start(rasterx.ToFixedP(0, 0))
	qp.QuadBezier(rasterx.ToFixedP(50, 100), rasterx.ToFixedP(100, 0))
	qlt := buildLengthTable(qp)
	if qlt.total <= 100 || qlt.total >= 224 {
		t.Errorf("quad length %v out of expected range (100, 224)", qlt.total)
	}
	qseg := &qlt.subpaths[0].segs[0]
	for _, frac := range []float64{0.1, 0.25, 0.5, 0.75, 0.9} {
		targetS := frac * qseg.len
		tv := qseg.paramAtLength(targetS)
		gotS := adaptiveSegLen(qseg, 0, tv, 1e-6, 0)
		if math.Abs(gotS-targetS) > 1e-2 {
			t.Errorf("quad paramAtLength(%.2f*L): got arcLen=%.4f, want %.4f", frac, gotS, targetS)
		}
		// Split at [0, tv] and [tv, 1]: lengths must sum to qseg.len.
		k1, pts1 := splitSeg(qseg, 0, tv)
		k2, pts2 := splitSeg(qseg, tv, 1)
		s1 := arcSeg{kind: k1, pts: pts1}
		s2 := arcSeg{kind: k2, pts: pts2}
		l1 := adaptiveSegLen(&s1, 0, 1, 1e-5, 0)
		l2 := adaptiveSegLen(&s2, 0, 1, 1e-5, 0)
		if math.Abs((l1+l2)-qseg.len) > 1e-2 {
			t.Errorf("quad split sum = %.4f, want %.4f", l1+l2, qseg.len)
		}
		// Shared junction point continuity.
		if math.Hypot(pts1[4]-pts2[0], pts1[5]-pts2[1]) > 1e-9 {
			t.Errorf("quad split discontinuity at t=%v", tv)
		}
	}

	// 4. Cubic Bezier S-curve: inversion and split.
	var cub rasterx.Path
	cub.Start(rasterx.ToFixedP(0, 0))
	cub.CubeBezier(rasterx.ToFixedP(30, 80), rasterx.ToFixedP(70, -80), rasterx.ToFixedP(100, 0))
	cubLT := buildLengthTable(cub)
	cseg := &cubLT.subpaths[0].segs[0]
	for _, frac := range []float64{0.2, 0.5, 0.8} {
		targetS := frac * cseg.len
		tv := cseg.paramAtLength(targetS)
		gotS := adaptiveSegLen(cseg, 0, tv, 1e-6, 0)
		if math.Abs(gotS-targetS) > 1e-2 {
			t.Errorf("cubic paramAtLength(%.2f*L): got arcLen=%.4f, want %.4f", frac, gotS, targetS)
		}
		k1, pts1 := splitSeg(cseg, 0.25, 0.75)
		if k1 != arcCubic {
			t.Fatalf("expected arcCubic, got %v", k1)
		}
		s1 := arcSeg{kind: k1, pts: pts1}
		lMid := adaptiveSegLen(&s1, 0, 1, 1e-5, 0)
		wantMid := adaptiveSegLen(cseg, 0.25, 0.75, 1e-5, 0)
		if math.Abs(lMid-wantMid) > 1e-2 {
			t.Errorf("cubic sub-interval [0.25,0.75] len = %.4f, want %.4f", lMid, wantMid)
		}
	}
}

func TestDasherSetPhaseBetweenSubpaths(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	scanner := rasterx.NewScannerGV(40, 20, img, img.Bounds())
	d := rasterx.NewDasher(40, 20, scanner)
	d.SetStroke(4*64, 4*64, rasterx.ButtCap, nil, rasterx.FlatGap, rasterx.Miter, []float64{10, 10}, 0)
	d.SetPaint(rasterx.ColorPaint(color.NRGBA{255, 255, 255, 255}))

	// Subpath 1 at y=5 with phase 0 -> [0..10] lit, [10..20] gap.
	d.SetPhase(0)
	d.Start(fixed.Point26_6{X: 0, Y: 5 * 64})
	d.Line(fixed.Point26_6{X: 40 * 64, Y: 5 * 64})
	d.Stop(false)

	// Subpath 2 at y=15 with phase 10 -> [0..10] gap, [10..20] lit.
	d.SetPhase(10)
	d.Start(fixed.Point26_6{X: 0, Y: 15 * 64})
	d.Line(fixed.Point26_6{X: 40 * 64, Y: 15 * 64})
	d.Stop(false)

	d.Draw()

	if img.RGBAAt(5, 5).A != 255 || img.RGBAAt(15, 5).A != 0 {
		t.Errorf("subpath 1 (phase 0): (5,5)=%v (15,5)=%v", img.RGBAAt(5, 5), img.RGBAAt(15, 5))
	}
	if img.RGBAAt(5, 15).A != 0 || img.RGBAAt(15, 15).A != 255 {
		t.Errorf("subpath 2 (phase 10): (5,15)=%v (15,15)=%v", img.RGBAAt(5, 15), img.RGBAAt(15, 15))
	}
}

func TestSMILInterceptionB1AndCSSKeyframes(t *testing.T) {
	// B1: fill="freeze" inside <animate>, nested <mpath> inside <animateMotion>,
	// and SMIL elements inside <defs>, <clipPath>, <mask>, <marker> must all parse cleanly.
	// B5: @keyframes, /* comments */, #id, tag selectors, !important, and CSS transform with deg.
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
<style>
  /* comment with { braces } */
  @keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
  .box { fill: #f00 !important; }
  #c1 { stroke: #0f0; }
  rect { stroke-width: 2; }
</style>
<defs>
  <g id="gdef">
    <rect x="0" y="0" width="10" height="10" fill="#00f">
      <animate attributeName="x" from="0" to="10" dur="1s" fill="freeze"/>
    </rect>
  </g>
  <clipPath id="cp">
    <rect x="0" y="0" width="50" height="50">
      <animate attributeName="width" from="10" to="50" dur="1s" fill="freeze"/>
    </rect>
  </clipPath>
  <mask id="mk">
    <rect x="0" y="0" width="100" height="100" fill="#fff">
      <set attributeName="opacity" to="0.5" begin="0s"/>
    </rect>
  </mask>
  <marker id="mrk" markerWidth="4" markerHeight="4" refX="2" refY="2">
    <circle cx="2" cy="2" r="2" fill="#000">
      <animate attributeName="r" from="1" to="2" dur="1s" fill="freeze"/>
    </circle>
  </marker>
</defs>
<rect id="r1" class="box" x="10" y="10" width="20" height="20" style="transform: rotate(45deg)">
  <animate attributeName="opacity" from="0" to="1" dur="1s" fill="freeze"/>
  <animateTransform attributeName="transform" type="rotate" from="0" to="90" dur="1s" fill="freeze"/>
  <animateMotion dur="2s" fill="freeze"><mpath href="#p1"/></animateMotion>
</rect>
<circle id="c1" cx="60" cy="60" r="15" fill="#0f0" clip-path="url(#cp)" mask="url(#mk)">
  <animate attributeName="r" values="5;15" dur="1s" fill="freeze"/>
</circle>
<path id="p1" d="M10 80 L90 80" stroke="#000" stroke-width="2" marker-end="url(#mrk)"/>
<use href="#gdef" x="70" y="10"/>
</svg>`

	icon, err := ReadIconStream(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ReadIconStream failed on SMIL/CSS doc: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	p := NewPainter(100, 100, img, img.Bounds())
	icon.Draw(p, 1)
	if len(icon.NodeIDs()) == 0 {
		t.Error("expected non-empty NodeIDs")
	}
}
