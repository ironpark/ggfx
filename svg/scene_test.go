// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/svg"
)

func drawOpts(d *svg.Document, r image.Rectangle, g ggfx.GeoM, opts *svg.DrawOptions) *image.RGBA {
	dst := image.NewRGBA(r)
	d.DrawWith(dst, g, opts)
	return dst
}

func TestNodeQueriesAndLocalBounds(t *testing.T) {
	d := parse(t, svgDoc("0 0 100 100", `
<defs>
  <rect id="defRect" x="0" y="0" width="10" height="20"/>
</defs>
<g id="grp">
  <rect id="r1" x="10" y="20" width="30" height="40" fill="#f00"/>
  <circle id="c1" cx="70" cy="50" r="10" fill="#0f0"/>
</g>
<use id="u1" href="#defRect" x="5" y="5"/>`))

	wantIDs := []string{"defRect", "grp", "r1", "c1", "u1"}
	gotIDs := d.NodeIDs()
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("NodeIDs = %v, want %v", gotIDs, wantIDs)
	}
	// Mutating returned slice must not affect subsequent calls.
	gotIDs[0] = "mutated"
	if d.NodeIDs()[0] != "defRect" {
		t.Error("NodeIDs did not return a fresh slice")
	}

	n, ok := d.Node("r1")
	if !ok || !n.Valid() || n.ID() != "r1" || n.Tag() != "rect" {
		t.Fatalf("Node(r1) = (%v, %v), ID=%q Tag=%q", n, ok, n.ID(), n.Tag())
	}
	lb, ok := n.LocalBounds()
	if !ok || math.Abs(lb.X-10) > 0.1 || math.Abs(lb.Y-20) > 0.1 || math.Abs(lb.W-30) > 0.1 || math.Abs(lb.H-40) > 0.1 {
		t.Errorf("r1.LocalBounds = (%+v, %v), want {10 20 30 40}", lb, ok)
	}

	grp, ok := d.Node("grp")
	if !ok || grp.Tag() != "g" {
		t.Fatalf("Node(grp) failed")
	}
	glb, ok := grp.LocalBounds()
	// Union of [10,20,40,60] and [60,40,80,60] = [10,20,80,60] => W=70, H=40.
	if !ok || math.Abs(glb.X-10) > 0.2 || math.Abs(glb.Y-20) > 0.2 || math.Abs(glb.W-70) > 0.2 || math.Abs(glb.H-40) > 0.2 {
		t.Errorf("grp.LocalBounds = (%+v, %v), want {10 20 70 40}", glb, ok)
	}

	// Missing node & invalid zero Node.
	if _, ok := d.Node("missing"); ok {
		t.Error("Node(missing) returned ok=true")
	}
	var zero svg.Node
	if zero.Valid() || zero.ID() != "" || zero.Tag() != "" {
		t.Error("zero Node should be invalid")
	}
	if _, ok := zero.LocalBounds(); ok {
		t.Error("zero Node LocalBounds returned ok=true")
	}
}

func TestOverrideTransformsAndBounds(t *testing.T) {
	// Node has static rotate(90) around (50, 50).
	// Parent-space Translate(20, 0) must move it RIGHT (+x), NOT down (+y).
	d := parse(t, svgDoc("0 0 100 100", `
<rect id="r" x="40" y="45" width="20" height="10" transform="rotate(90 50 50)" fill="#f00"/>`))

	// Static bounds after 90-deg rotation around (50,50): rect [40..60]x[45..55] -> [45..55]x[40..60].
	sb, ok := d.Bounds("r", nil)
	if !ok || math.Abs(sb.X-45) > 0.2 || math.Abs(sb.Y-40) > 0.2 || math.Abs(sb.W-10) > 0.2 || math.Abs(sb.H-20) > 0.2 {
		t.Fatalf("static Bounds = (%+v, %v), want {45 40 10 20}", sb, ok)
	}

	opts := &svg.DrawOptions{
		Overrides: svg.Overrides{
			"r": svg.NodeOverride{}.Translate(20, 0),
		},
	}
	tb, ok := d.Bounds("r", opts)
	if !ok || math.Abs(tb.X-65) > 0.2 || math.Abs(tb.Y-40) > 0.2 || math.Abs(tb.W-10) > 0.2 || math.Abs(tb.H-20) > 0.2 {
		t.Fatalf("translated Bounds = (%+v, %v), want {65 40 10 20}", tb, ok)
	}
	img := drawOpts(d, image.Rect(0, 0, 100, 100), ggfx.GeoM{}, opts)
	if img.RGBAAt(70, 50).R != 255 {
		t.Errorf("translated pixel (70,50) = %v, want red", img.RGBAAt(70, 50))
	}
	if img.RGBAAt(50, 50).A != 0 {
		t.Errorf("old position (50,50) = %v, want transparent", img.RGBAAt(50, 50))
	}

	// Scale(2, 2) around PivotBox(0.5, 0.5) stays centered at (50, 50).
	scaleOpts := &svg.DrawOptions{
		Overrides: svg.Overrides{
			"r": svg.NodeOverride{}.Scale(2, 2).Pivot(svg.PivotBox(0.5, 0.5)),
		},
	}
	scb, ok := d.Bounds("r", scaleOpts)
	if !ok || math.Abs(scb.X-40) > 0.3 || math.Abs(scb.Y-30) > 0.3 || math.Abs(scb.W-20) > 0.3 || math.Abs(scb.H-40) > 0.3 {
		t.Fatalf("scaled Bounds = (%+v, %v), want {40 30 20 40}", scb, ok)
	}

	// Rotate(pi/2) with PivotAt(50, 50) rotates the 90-deg rect to 180-deg -> back to [40..60]x[45..55].
	rotOpts := &svg.DrawOptions{
		Overrides: svg.Overrides{
			"r": svg.NodeOverride{}.Rotate(math.Pi / 2).Pivot(svg.PivotAt(50, 50)),
		},
	}
	rb, ok := d.Bounds("r", rotOpts)
	if !ok || math.Abs(rb.X-40) > 0.3 || math.Abs(rb.Y-45) > 0.3 || math.Abs(rb.W-20) > 0.3 || math.Abs(rb.H-10) > 0.3 {
		t.Fatalf("rotated Bounds = (%+v, %v), want {40 45 20 10}", rb, ok)
	}

	// Matrix replaces TRS, and last setter wins.
	var m ggfx.GeoM
	m.Translate(-30, -30)
	matOpts := &svg.DrawOptions{
		Overrides: svg.Overrides{
			"r": svg.NodeOverride{}.Translate(999, 999).Matrix(m),
		},
	}
	mb, ok := d.Bounds("r", matOpts)
	if !ok || math.Abs(mb.X-15) > 0.3 || math.Abs(mb.Y-10) > 0.3 {
		t.Fatalf("Matrix Bounds = (%+v, %v), want X~15 Y~10", mb, ok)
	}

	// Skew(radX, radY) changes geometry without panic.
	skewOpts := &svg.DrawOptions{
		Overrides: svg.Overrides{
			"r": svg.NodeOverride{}.Skew(math.Pi/6, 0).Pivot(svg.PivotBox(0.5, 0.5)),
		},
	}
	skb, ok := d.Bounds("r", skewOpts)
	if !ok || skb.W <= sb.W+2 {
		t.Errorf("SkewX expected wider bounds than %.1f, got %.1f", sb.W, skb.W)
	}
}

func TestOverrideOpacityHiddenVisibleAndStyleInheritance(t *testing.T) {
	d := parse(t, svgDoc("0 0 60 20", `
<g id="grp" opacity="0.5" fill="#f00">
  <rect id="a" x="0" y="0" width="20" height="20"/>
  <rect id="b" x="20" y="0" width="20" height="20" fill="#00f"/>
  <rect id="c" x="40" y="0" width="20" height="20" fill="currentColor"/>
</g>`))

	// Opacity multiplies: 0.5 * 0.5 = 0.25 (~64 alpha).
	imgOp := drawOpts(d, image.Rect(0, 0, 60, 20), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{
			"grp": svg.NodeOverride{}.Opacity(0.5),
		},
	})
	if a := imgOp.RGBAAt(10, 10).A; a < 58 || a > 70 {
		t.Errorf("multiplicative opacity alpha = %d, want ~64", a)
	}

	// Hidden(true) skips subtree and Bounds returns false.
	hidOpts := &svg.DrawOptions{
		Overrides: svg.Overrides{
			"grp": svg.NodeOverride{}.Hidden(true),
		},
	}
	if _, ok := d.Bounds("a", hidOpts); ok {
		t.Error("Bounds(a) under Hidden(true) parent should return false")
	}
	imgHid := drawOpts(d, image.Rect(0, 0, 60, 20), ggfx.GeoM{}, hidOpts)
	if imgHid.RGBAAt(10, 10).A != 0 || imgHid.RGBAAt(30, 10).A != 0 {
		t.Error("Hidden(true) group still painted pixels")
	}

	// Visible(false) on group skips painting a, but child b with Visible(true) re-shows itself,
	// AND Bounds(a) is still valid!
	visOpts := &svg.DrawOptions{
		Overrides: svg.Overrides{
			"grp": svg.NodeOverride{}.Visible(false),
			"b":   svg.NodeOverride{}.Visible(true),
		},
	}
	if _, ok := d.Bounds("a", visOpts); !ok {
		t.Error("Bounds(a) under Visible(false) should remain true")
	}
	imgVis := drawOpts(d, image.Rect(0, 0, 60, 20), ggfx.GeoM{}, visOpts)
	if imgVis.RGBAAt(10, 10).A != 0 {
		t.Errorf("visibility:hidden rect a painted %v", imgVis.RGBAAt(10, 10))
	}
	if imgVis.RGBAAt(30, 10).B == 0 {
		t.Errorf("visibility:visible child b did not paint: %v", imgVis.RGBAAt(30, 10))
	}

	// Fill, Color, InheritPaint, NoPaint, FillOpacity, Stroke, StrokeOpacity, StrokeWidth.
	styleOpts := &svg.DrawOptions{
		CurrentColor: color.NRGBA{0, 255, 0, 255},
		Overrides: svg.Overrides{
			"grp": svg.NodeOverride{}.Fill(svg.ColorPaint(color.NRGBA{0, 255, 255, 255})).Color(color.NRGBA{255, 0, 255, 255}),
			"a":   svg.NodeOverride{}.FillOpacity(1).Stroke(svg.ColorPaint(color.NRGBA{255, 255, 0, 255})).StrokeWidth(4).StrokeOpacity(1),
			"b":   svg.NodeOverride{}.Fill(svg.InheritPaint()),
		},
	}
	imgSt := drawOpts(d, image.Rect(0, 0, 60, 20), ggfx.GeoM{}, styleOpts)
	// a inherited cyan fill from grp override (with grp opacity 0.5 -> premul ~0,127,127,127).
	if px := imgSt.RGBAAt(10, 10); px.R != 0 || px.G < 120 || px.B < 120 {
		t.Errorf("rect a inherited fill = %v, want semi-transparent cyan", px)
	}
	// b overrode fill to InheritPaint -> inherits cyan from grp!
	if px := imgSt.RGBAAt(30, 10); px.R != 0 || px.G < 120 || px.B < 120 {
		t.Errorf("rect b InheritPaint = %v, want semi-transparent cyan", px)
	}
	// c has fill="currentColor" and grp overrode Color to magenta (taking precedence over root green)!
	if px := imgSt.RGBAAt(50, 10); px.R < 120 || px.G != 0 || px.B < 120 {
		t.Errorf("rect c subtree Color override = %v, want semi-transparent magenta", px)
	}
}

func TestCurrentColorAndGradientStopCurrentColor(t *testing.T) {
	d := parse(t, svgDoc("0 0 60 20", `
<defs>
  <linearGradient id="g">
    <stop offset="0" stop-color="currentColor"/>
    <stop offset="1" stop-color="currentColor"/>
  </linearGradient>
</defs>
<rect x="0" y="0" width="20" height="20" fill="currentColor"/>
<line x1="25" y1="10" x2="35" y2="10" stroke="currentColor" stroke-width="6"/>
<rect x="40" y="0" width="20" height="20" fill="url(#g)"/>`))

	if !d.UsesCurrentColor() {
		t.Fatal("UsesCurrentColor() = false, want true")
	}

	// Default (nil CurrentColor) resolves to black.
	defImg := drawOpts(d, image.Rect(0, 0, 60, 20), ggfx.GeoM{}, &svg.DrawOptions{})
	for _, pt := range [][2]int{{10, 10}, {30, 10}, {50, 10}} {
		if c := defImg.RGBAAt(pt[0], pt[1]); c != (color.RGBA{0, 0, 0, 255}) {
			t.Errorf("default currentColor at %v = %v, want black", pt, c)
		}
	}

	// Custom CurrentColor tints fill, stroke, AND gradient stops (also tested via Renderer cache).
	r := d.NewRenderer()
	defer r.Release()
	for _, want := range []color.NRGBA{{255, 0, 0, 255}, {0, 128, 255, 255}} {
		dst := image.NewRGBA(image.Rect(0, 0, 60, 20))
		r.Draw(dst, ggfx.GeoM{}, &svg.DrawOptions{CurrentColor: want})
		wantRGBA := color.RGBA(want)
		for _, pt := range [][2]int{{10, 10}, {30, 10}, {50, 10}} {
			if c := dst.RGBAAt(pt[0], pt[1]); c != wantRGBA {
				t.Errorf("currentColor %v at %v = %v, want %v", want, pt, c, wantRGBA)
			}
		}
	}
}

func TestTrimBasicMultiSubpathClosedWrapDotsAndDashed(t *testing.T) {
	// 1. Basic line trim: [0, 0.5], [0.25, 0.75], empty, full pass-through, NaN.
	dLine := parse(t, svgDoc("0 0 100 20", `
<path id="p" d="M0 10 H100" fill="none" stroke="#fff" stroke-width="6"/>`))

	// [0, 0.5] -> left half lit, right half clear.
	half := drawOpts(dLine, image.Rect(0, 0, 100, 20), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"p": svg.NodeOverride{}.Trim(0, 0.5, 0)},
	})
	if half.RGBAAt(25, 10).A != 255 || half.RGBAAt(75, 10).A != 0 {
		t.Errorf("trim [0,0.5]: (25,10)=%v (75,10)=%v", half.RGBAAt(25, 10), half.RGBAAt(75, 10))
	}

	// [0.25, 0.75] -> middle lit, ends clear.
	mid := drawOpts(dLine, image.Rect(0, 0, 100, 20), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"p": svg.NodeOverride{}.Trim(0.25, 0.75, 0)},
	})
	if mid.RGBAAt(10, 10).A != 0 || mid.RGBAAt(50, 10).A != 255 || mid.RGBAAt(90, 10).A != 0 {
		t.Errorf("trim [0.25,0.75]: (10)=%v (50)=%v (90)=%v", mid.RGBAAt(10, 10), mid.RGBAAt(50, 10), mid.RGBAAt(90, 10))
	}

	// Empty trim (start >= end) -> nothing drawn.
	empty := drawOpts(dLine, image.Rect(0, 0, 100, 20), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"p": svg.NodeOverride{}.Trim(0.5, 0.5, 0)},
	})
	if empty.RGBAAt(50, 10).A != 0 {
		t.Errorf("empty trim drew pixel: %v", empty.RGBAAt(50, 10))
	}

	// Full trim [0, 1] and NaN -> pixel-identical to untrimmed.
	untrimmed := drawInto(dLine, image.Rect(0, 0, 100, 20), ggfx.GeoM{})
	full := drawOpts(dLine, image.Rect(0, 0, 100, 20), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"p": svg.NodeOverride{}.Trim(0, 1, 0)},
	})
	nanTrim := drawOpts(dLine, image.Rect(0, 0, 100, 20), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"p": svg.NodeOverride{}.Trim(math.NaN(), 0.5, 0)},
	})
	if !bytes.Equal(untrimmed.Pix, full.Pix) {
		t.Error("full trim [0,1] differs from untrimmed pass-through")
	}
	if !bytes.Equal(untrimmed.Pix, nanTrim.Pix) {
		t.Error("NaN trim differs from untrimmed pass-through")
	}

	// 2. Multi-subpath sequential trim: two lines of length 50 each (total L=100).
	dMulti := parse(t, svgDoc("0 0 60 30", `
<path id="m" d="M5 10 H55 M5 20 H55" fill="none" stroke="#fff" stroke-width="4"/>`))
	m1 := drawOpts(dMulti, image.Rect(0, 0, 60, 30), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"m": svg.NodeOverride{}.Trim(0, 0.5, 0)},
	})
	if m1.RGBAAt(30, 10).A != 255 || m1.RGBAAt(30, 20).A != 0 {
		t.Errorf("multi-subpath [0,0.5]: line1=%v line2=%v", m1.RGBAAt(30, 10), m1.RGBAAt(30, 20))
	}
	m2 := drawOpts(dMulti, image.Rect(0, 0, 60, 30), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"m": svg.NodeOverride{}.Trim(0.25, 0.75, 0)},
	})
	if m2.RGBAAt(10, 10).A != 0 || m2.RGBAAt(45, 10).A != 255 || m2.RGBAAt(15, 20).A != 255 || m2.RGBAAt(45, 20).A != 0 {
		t.Errorf("multi-subpath [0.25,0.75] failed: (10,10)=%v (45,10)=%v (15,20)=%v (45,20)=%v",
			m2.RGBAAt(10, 10), m2.RGBAAt(45, 10), m2.RGBAAt(15, 20), m2.RGBAAt(45, 20))
	}

	// 3. Closed square wrap across seam (vertex 0 at (20,20)):
	// Perimeter = 4*40 = 160. Trim [0, 0.5] with offset 0.75 -> wraps [0.75, 1] U [0, 0.25],
	// i.e. left edge going up to (20,20) and top edge going right from (20,20).
	// Because it is emitted as ONE open piece through (20,20) with miter join,
	// the outer corner pixel at (18, 18) must be filled (butt caps would leave (18,18) empty!).
	dSquare := parse(t, svgDoc("0 0 80 80", `
<path id="sq" d="M20 20 H60 V60 H20 Z" fill="none" stroke="#fff" stroke-width="8" stroke-linejoin="miter"/>`))
	wrap := drawOpts(dSquare, image.Rect(0, 0, 80, 80), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"sq": svg.NodeOverride{}.Trim(0, 0.5, 0.75)},
	})
	if wrap.RGBAAt(18, 18).A != 255 {
		t.Errorf("closed wrap outer miter corner at (18,18) = %v, want opaque (continuous join across seam)", wrap.RGBAAt(18, 18))
	}
	if wrap.RGBAAt(60, 60).A != 0 {
		t.Errorf("opposite corner (60,60) should be untrimmed/clear, got %v", wrap.RGBAAt(60, 60))
	}

	// 4. Trim + stroke-dasharray: dashes stay anchored to original path (no crawling!).
	dDash := parse(t, svgDoc("0 0 100 20", `
<path id="dl" d="M0 10 H100" fill="none" stroke="#fff" stroke-width="6" stroke-dasharray="10 10"/>`))
	dashTrim := drawOpts(dDash, image.Rect(0, 0, 100, 20), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"dl": svg.NodeOverride{}.Trim(0.15, 0.55, 0)},
	})
	// Original dashes: [0..10] on, [10..20] off, [20..30] on, [30..40] off, [40..50] on, [50..60] off.
	// Trimmed to [15..55]: [15..20] must be OFF (gap), [20..30] ON, [30..40] OFF, [40..50] ON, [50..55] OFF!
	for _, tc := range []struct {
		x  int
		on bool
	}{
		{17, false}, {25, true}, {35, false}, {45, true}, {52, false},
	} {
		got := dashTrim.RGBAAt(tc.x, 10).A > 128
		if got != tc.on {
			t.Errorf("dashed trim at x=%d: lit=%v, want %v (alpha=%d)", tc.x, got, tc.on, dashTrim.RGBAAt(tc.x, 10).A)
		}
	}

	// 5. High-scale smooth trim (no 1/64 user-unit quantization):
	// 24-unit line scaled by 20 -> 480px. Step by 1/4800 = 0.1px increments;
	// verify adjacent small trim increments advance smoothly without 0.31px jumps.
	dIcon := parse(t, svgDoc("0 0 24 4", `<path id="i" d="M0 2 H24" fill="none" stroke="#fff" stroke-width="2"/>`))
	var g20 ggfx.GeoM
	g20.Scale(20, 20)
	// Compare 0.500 vs 0.500 + 0.5/480 (half a pixel in output space, which is 0.025 user units > 1/64? No, 0.005 user units < 1/64!).
	// Wait: 1/64 user unit * 20 scale = 0.3125px. Moving by 0.008 user units (~0.16px) is < 1/64 user unit:
	// without float emission both would quantize to the same 1/64 user unit and draw identical pixels!
	aImg := drawOpts(dIcon, image.Rect(0, 0, 480, 80), g20, &svg.DrawOptions{
		Overrides: svg.Overrides{"i": svg.NodeOverride{}.Trim(0, 0.5, 0)},
	})
	bImg := drawOpts(dIcon, image.Rect(0, 0, 480, 80), g20, &svg.DrawOptions{
		Overrides: svg.Overrides{"i": svg.NodeOverride{}.Trim(0, 0.5+0.008/24.0, 0)},
	})
	if bytes.Equal(aImg.Pix, bImg.Pix) {
		t.Error("sub-1/64-user-unit trim advance produced identical pixels at 20x scale (quantization regression)")
	}

	// 6. Trim on <g> applies separately to each descendant shape; fill is not trimmed.
	dGrpTrim := parse(t, svgDoc("0 0 100 40", `
<g id="tg">
  <path d="M0 10 H100" fill="none" stroke="#fff" stroke-width="4"/>
  <path d="M0 30 H50" fill="none" stroke="#fff" stroke-width="4"/>
  <rect x="60" y="22" width="20" height="16" fill="#0f0"/>
</g>`))
	gtImg := drawOpts(dGrpTrim, image.Rect(0, 0, 100, 40), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"tg": svg.NodeOverride{}.Trim(0, 0.5, 0)},
	})
	// First path (len 100) trimmed to [0..50], second path (len 50) trimmed to [0..25], rect fill untouched!
	if gtImg.RGBAAt(40, 10).A != 255 || gtImg.RGBAAt(60, 10).A != 0 {
		t.Errorf("group trim path 1 failed: (40,10)=%v (60,10)=%v", gtImg.RGBAAt(40, 10), gtImg.RGBAAt(60, 10))
	}
	if gtImg.RGBAAt(15, 30).A != 255 || gtImg.RGBAAt(35, 30).A != 0 {
		t.Errorf("group trim path 2 failed: (15,30)=%v (35,30)=%v", gtImg.RGBAAt(15, 30), gtImg.RGBAAt(35, 30))
	}
	if gtImg.RGBAAt(70, 30).G != 255 {
		t.Errorf("group trim should not trim fill: (70,30)=%v", gtImg.RGBAAt(70, 30))
	}
}

func TestPathLengthDashScaling(t *testing.T) {
	// Line length is 100 user units, pathLength="10".
	// stroke-dasharray="5 5" should scale by 100/10 = 10 -> 50px dash, 50px gap!
	d := parse(t, svgDoc("0 0 100 20", `
<path id="pl" d="M0 10 H100" pathLength="10" fill="none" stroke="#fff" stroke-width="6" stroke-dasharray="5 5"/>`))

	img := drawInto(d, image.Rect(0, 0, 100, 20), ggfx.GeoM{})
	if img.RGBAAt(25, 10).A != 255 || img.RGBAAt(75, 10).A != 0 {
		t.Errorf("pathLength static dash: (25,10)=%v (75,10)=%v, want lit then clear", img.RGBAAt(25, 10), img.RGBAAt(75, 10))
	}
	// Override DashOffset(5) -> scales to 50 user units -> first half gap, second half lit!
	imgOff := drawOpts(d, image.Rect(0, 0, 100, 20), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"pl": svg.NodeOverride{}.DashOffset(5)},
	})
	if imgOff.RGBAAt(25, 10).A != 0 || imgOff.RGBAAt(75, 10).A != 255 {
		t.Errorf("pathLength dashoffset override: (25,10)=%v (75,10)=%v, want clear then lit", imgOff.RGBAAt(25, 10), imgOff.RGBAAt(75, 10))
	}
}

func TestUseInstancesAndDrawTimeClipsMasksMarkers(t *testing.T) {
	// 1. <use> instances: override target affects all instances; override <use> affects only that instance.
	dUse := parse(t, svgDoc("0 0 60 20", `
<defs><rect id="box" width="10" height="10" fill="#f00"/></defs>
<use id="u1" href="#box" x="5" y="5"/>
<use id="u2" href="#box" x="35" y="5"/>`))
	imgUse := drawOpts(dUse, image.Rect(0, 0, 60, 20), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{
			"box": svg.NodeOverride{}.Fill(svg.ColorPaint(color.NRGBA{0, 255, 0, 255})),
			"u2":  svg.NodeOverride{}.Translate(10, 0),
		},
	})
	if imgUse.RGBAAt(10, 10).G != 255 {
		t.Errorf("u1 did not get #box green override: %v", imgUse.RGBAAt(10, 10))
	}
	if imgUse.RGBAAt(40, 10).A != 0 || imgUse.RGBAAt(50, 10).G != 255 {
		t.Errorf("u2 translate+green failed: old=%v new=%v", imgUse.RGBAAt(40, 10), imgUse.RGBAAt(50, 10))
	}

	// 2. Draw-time clipPath:
	// Moving a clipped group moves its clip; moving a child keeps the clip fixed;
	// overriding the shape INSIDE <clipPath> moves the clip!
	dClip := parse(t, svgDoc("0 0 100 100", `
<defs><clipPath id="c"><rect id="cr" x="0" y="0" width="20" height="20"/></clipPath></defs>
<g id="cg" clip-path="url(#c)"><rect id="child" x="0" y="0" width="100" height="100" fill="#f00"/></g>`))

	// Move clipped group by (+30, 0) -> clipped red square is now at [30..50]x[0..20].
	imgCG := drawOpts(dClip, image.Rect(0, 0, 100, 100), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"cg": svg.NodeOverride{}.Translate(30, 0)},
	})
	if imgCG.RGBAAt(10, 10).A != 0 || imgCG.RGBAAt(40, 10).R != 255 {
		t.Errorf("moved clipped group failed: (10,10)=%v (40,10)=%v", imgCG.RGBAAt(10, 10), imgCG.RGBAAt(40, 10))
	}

	// Override clipPath child #cr by (+50, 0) -> clip window moves to [50..70]x[0..20].
	imgCR := drawOpts(dClip, image.Rect(0, 0, 100, 100), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"cr": svg.NodeOverride{}.Translate(50, 0)},
	})
	if imgCR.RGBAAt(10, 10).A != 0 || imgCR.RGBAAt(60, 10).R != 255 {
		t.Errorf("overridden clipPath content failed: (10,10)=%v (60,10)=%v", imgCR.RGBAAt(10, 10), imgCR.RGBAAt(60, 10))
	}

	// 3. Draw-time mask overridden by ID.
	dMask := parse(t, svgDoc("0 0 40 20", `
<defs><mask id="m"><rect id="mr" x="0" y="0" width="20" height="20" fill="#fff"/></mask></defs>
<rect width="40" height="20" fill="#00f" mask="url(#m)"/>`))
	imgMask := drawOpts(dMask, image.Rect(0, 0, 40, 20), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"mr": svg.NodeOverride{}.Translate(20, 0)},
	})
	if imgMask.RGBAAt(10, 10).A != 0 || imgMask.RGBAAt(30, 10).B != 255 {
		t.Errorf("overridden mask content failed: (10,10)=%v (30,10)=%v", imgMask.RGBAAt(10, 10), imgMask.RGBAAt(30, 10))
	}

	// 4. Draw-time markers: stroke-width override scales marker; marker shape override changes color; hidden shape hides markers.
	dMark := parse(t, svgDoc("0 0 100 40", `
<defs>
  <marker id="dot" viewBox="0 0 10 10" refX="5" refY="5" markerWidth="4" markerHeight="4">
    <rect id="mdot" width="10" height="10" fill="#f00"/>
  </marker>
</defs>
<path id="edge" d="M20 20 H80" stroke="#000" stroke-width="2" marker-end="url(#dot)"/>`))

	// StrokeWidth(6) -> marker size becomes 4*6 = 24px (extends +-12px from (80,20)), and #mdot turns green.
	imgMk := drawOpts(dMark, image.Rect(0, 0, 100, 40), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{
			"edge": svg.NodeOverride{}.StrokeWidth(6),
			"mdot": svg.NodeOverride{}.Fill(svg.ColorPaint(color.NRGBA{0, 255, 0, 255})),
		},
	})
	if imgMk.RGBAAt(80, 12).G != 255 {
		t.Errorf("draw-time marker strokeWidth+color override at (80,12) = %v, want green", imgMk.RGBAAt(80, 12))
	}
	// Hidden edge hides markers too.
	imgMkHid := drawOpts(dMark, image.Rect(0, 0, 100, 40), ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{"edge": svg.NodeOverride{}.Hidden(true)},
	})
	if imgMkHid.RGBAAt(80, 20).A != 0 {
		t.Errorf("hidden edge still drew marker: %v", imgMkHid.RGBAAt(80, 20))
	}
}

func TestStaticDisplayVisibilityAndTransformOrigin(t *testing.T) {
	d := parse(t, svgDoc("0 0 100 40", `
<rect x="0" y="0" width="20" height="20" fill="#f00" display="none"/>
<g visibility="hidden">
  <rect x="20" y="0" width="20" height="20" fill="#0f0"/>
  <rect x="40" y="0" width="20" height="20" fill="#00f" visibility="visible"/>
</g>
<rect x="70" y="10" width="20" height="10" fill="#ff0" transform="rotate(90)" transform-origin="center" transform-box="fill-box"/>`))

	img := drawInto(d, image.Rect(0, 0, 100, 40), ggfx.GeoM{})
	if img.RGBAAt(10, 10).A != 0 {
		t.Errorf("display:none rect drew %v", img.RGBAAt(10, 10))
	}
	if img.RGBAAt(30, 10).A != 0 {
		t.Errorf("visibility:hidden rect drew %v", img.RGBAAt(30, 10))
	}
	if img.RGBAAt(50, 10).B != 255 {
		t.Errorf("visibility:visible child did not draw blue: %v", img.RGBAAt(50, 10))
	}
	// Rect [70..90]x[10..20] centered at (80, 15) rotated 90 deg -> [75..85]x[5..25].
	if img.RGBAAt(80, 8).R != 255 || img.RGBAAt(80, 8).G != 255 {
		t.Errorf("transform-origin:center fill-box rotated rect top (80,8) = %v, want yellow", img.RGBAAt(80, 8))
	}
	if img.RGBAAt(72, 15).A != 0 {
		t.Errorf("transform-origin:center rotated rect outside (72,15) = %v, want clear", img.RGBAAt(72, 15))
	}
}

func TestBinaryV1UnchangedAndV2CurrentColor(t *testing.T) {
	// 1. Plain icon without currentColor must still emit version 1.
	plain := parse(t, binaryDocs[0])
	v1Data, err := plain.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary v1: %v", err)
	}
	if v1Data[0] != 1 {
		t.Errorf("plain icon binary version = %d, want 1", v1Data[0])
	}

	// 2. Icon with currentColor fill & stroke emits version 2 and round-trips.
	ccDoc := parse(t, svgDoc("0 0 24 24", `
<rect x="2" y="2" width="10" height="10" fill="currentColor" fill-opacity="0.8"/>
<path d="M14 4 V20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
<path d="M18 4 V20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>`))
	if !ccDoc.UsesCurrentColor() {
		t.Fatal("ccDoc.UsesCurrentColor() = false")
	}
	v2Data, err := ccDoc.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary v2: %v", err)
	}
	if v2Data[0] != 2 {
		t.Fatalf("currentColor icon binary version = %d, want 2", v2Data[0])
	}

	var back svg.Document
	if err := back.UnmarshalBinary(v2Data); err != nil {
		t.Fatalf("UnmarshalBinary v2: %v", err)
	}
	if !back.UsesCurrentColor() {
		t.Error("unmarshaled v2 doc UsesCurrentColor() = false")
	}
	if len(back.NodeIDs()) != 0 || back.Animated() {
		t.Error("unmarshaled binary doc should have no scene/nodes and Animated()=false")
	}

	// Render original and unmarshaled with two different CurrentColors: must match byte-for-byte.
	for _, col := range []color.Color{nil, color.NRGBA{255, 64, 32, 255}, color.NRGBA{32, 192, 255, 255}} {
		var g ggfx.GeoM
		g.Scale(2, 2)
		opts := &svg.DrawOptions{CurrentColor: col}
		want := drawOpts(ccDoc, image.Rect(0, 0, 48, 48), g, opts)
		got := drawOpts(&back, image.Rect(0, 0, 48, 48), g, opts)
		if !bytes.Equal(want.Pix, got.Pix) {
			t.Errorf("v2 binary rendered differently for CurrentColor=%v", col)
		}
	}

	// Re-marshaling v2 produces identical bytes.
	again, err := back.MarshalBinary()
	if err != nil || !bytes.Equal(again, v2Data) {
		t.Errorf("v2 re-marshal mismatch (err=%v)", err)
	}

	// Document with display:none has no binary form.
	hidDoc := parse(t, svgDoc("0 0 10 10", `<rect width="10" height="10" display="none"/>`))
	if _, err := hidDoc.MarshalBinary(); !errors.Is(err, svg.ErrNoBinaryForm) {
		t.Errorf("display:none doc MarshalBinary err = %v, want ErrNoBinaryForm", err)
	}
}

func TestRendererLifecycleAndEqualityHelpers(t *testing.T) {
	d := parse(t, iconSVG)
	r := d.NewRenderer()
	var g ggfx.GeoM
	g.Scale(1, 1)
	opts := &svg.DrawOptions{
		Time:         500 * time.Millisecond,
		CurrentColor: color.NRGBA{255, 0, 0, 255},
	}
	want := drawOpts(d, image.Rect(0, 0, 100, 100), g, opts)
	got := image.NewRGBA(image.Rect(0, 0, 100, 100))
	r.Draw(got, g, opts)
	if !bytes.Equal(want.Pix, got.Pix) {
		t.Error("Renderer.Draw differs from Document.DrawWith")
	}
	r.Release()
	// Drawing again after Release must still work cleanly.
	got2 := image.NewRGBA(image.Rect(0, 0, 100, 100))
	r.Draw(got2, g, opts)
	if !bytes.Equal(want.Pix, got2.Pix) {
		t.Error("Renderer.Draw after Release differs")
	}
	r.Release()

	// Defensive copy on Dash and Overrides.Equal.
	dash := []float64{4, 2}
	o1 := svg.NodeOverride{}.Dash(dash...)
	dash[0] = 99
	o2 := svg.NodeOverride{}.Dash(4, 2)
	if !o1.Equal(o2) {
		t.Error("Dash did not make a defensive copy of caller slice")
	}
	ovA := svg.Overrides{"a": o1, "empty": svg.NodeOverride{}}
	ovB := svg.Overrides{"a": o2}
	if !ovA.Equal(ovB) {
		t.Error("Overrides.Equal should ignore zero NodeOverride entries")
	}
}

func TestConcurrentDrawWithAndQueriesRaceSafe(t *testing.T) {
	d := parse(t, svgDoc("0 0 100 100", `
<defs>
  <clipPath id="cp"><circle id="cc" cx="50" cy="50" r="40"/></clipPath>
  <linearGradient id="lg"><stop offset="0" stop-color="currentColor"/><stop offset="1" stop-color="#fff"/></linearGradient>
</defs>
<g id="root" clip-path="url(#cp)">
  <path id="p" d="M10 50 Q50 10 90 50 T10 50" fill="url(#lg)" stroke="currentColor" stroke-width="4"/>
</g>`))

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			frac := float64(i+1) / 16.0
			opts := &svg.DrawOptions{
				Time:         time.Duration(i*50) * time.Millisecond,
				CurrentColor: color.NRGBA{uint8(i * 15), 100, 200, 255},
				Overrides: svg.Overrides{
					"p":  svg.NodeOverride{}.Trim(0, frac, 0).Rotate(frac).Pivot(svg.PivotBox(0.5, 0.5)),
					"cc": svg.NodeOverride{}.Translate(float64(i)-8, 0),
				},
			}
			_ = drawOpts(d, image.Rect(0, 0, 64, 64), scale(0.64), opts)
			_, _ = d.Bounds("p", opts)
			if n, ok := d.Node("p"); ok {
				_, _ = n.LocalBounds()
			}
			_ = d.NodeIDs()
			_ = d.Timeline()
			_, _ = d.NextChange(opts.Time)
		})
	}
	wg.Wait()
}

func drawRenderer(r *svg.Renderer, bounds image.Rectangle, g ggfx.GeoM, opts *svg.DrawOptions) *image.RGBA {
	dst := image.NewRGBA(bounds)
	r.Draw(dst, g, opts)
	return dst
}

func TestSMILAnimationsEndToEndPixels(t *testing.T) {
	// 1. SMIL <animate> on r, fill, opacity.
	dAnim := parse(t, svgDoc("0 0 100 100", `
<circle id="c" cx="50" cy="50" r="10" fill="#ff0000" opacity="1">
  <animate attributeName="r" from="10" to="30" dur="1s" fill="freeze"/>
  <animate attributeName="fill" from="#ff0000" to="#0000ff" dur="1s" fill="freeze"/>
  <animate attributeName="opacity" from="1" to="0.5" dur="1s" fill="freeze"/>
</circle>`))
	if !dAnim.Animated() {
		t.Fatal("dAnim.Animated() = false, want true")
	}

	bounds := image.Rect(0, 0, 100, 100)
	ren := dAnim.NewRenderer()
	defer ren.Release()

	// t = 0: r=10, solid red, alpha=255.
	o0 := &svg.DrawOptions{Time: 0}
	img0 := drawOpts(dAnim, bounds, ggfx.GeoM{}, o0)
	ren0 := drawRenderer(ren, bounds, ggfx.GeoM{}, o0)
	if !bytes.Equal(img0.Pix, ren0.Pix) {
		t.Error("t=0: Renderer.Draw differs from DrawWith")
	}
	if px := img0.RGBAAt(50, 50); px != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("t=0 center = %v, want opaque red", px)
	}
	if px := img0.RGBAAt(75, 50); px.A != 0 {
		t.Errorf("t=0 outside r=10 at (75,50) = %v, want clear", px)
	}

	// t = 500ms: r=20, blended purple (~128,0,128), opacity=0.75 (~191 alpha).
	oMid := &svg.DrawOptions{Time: 500 * time.Millisecond}
	imgMid := drawOpts(dAnim, bounds, ggfx.GeoM{}, oMid)
	renMid := drawRenderer(ren, bounds, ggfx.GeoM{}, oMid)
	if !bytes.Equal(imgMid.Pix, renMid.Pix) {
		t.Error("t=500ms: Renderer.Draw differs from DrawWith")
	}
	if px := imgMid.RGBAAt(65, 50); px.A < 180 || px.A > 205 || px.R < 80 || px.B < 80 || px.G != 0 {
		t.Errorf("t=500ms at (65,50) = %v, want semi-transparent purple", px)
	}
	if px := imgMid.RGBAAt(75, 50); px.A != 0 {
		t.Errorf("t=500ms outside r=20 at (75,50) = %v, want clear", px)
	}

	// t = 1s (frozen): r=30, blue, opacity=0.5 (~128 alpha).
	oEnd := &svg.DrawOptions{Time: time.Second}
	imgEnd := drawOpts(dAnim, bounds, ggfx.GeoM{}, oEnd)
	renEnd := drawRenderer(ren, bounds, ggfx.GeoM{}, oEnd)
	if !bytes.Equal(imgEnd.Pix, renEnd.Pix) {
		t.Error("t=1s: Renderer.Draw differs from DrawWith")
	}
	if px := imgEnd.RGBAAt(75, 50); px.A < 120 || px.A > 136 || px.B < 120 || px.R != 0 {
		t.Errorf("t=1s at (75,50) = %v, want ~50%% alpha blue", px)
	}

	// 2. SMIL <animateTransform> (translate + additive="sum" rotate).
	dXform := parse(t, svgDoc("0 0 100 60", `
<rect id="tr" x="0" y="0" width="20" height="10" fill="#00ff00">
  <animateTransform attributeName="transform" type="translate" from="10,20" to="50,20" dur="1s" fill="freeze"/>
  <animateTransform attributeName="transform" type="rotate" from="0 10 5" to="90 10 5" dur="1s" additive="sum" fill="freeze"/>
</rect>`))
	xfRen := dXform.NewRenderer()
	defer xfRen.Release()
	xfBounds := image.Rect(0, 0, 100, 60)

	// t=0: rect at [10..30] x [20..30].
	xf0 := drawOpts(dXform, xfBounds, ggfx.GeoM{}, &svg.DrawOptions{Time: 0})
	if px := xf0.RGBAAt(20, 25); px.G != 255 {
		t.Errorf("animateTransform t=0 (20,25) = %v, want green", px)
	}
	if px := xf0.RGBAAt(60, 25); px.A != 0 {
		t.Errorf("animateTransform t=0 (60,25) = %v, want clear", px)
	}

	// t=1s: translated to (50,20) and rotated 90 deg around local (10,5) -> [55..65] x [15..35].
	xf1Opts := &svg.DrawOptions{Time: time.Second}
	xf1 := drawOpts(dXform, xfBounds, ggfx.GeoM{}, xf1Opts)
	xf1Ren := drawRenderer(xfRen, xfBounds, ggfx.GeoM{}, xf1Opts)
	if !bytes.Equal(xf1.Pix, xf1Ren.Pix) {
		t.Error("animateTransform t=1s: Renderer.Draw differs from DrawWith")
	}
	if px := xf1.RGBAAt(60, 17); px.G != 255 {
		t.Errorf("animateTransform t=1s rotated top (60,17) = %v, want green", px)
	}
	if px := xf1.RGBAAt(60, 33); px.G != 255 {
		t.Errorf("animateTransform t=1s rotated bottom (60,33) = %v, want green", px)
	}
	if px := xf1.RGBAAt(52, 25); px.A != 0 {
		t.Errorf("animateTransform t=1s outside rotated rect (52,25) = %v, want clear", px)
	}

	// 3. SMIL <set> visibility.
	dSet := parse(t, svgDoc("0 0 40 40", `
<rect id="s" x="10" y="10" width="20" height="20" fill="#ff0000" visibility="visible">
  <set attributeName="visibility" to="hidden" begin="0.5s" dur="0.5s"/>
</rect>`))
	sBounds := image.Rect(0, 0, 40, 40)
	if px := drawOpts(dSet, sBounds, ggfx.GeoM{}, &svg.DrawOptions{Time: 0}).RGBAAt(20, 20); px.R != 255 {
		t.Errorf("<set> t=0 = %v, want red", px)
	}
	if px := drawOpts(dSet, sBounds, ggfx.GeoM{}, &svg.DrawOptions{Time: 600 * time.Millisecond}).RGBAAt(20, 20); px.A != 0 {
		t.Errorf("<set> t=600ms = %v, want hidden/clear", px)
	}
	if px := drawOpts(dSet, sBounds, ggfx.GeoM{}, &svg.DrawOptions{Time: 1200 * time.Millisecond}).RGBAAt(20, 20); px.R != 255 {
		t.Errorf("<set> t=1200ms = %v, want visible red again", px)
	}
}

func TestCSSKeyframesTransformOriginAlternateStepsBezier(t *testing.T) {
	// 1. rotate(0deg) -> rotate(360deg) with transform-box: fill-box; transform-origin: 50% 50%.
	dSpin := parse(t, svgDoc("0 0 100 100", `
<style>
  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
  #bar {
    transform-box: fill-box;
    transform-origin: 50% 50%;
    animation: spin 1s linear infinite;
  }
</style>
<rect id="bar" x="30" y="45" width="40" height="10" fill="#ff0000"/>`))
	b100 := image.Rect(0, 0, 100, 100)
	ren := dSpin.NewRenderer()
	defer ren.Release()

	// t=0: horizontal [30..70]x[45..55].
	img0 := drawRenderer(ren, b100, ggfx.GeoM{}, &svg.DrawOptions{Time: 0})
	if img0.RGBAAt(65, 50).R != 255 || img0.RGBAAt(50, 35).A != 0 {
		t.Errorf("spin t=0: (65,50)=%v (50,35)=%v", img0.RGBAAt(65, 50), img0.RGBAAt(50, 35))
	}
	// t=250ms (90 deg): vertical [45..55]x[30..70].
	img250 := drawOpts(dSpin, b100, ggfx.GeoM{}, &svg.DrawOptions{Time: 250 * time.Millisecond})
	ren250 := drawRenderer(ren, b100, ggfx.GeoM{}, &svg.DrawOptions{Time: 250 * time.Millisecond})
	if !bytes.Equal(img250.Pix, ren250.Pix) {
		t.Error("spin t=250ms: Renderer.Draw differs from DrawWith")
	}
	if img250.RGBAAt(50, 35).R != 255 || img250.RGBAAt(65, 50).A != 0 {
		t.Errorf("spin t=250ms (90deg): (50,35)=%v (65,50)=%v", img250.RGBAAt(50, 35), img250.RGBAAt(65, 50))
	}

	// 2. alternate, steps(), and cubic-bezier().
	dTiming := parse(t, svgDoc("0 0 100 60", `
<style>
  @keyframes slide {
    from { transform: translateX(0px); }
    to { transform: translateX(60px); }
  }
  #alt { animation: slide 1s linear 2 alternate forwards; }
  #stp { animation: slide 1s steps(2, end) forwards; }
  #lin { animation: slide 1s linear forwards; }
  #bez { animation: slide 1s cubic-bezier(0, 0, 0.2, 1) forwards; }
</style>
<rect id="alt" x="0" y="0" width="20" height="10" fill="#ff0000"/>
<rect id="stp" x="0" y="15" width="20" height="10" fill="#00ff00"/>
<rect id="lin" x="0" y="30" width="20" height="10" fill="#0000ff"/>
<rect id="bez" x="0" y="45" width="20" height="10" fill="#ffff00"/>`))
	bTiming := image.Rect(0, 0, 100, 60)

	// alternate: t=500ms -> x=30, t=1000ms -> x=60, t=1500ms -> reversing back to x=30.
	alt1500 := drawOpts(dTiming, bTiming, ggfx.GeoM{}, &svg.DrawOptions{Time: 1500 * time.Millisecond})
	if alt1500.RGBAAt(40, 5).R != 255 || alt1500.RGBAAt(70, 5).A != 0 {
		t.Errorf("alternate t=1500ms: (40,5)=%v (70,5)=%v", alt1500.RGBAAt(40, 5), alt1500.RGBAAt(70, 5))
	}

	// steps(2, end): t=250ms -> x=0 ([0..20]), t=750ms -> x=30 ([30..50]), t=1000ms -> x=60 ([60..80]).
	stp250 := drawOpts(dTiming, bTiming, ggfx.GeoM{}, &svg.DrawOptions{Time: 250 * time.Millisecond})
	if stp250.RGBAAt(10, 20).G != 255 || stp250.RGBAAt(40, 20).A != 0 {
		t.Errorf("steps(2,end) t=250ms: (10,20)=%v (40,20)=%v", stp250.RGBAAt(10, 20), stp250.RGBAAt(40, 20))
	}
	stp750 := drawOpts(dTiming, bTiming, ggfx.GeoM{}, &svg.DrawOptions{Time: 750 * time.Millisecond})
	if stp750.RGBAAt(40, 20).G != 255 || stp750.RGBAAt(10, 20).A != 0 {
		t.Errorf("steps(2,end) t=750ms: (40,20)=%v (10,20)=%v", stp750.RGBAAt(40, 20), stp750.RGBAAt(10, 20))
	}
	stp1000 := drawOpts(dTiming, bTiming, ggfx.GeoM{}, &svg.DrawOptions{Time: time.Second})
	if stp1000.RGBAAt(70, 20).G != 255 {
		t.Errorf("steps(2,end) t=1000ms: (70,20)=%v, want green", stp1000.RGBAAt(70, 20))
	}

	// cubic-bezier(0, 0, 0.2, 1) (fast ease-out) must be significantly ahead of linear at t=500ms.
	linBox, ok1 := dTiming.Bounds("lin", &svg.DrawOptions{Time: 500 * time.Millisecond})
	bezBox, ok2 := dTiming.Bounds("bez", &svg.DrawOptions{Time: 500 * time.Millisecond})
	if !ok1 || !ok2 || bezBox.X <= linBox.X+15 {
		t.Errorf("cubic-bezier ease-out t=500ms: bez.X=%.2f (ok=%v), lin.X=%.2f (ok=%v); want bez ahead by >15", bezBox.X, ok2, linBox.X, ok1)
	}
}

func TestAnimatedFillOnGradientPreservesBaseGradient(t *testing.T) {
	d := parse(t, svgDoc("0 0 100 20", `
<defs>
  <linearGradient id="g">
    <stop offset="0" stop-color="#ff0000"/>
    <stop offset="1" stop-color="#0000ff"/>
  </linearGradient>
</defs>
<rect id="r" x="0" y="0" width="100" height="20" fill="url(#g)">
  <animate attributeName="fill" to="#00ff00" dur="1s" fill="freeze"/>
</rect>`))
	bounds := image.Rect(0, 0, 100, 20)

	// First half (t=200ms < 500ms): discrete paint interpolation returns base gradient, NOT transparent/empty!
	imgEarly := drawOpts(d, bounds, ggfx.GeoM{}, &svg.DrawOptions{Time: 200 * time.Millisecond})
	left := imgEarly.RGBAAt(5, 10)
	right := imgEarly.RGBAAt(95, 10)
	if left.A != 255 || left.R < 200 || left.B > 50 {
		t.Errorf("t=200ms left gradient pixel = %v, want reddish", left)
	}
	if right.A != 255 || right.B < 200 || right.R > 50 {
		t.Errorf("t=200ms right gradient pixel = %v, want blueish", right)
	}

	// Second half (t=700ms >= 500ms): switches to solid green.
	imgLate := drawOpts(d, bounds, ggfx.GeoM{}, &svg.DrawOptions{Time: 700 * time.Millisecond})
	if px := imgLate.RGBAAt(50, 10); px != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("t=700ms pixel = %v, want solid green", px)
	}
}

func TestAnimatedCurrentColorAndInheritColor(t *testing.T) {
	// 1. currentColor referenced ONLY inside SMIL <animate> must set UsesCurrentColor()=true.
	d := parse(t, svgDoc("0 0 40 20", `
<rect id="r" x="0" y="0" width="20" height="20" fill="#ff0000">
  <animate attributeName="fill" from="currentColor" to="#0000ff" dur="1s" fill="freeze"/>
</rect>`))
	if !d.UsesCurrentColor() {
		t.Fatal("UsesCurrentColor() = false for animated currentColor, want true")
	}

	b := image.Rect(0, 0, 40, 20)
	img0 := drawOpts(d, b, ggfx.GeoM{}, &svg.DrawOptions{
		Time:         0,
		CurrentColor: color.NRGBA{0, 255, 0, 255},
	})
	if px := img0.RGBAAt(10, 10); px != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("animated currentColor t=0 = %v, want green", px)
	}
	imgMid := drawOpts(d, b, ggfx.GeoM{}, &svg.DrawOptions{
		Time:         500 * time.Millisecond,
		CurrentColor: color.NRGBA{0, 255, 0, 255},
	})
	if px := imgMid.RGBAAt(10, 10); px.G < 110 || px.B < 110 || px.R != 0 {
		t.Errorf("animated currentColor t=500ms = %v, want green/blue blend", px)
	}

	// 2. Animated color="currentColor" and color="inherit" inherit parent color (not transparent black).
	dInh := parse(t, svgDoc("0 0 40 20", `
<g color="#00ff00">
  <rect x="0" y="0" width="20" height="20" color="#ff0000" fill="currentColor">
    <set attributeName="color" to="currentColor" begin="0.5s"/>
  </rect>
  <rect x="20" y="0" width="20" height="20" color="#ff0000" fill="currentColor">
    <set attributeName="color" to="inherit" begin="0.5s"/>
  </rect>
</g>`))
	imgBefore := drawOpts(dInh, b, ggfx.GeoM{}, &svg.DrawOptions{Time: 0})
	if imgBefore.RGBAAt(10, 10) != (color.RGBA{255, 0, 0, 255}) || imgBefore.RGBAAt(30, 10) != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("before color set: (10,10)=%v (30,10)=%v, want red", imgBefore.RGBAAt(10, 10), imgBefore.RGBAAt(30, 10))
	}
	imgAfter := drawOpts(dInh, b, ggfx.GeoM{}, &svg.DrawOptions{Time: 600 * time.Millisecond})
	if imgAfter.RGBAAt(10, 10) != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("animated color=currentColor = %v, want parent green", imgAfter.RGBAAt(10, 10))
	}
	if imgAfter.RGBAAt(30, 10) != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("animated color=inherit = %v, want parent green", imgAfter.RGBAAt(30, 10))
	}
}

func TestStaticCSSColor4PercentOpacityTransformsAndStyleOrder(t *testing.T) {
	// 1. Static CSS Color 4, % opacity, gradient stop CSS Color 4 + % opacity, translate3d/scale3d.
	d := parse(t, svgDoc("0 0 140 40", `
<defs>
  <linearGradient id="cg">
    <stop offset="0" stop-color="rgba(0, 255, 0, 1)" stop-opacity="50%"/>
    <stop offset="1" stop-color="#00ff00ff" stop-opacity="50%"/>
  </linearGradient>
</defs>
<rect x="0" y="0" width="20" height="20" fill="rgba(255, 0, 0, 0.5)"/>
<rect x="20" y="0" width="20" height="20" fill="#00ff0080"/>
<rect x="40" y="0" width="20" height="20" fill="transparent"/>
<rect x="60" y="0" width="20" height="20" fill="rebeccapurple"/>
<rect x="80" y="0" width="20" height="20" fill="rgb(0 0 255 / 50%)"/>
<rect x="100" y="0" width="20" height="20" fill="#ff0000" opacity="50%"/>
<rect x="120" y="0" width="20" height="20" fill="url(#cg)"/>
<rect x="0" y="20" width="10" height="10" fill="#ff0000" style="transform: translate3d(20px, 0px, 5px) scale3d(200%, 100%, 1)"/>`))

	img := drawInto(d, image.Rect(0, 0, 140, 40), ggfx.GeoM{})
	if px := img.RGBAAt(10, 10); px.A < 120 || px.A > 136 || px.R < 120 {
		t.Errorf("rgba(255,0,0,0.5) = %v, want ~50%% red", px)
	}
	if px := img.RGBAAt(30, 10); px.A < 120 || px.A > 136 || px.G < 120 {
		t.Errorf("#00ff0080 = %v, want ~50%% green", px)
	}
	if px := img.RGBAAt(50, 10); px.A != 0 {
		t.Errorf("transparent = %v, want clear", px)
	}
	if px := img.RGBAAt(70, 10); px != (color.RGBA{0x66, 0x33, 0x99, 0xff}) {
		t.Errorf("rebeccapurple = %v, want #663399", px)
	}
	if px := img.RGBAAt(90, 10); px.A < 120 || px.A > 136 || px.B < 120 {
		t.Errorf("rgb(0 0 255 / 50%%) = %v, want ~50%% blue", px)
	}
	if px := img.RGBAAt(110, 10); px.A < 120 || px.A > 136 || px.R < 120 {
		t.Errorf("opacity=50%% = %v, want ~50%% red", px)
	}
	if px := img.RGBAAt(130, 10); px.A < 120 || px.A > 136 || px.G < 120 {
		t.Errorf("gradient stop-color rgba + stop-opacity 50%% = %v, want ~50%% green", px)
	}
	// translate3d(20px, 0, 5px) scale3d(200%, 100%, 1) maps [0..10] -> [20..40].
	if px := img.RGBAAt(30, 25); px.R != 255 {
		t.Errorf("translate3d+scale3d at (30,25) = %v, want red", px)
	}

	// 2. Static % translate with transform-box: view-box (default) vs fill-box.
	dPct := parse(t, svgDoc("0 0 100 40", `
<rect id="vb" x="0" y="0" width="20" height="15" fill="#ff0000" style="transform: translate(50%, 0)"/>
<rect id="fb" x="0" y="20" width="20" height="15" fill="#00ff00" style="transform: translate(50%, 0); transform-box: fill-box"/>`))
	imgPct := drawInto(dPct, image.Rect(0, 0, 100, 40), ggfx.GeoM{})
	// view-box (W=100): 50% -> +50px -> [50..70].
	if imgPct.RGBAAt(60, 7).R != 255 || imgPct.RGBAAt(20, 7).A != 0 {
		t.Errorf("view-box 50%% translate: (60,7)=%v (20,7)=%v", imgPct.RGBAAt(60, 7), imgPct.RGBAAt(20, 7))
	}
	// fill-box (rect W=20): 50% -> +10px -> [10..30].
	if imgPct.RGBAAt(20, 27).G != 255 || imgPct.RGBAAt(60, 27).A != 0 {
		t.Errorf("fill-box 50%% translate: (20,27)=%v (60,27)=%v", imgPct.RGBAAt(20, 27), imgPct.RGBAAt(60, 27))
	}

	// 3. Deterministic <style> multi-class order: stylesheet rule order (.a after .b) wins over class="a b".
	dStyle := parse(t, svgDoc("0 0 20 20", `
<style>
  .b { fill: #0000ff; }
  .a { fill: #ff0000; }
</style>
<rect class="a b" x="0" y="0" width="20" height="20"/>`))
	stDraw := drawInto(dStyle, image.Rect(0, 0, 20, 20), ggfx.GeoM{})
	stWith := drawOpts(dStyle, image.Rect(0, 0, 20, 20), ggfx.GeoM{}, &svg.DrawOptions{})
	if stDraw.RGBAAt(10, 10) != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("multi-class Draw = %v, want red (.a defined after .b)", stDraw.RGBAAt(10, 10))
	}
	if !bytes.Equal(stDraw.Pix, stWith.Pix) {
		t.Error("multi-class Draw differs from DrawWith")
	}

	// 4. Strict parse rejection on invalid color / opacity / transform.
	for _, bad := range []string{
		svgDoc("0 0 10 10", `<rect width="10" height="10" fill="notacolor"/>`),
		svgDoc("0 0 10 10", `<rect width="10" height="10" opacity="50%%"/>`),
		svgDoc("0 0 10 10", `<rect width="10" height="10" opacity="abc"/>`),
		svgDoc("0 0 10 10", `<rect width="10" height="10" transform="bogus(10)"/>`),
	} {
		if _, err := svg.Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("svg.Parse unexpectedly succeeded on invalid input: %s", bad)
		}
	}
}

func TestNodeOverrideOnMarkerMaskAndClipPath(t *testing.T) {
	// 1. Marker override: Fill and Visible(false) directly on <marker id="mk">.
	dMark := parse(t, svgDoc("0 0 100 40", `
<defs>
  <marker id="mk" viewBox="0 0 10 10" refX="5" refY="5" markerWidth="10" markerHeight="10" markerUnits="userSpaceOnUse">
    <rect width="10" height="10"/>
  </marker>
</defs>
<path d="M20 20 H80" stroke="#000" stroke-width="2" marker-end="url(#mk)"/>`))
	bMark := image.Rect(0, 0, 100, 40)

	imgFill := drawOpts(dMark, bMark, ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{
			"mk": svg.NodeOverride{}.Fill(svg.ColorPaint(color.NRGBA{0, 255, 0, 255})),
		},
	})
	if px := imgFill.RGBAAt(80, 17); px.G != 255 {
		t.Errorf("marker Fill override at (80,17) = %v, want green", px)
	}
	imgHid := drawOpts(dMark, bMark, ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{
			"mk": svg.NodeOverride{}.Visible(false),
		},
	})
	if px := imgHid.RGBAAt(80, 17); px.A != 0 {
		t.Errorf("marker Visible(false) override at (80,17) = %v, want clear", px)
	}

	// 2. Mask override: Fill(white) vs Visible(false) directly on <mask id="m">.
	dMask := parse(t, svgDoc("0 0 40 20", `
<defs>
  <mask id="m"><rect width="40" height="20"/></mask>
</defs>
<rect width="40" height="20" fill="#0000ff" mask="url(#m)"/>`))
	bMask := image.Rect(0, 0, 40, 20)

	// Default fill of rect inside mask is black -> lets nothing through; Fill(white) override reveals blue!
	imgMaskWhite := drawOpts(dMask, bMask, ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{
			"m": svg.NodeOverride{}.Fill(svg.ColorPaint(color.NRGBA{255, 255, 255, 255})),
		},
	})
	if px := imgMaskWhite.RGBAAt(20, 10); px.B != 255 {
		t.Errorf("mask Fill(white) override = %v, want blue", px)
	}
	imgMaskInvis := drawOpts(dMask, bMask, ggfx.GeoM{}, &svg.DrawOptions{
		Overrides: svg.Overrides{
			"m": svg.NodeOverride{}.Fill(svg.ColorPaint(color.NRGBA{255, 255, 255, 255})).Visible(false),
		},
	})
	if px := imgMaskInvis.RGBAAt(20, 10); px.A != 0 {
		t.Errorf("mask Visible(false) override = %v, want clear", px)
	}
}

func TestDocumentNextChangeContract(t *testing.T) {
	// 1. Discrete <set> animation.
	dSet := parse(t, svgDoc("0 0 20 20", `
<rect width="20" height="20" fill="#f00">
  <set attributeName="visibility" to="hidden" begin="0.5s" dur="0.5s"/>
</rect>`))
	t1, ok := dSet.NextChange(0)
	if !ok || t1 != 500*time.Millisecond {
		t.Fatalf("NextChange(0) = (%v, %v), want (500ms, true)", t1, ok)
	}
	t2, ok := dSet.NextChange(t1)
	if !ok || t2 != time.Second {
		t.Fatalf("NextChange(500ms) = (%v, %v), want (1s, true)", t2, ok)
	}
	if t3, ok := dSet.NextChange(t2); ok {
		t.Fatalf("NextChange(1s) = (%v, true), want (_, false)", t3)
	}

	// 2. Stepped CSS animation with repeating fractional timestamps (1/3s, 2/3s):
	// NextChange must ALWAYS return next > cur (never round down to <= cur) and terminate!
	dStep := parse(t, svgDoc("0 0 100 20", `
<style>
  @keyframes step3 {
    from { transform: translateX(0px); }
    to { transform: translateX(90px); }
  }
  #r { animation: step3 1s steps(3, end) forwards; }
</style>
<rect id="r" x="0" y="0" width="10" height="20" fill="#0f0"/>`))

	cur := time.Duration(0)
	steps := 0
	for {
		next, ok := dStep.NextChange(cur)
		if !ok {
			break
		}
		if next <= cur {
			t.Fatalf("NextChange(%v) = %v (not strictly greater)", cur, next)
		}
		cur = next
		steps++
		if steps > 10 {
			t.Fatalf("NextChange did not terminate after %d steps (stuck at %v)", steps, cur)
		}
	}
	if steps < 2 {
		t.Errorf("expected at least 2 stepped changes for steps(3, end), got %d", steps)
	}
}
