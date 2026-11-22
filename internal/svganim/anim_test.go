// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"encoding/xml"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Fake SceneView for unit testing ---

type fakeElem struct {
	tag     string
	id      string
	classes []string
	parent  ElemID
	attrs   map[string]string
}

type fakeScene struct {
	elems []fakeElem
}

func (f *fakeScene) add(tag, id string, classes []string, parent ElemID, attrs map[string]string) ElemID {
	e := ElemID(len(f.elems))
	f.elems = append(f.elems, fakeElem{
		tag:     tag,
		id:      id,
		classes: classes,
		parent:  parent,
		attrs:   attrs,
	})
	return e
}

func (f *fakeScene) Elems() int                { return len(f.elems) }
func (f *fakeScene) Tag(e ElemID) string       { return f.elems[e].tag }
func (f *fakeScene) ID(e ElemID) string        { return f.elems[e].id }
func (f *fakeScene) Classes(e ElemID) []string { return f.elems[e].classes }
func (f *fakeScene) Parent(e ElemID) ElemID    { return f.elems[e].parent }
func (f *fakeScene) ByID(id string) []ElemID {
	var out []ElemID
	for i, el := range f.elems {
		if el.id == id {
			out = append(out, ElemID(i))
		}
	}
	return out
}
func (f *fakeScene) RawAttr(e ElemID, name string) (string, bool) {
	if f.elems[e].attrs == nil {
		return "", false
	}
	v, ok := f.elems[e].attrs[name]
	return v, ok
}

func smilStart(tag string, kv ...string) xml.StartElement {
	se := xml.StartElement{Name: xml.Name{Local: tag}}
	for i := 0; i+1 < len(kv); i += 2 {
		k := kv[i]
		v := kv[i+1]
		if space, local, ok := strings.Cut(k, ":"); ok {
			se.Attr = append(se.Attr, xml.Attr{Name: xml.Name{Space: space, Local: local}, Value: v})
		} else {
			se.Attr = append(se.Attr, xml.Attr{Name: xml.Name{Local: k}, Value: v})
		}
	}
	return se
}

func approx(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func assertNear(t *testing.T, got, want, tol float64, label string) {
	t.Helper()
	if !approx(got, want, tol) {
		t.Errorf("%s: got %.6f, want %.6f (tol %.6f)", label, got, want, tol)
	}
}

func assertColorNear(t *testing.T, got, want [4]float64, tol float64, label string) {
	t.Helper()
	for i := range 4 {
		if !approx(got[i], want[i], tol) {
			t.Errorf("%s[%d]: got %.4f, want %.4f (full got=%v want=%v)", label, i, got[i], want[i], got, want)
			return
		}
	}
}

// --- 1. PropByName & IsSMILTag ---

func TestPropByNameAndSMILTag(t *testing.T) {
	cases := map[string]PropID{
		"fill":              PropFill,
		"FILL":              PropFill,
		"stroke-width":      PropStrokeWidth,
		"stroke-dasharray":  PropStrokeDasharray,
		"stroke-dashoffset": PropStrokeDashoffset,
		"transform":         PropTransform,
		"transform-origin":  PropTransformOrigin,
		"transform-box":     PropTransformBox,
		"cx":                PropCx,
		"r":                 PropR,
		"points":            PropPoints,
		"d":                 PropD,
		"visibility":        PropVisibility,
		"display":           PropDisplay,
		"offset":            PropNone, // reserved
		"stop-color":        PropNone, // reserved
		"xlink:href":        PropNone,
		"unknown":           PropNone,
	}
	for name, want := range cases {
		if got := PropByName(name); got != want {
			t.Errorf("PropByName(%q) = %v, want %v", name, got, want)
		}
	}
	for _, tag := range []string{"animate", "set", "animateTransform", "animateMotion", "animateColor", "mpath", "discard"} {
		if !IsSMILTag(tag) {
			t.Errorf("IsSMILTag(%q) = false, want true", tag)
		}
	}
	if IsSMILTag("path") {
		t.Error("IsSMILTag(path) = true, want false")
	}
}

// --- 2. Color & Value Parsing ---

func TestParseColorFormats(t *testing.T) {
	type tc struct {
		raw  string
		kind PaintKind
		col  [4]float64
	}
	tests := []tc{
		{"#f00", PaintColor, [4]float64{1, 0, 0, 1}},
		{"#0f08", PaintColor, [4]float64{0, 1, 0, 0x88 / 255.0}},
		{"#112233", PaintColor, [4]float64{0x11 / 255.0, 0x22 / 255.0, 0x33 / 255.0, 1}},
		{"#112233cc", PaintColor, [4]float64{0x11 / 255.0, 0x22 / 255.0, 0x33 / 255.0, 0xcc / 255.0}},
		{"rgb(255, 0, 128)", PaintColor, [4]float64{1, 0, 128.0 / 255.0, 1}},
		{"rgba(100%, 50%, 0%, 0.5)", PaintColor, [4]float64{1, 0.5, 0, 0.5}},
		{"rgb(255 0 128 / 50%)", PaintColor, [4]float64{1, 0, 128.0 / 255.0, 0.5}},
		{"rgba(0 255 0 / 0.25)", PaintColor, [4]float64{0, 1, 0, 0.25}},
		{"hsl(120, 100%, 50%)", PaintColor, [4]float64{0, 1, 0, 1}},
		{"hsla(240deg, 100%, 50%, 0.5)", PaintColor, [4]float64{0, 0, 1, 0.5}},
		{"hsl(0.5turn 100% 50% / 25%)", PaintColor, [4]float64{0, 1, 1, 0.25}},
		{"transparent", PaintColor, [4]float64{0, 0, 0, 0}},
		{"rebeccapurple", PaintColor, [4]float64{0x66 / 255.0, 0x33 / 255.0, 0x99 / 255.0, 1}},
		{"red", PaintColor, [4]float64{1, 0, 0, 1}},
		{"currentColor", PaintCurrentColor, [4]float64{}},
		{"none", PaintNone, [4]float64{}},
		{"inherit", PaintInherit, [4]float64{}},
		{"url(#grad1)", PaintURL, [4]float64{}},
	}
	for _, c := range tests {
		col, kind, err := ParseColor(c.raw)
		if err != nil {
			t.Errorf("ParseColor(%q) unexpected err: %v", c.raw, err)
			continue
		}
		if kind != c.kind {
			t.Errorf("ParseColor(%q) kind = %v, want %v", c.raw, kind, c.kind)
		}
		if kind == PaintColor {
			assertColorNear(t, col, c.col, 1e-3, "ParseColor("+c.raw+")")
		}
	}

	for _, bad := range []string{"", "#12", "#gggggg", "rgb(1,2)", "rgb(1 2 3, 4)", "notacolor"} {
		if _, _, err := ParseColor(bad); err == nil {
			t.Errorf("ParseColor(%q) expected error, got nil", bad)
		}
	}
}

func TestParseTransformAndToMatrix(t *testing.T) {
	ctx := &EvalCtx{
		Viewport: Rect{0, 0, 200, 100},
		FillBox:  func() Rect { return Rect{10, 20, 100, 50} },
	}

	// SVG attr syntax allows spaces and 3-arg rotate.
	v, err := ParseValue(PropTransform, "translate(10 20) rotate(90 50 50) scale(2)", SyntaxSVGAttr)
	if err != nil {
		t.Fatalf("ParseValue SVG transform: %v", err)
	}
	m := v.ToMatrix(ctx)
	// Point (50, 50) scaled by 2 -> (100, 100), rotated 90 deg around (50, 50):
	// dx=50, dy=50 -> rotated 90 CW -> (-50, 50) + (50, 50) = (0, 100), translated +(10,20) = (10, 120)
	x, y := m.Transform(50, 50)
	assertNear(t, x, 10, 1e-6, "svg transform x")
	assertNear(t, y, 120, 1e-6, "svg transform y")

	// CSS syntax with % translate (against FillBox 100x50), turn angle, rotateZ, scaleX, translate3d.
	vc, err := ParseValue(PropTransform, "translate(50%, 100%) rotateZ(0.25turn) scaleX(2)", SyntaxCSS)
	if err != nil {
		t.Fatalf("ParseValue CSS transform: %v", err)
	}
	mc := vc.ToMatrix(ctx)
	// Point (10, 0) -> scaleX(2) -> (20, 0) -> rotate(90deg CW) -> (0, 20) -> translate(50, 50) -> (50, 70)
	x, y = mc.Transform(10, 0)
	assertNear(t, x, 50, 1e-6, "css transform x")
	assertNear(t, y, 70, 1e-6, "css transform y")

	// CSS unitless 0 angle is valid, nonzero unitless angle is invalid.
	if _, err := ParseValue(PropTransform, "rotate(0)", SyntaxCSS); err != nil {
		t.Errorf("CSS rotate(0) should succeed: %v", err)
	}
	if _, err := ParseValue(PropTransform, "rotate(45)", SyntaxCSS); err == nil {
		t.Error("CSS rotate(45) without unit should fail")
	}
	// CSS requires commas between args.
	if _, err := ParseValue(PropTransform, "translate(10px 20px)", SyntaxCSS); err == nil {
		t.Error("CSS translate without comma should fail")
	}
	// 3D transforms rejected.
	for _, bad := range []string{"rotateX(45deg)", "rotateY(45deg)", "perspective(500px)", "matrix3d(1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1)"} {
		if _, err := ParseValue(PropTransform, bad, SyntaxCSS); err == nil {
			t.Errorf("CSS %s should be rejected", bad)
		}
	}
}

func TestParseTransformOrigin(t *testing.T) {
	v, err := ParseValue(PropTransformOrigin, "top left", SyntaxCSS)
	if err != nil {
		t.Fatalf("top left: %v", err)
	}
	assertNear(t, v.Origin[0].Val, 0, 1e-6, "x")
	assertNear(t, v.Origin[1].Val, 0, 1e-6, "y")

	v2, err := ParseValue(PropTransformOrigin, "bottom 20px", SyntaxCSS)
	if err != nil {
		t.Fatalf("bottom 20px: %v", err)
	}
	assertNear(t, v2.Origin[0].Val, 20, 1e-6, "x")
	assertNear(t, v2.Origin[1].Val, 100, 1e-6, "y")

	v3, err := ParseValue(PropTransformOrigin, "50% 25% 10px", SyntaxCSS)
	if err != nil {
		t.Fatalf("3-value origin: %v", err)
	}
	assertNear(t, v3.Origin[0].Val, 50, 1e-6, "x")
	assertNear(t, v3.Origin[1].Val, 25, 1e-6, "y")

	if _, err := ParseValue(PropTransformOrigin, "left right", SyntaxCSS); err == nil {
		t.Error("conflicting left right should fail")
	}
}

// --- 3. Value Interpolation: Premultiplied sRGB, Dasharray LCM, Transform & Matrix Decomposition ---

func TestPremulColorAndCurrentColorInterpolation(t *testing.T) {
	red, _ := ParseValue(PropFill, "#ff0000", SyntaxCSS)
	trans, _ := ParseValue(PropFill, "transparent", SyntaxCSS)
	ctx := &EvalCtx{Color: [4]float64{0, 0, 1, 1}}

	// Premultiplied sRGB midpoint between red(1,0,0,1) and transparent(0,0,0,0):
	// premul = (0.5, 0, 0, 0.5) -> straight = (1, 0, 0, 0.5)!
	mid := interpolateValues(red, trans, 0.5, PropFill, true, ctx)
	assertColorNear(t, mid.Color, [4]float64{1, 0, 0, 0.5}, 1e-4, "premul red->transparent")

	// currentColor resolved via ctx.Color (blue) -> interpolated with red = purple (0.5, 0, 0.5, 1)
	cc, _ := ParseValue(PropFill, "currentColor", SyntaxCSS)
	midCC := interpolateValues(red, cc, 0.5, PropFill, true, ctx)
	assertColorNear(t, midCC.Color, [4]float64{0.5, 0, 0.5, 1}, 1e-4, "red->currentColor")
}

func TestDasharrayLCMAndCap(t *testing.T) {
	a, _ := ParseValue(PropStrokeDasharray, "10 20", SyntaxCSS)   // len 2
	b, _ := ParseValue(PropStrokeDasharray, "0 30 60", SyntaxCSS) // len 3
	// LCM(2, 3) = 6: a=[10,20,10,20,10,20], b=[0,30,60,0,30,60] -> mid=[5,25,35,10,20,40]
	mid := interpolateValues(a, b, 0.5, PropStrokeDasharray, true, nil)
	want := []float64{5, 25, 35, 10, 20, 40}
	if len(mid.List) != len(want) {
		t.Fatalf("dash len=%d, want %d", len(mid.List), len(want))
	}
	for i := range want {
		assertNear(t, mid.List[i], want[i], 1e-6, "dash")
	}

	// LCM(7, 11) = 77 > 64 -> discrete fallback
	a7, _ := ParseValue(PropStrokeDasharray, "1 2 3 4 5 6 7", SyntaxCSS)
	b11, _ := ParseValue(PropStrokeDasharray, "10 10 10 10 10 10 10 10 10 10 10", SyntaxCSS)
	d1 := interpolateValues(a7, b11, 0.4, PropStrokeDasharray, true, nil)
	d2 := interpolateValues(a7, b11, 0.6, PropStrokeDasharray, true, nil)
	if len(d1.List) != 7 || len(d2.List) != 11 {
		t.Errorf("expected discrete fallback for LCM>64, got lens %d and %d", len(d1.List), len(d2.List))
	}
}

func TestTransformInterpolationAndDecompose(t *testing.T) {
	// Matching list: rotate(0) -> rotate(360deg) at t=0.5 MUST be 180deg (not identity!).
	r0, _ := ParseValue(PropTransform, "rotate(0deg)", SyntaxCSS)
	r360, _ := ParseValue(PropTransform, "rotate(360deg)", SyntaxCSS)
	rmid := interpolateValues(r0, r360, 0.5, PropTransform, true, nil)
	m := rmid.ToMatrix(nil)
	x, y := m.Transform(10, 0)
	assertNear(t, x, -10, 1e-5, "rotate 180 x")
	assertNear(t, y, 0, 1e-5, "rotate 180 y")

	// Prefix + identity padding: none -> translate(20px, 0) scale(3)
	noneVal, _ := ParseValue(PropTransform, "none", SyntaxCSS)
	ts, _ := ParseValue(PropTransform, "translate(20px, 0px) scale(3)", SyntaxCSS)
	midTS := interpolateValues(noneVal, ts, 0.5, PropTransform, true, nil)
	mTS := midTS.ToMatrix(nil)
	// translate(10, 0) * scale(2, 2): (1, 1) -> (12, 2)
	x, y = mTS.Transform(1, 1)
	assertNear(t, x, 12, 1e-5, "padded x")
	assertNear(t, y, 2, 1e-5, "padded y")

	// Mismatched function order -> 2D matrix decomposition fallback.
	a, _ := ParseValue(PropTransform, "translate(10px, 0px) rotate(0deg)", SyntaxCSS)
	b, _ := ParseValue(PropTransform, "rotate(90deg) translate(0px, -10px)", SyntaxCSS)
	// Note: rotate(90deg) * translate(0, -10) maps (0,0) -> (10, 0), rotation = 90deg.
	midD := interpolateValues(a, b, 0.5, PropTransform, true, nil)
	mD := midD.ToMatrix(nil)
	// Origin should stay at (10, 0), rotated 45deg.
	ox, oy := mD.Transform(0, 0)
	assertNear(t, ox, 10, 1e-5, "decomp origin x")
	assertNear(t, oy, 0, 1e-5, "decomp origin y")
	px, py := mD.Transform(1, 0)
	assertNear(t, px, 10+math.Sqrt2/2, 1e-5, "decomp rot45 x")
	assertNear(t, py, math.Sqrt2/2, 1e-5, "decomp rot45 y")
}

// --- 4. Easing ---

func TestEasingCurvesAndSteps(t *testing.T) {
	// Cubic bezier with y overshoot.
	e, err := parseCSSEasing("cubic-bezier(0.2, 1.5, 0.8, -0.5)")
	if err != nil {
		t.Fatalf("overshoot bezier: %v", err)
	}
	assertNear(t, e.eval(0), 0, 1e-6, "bezier(0)")
	assertNear(t, e.eval(1), 1, 1e-6, "bezier(1)")
	if e.eval(0.25) <= 0.5 {
		t.Errorf("expected overshoot/fast rise at 0.25, got %v", e.eval(0.25))
	}

	// Out-of-range x is clamped to [0, 1].
	ec, err := parseCSSEasing("cubic-bezier(-0.5, 0, 1.5, 1)")
	if err != nil {
		t.Fatalf("clamped x bezier: %v", err)
	}
	assertNear(t, ec.x1, 0, 1e-6, "x1 clamped")
	assertNear(t, ec.x2, 1, 1e-6, "x2 clamped")

	// Steps modes
	sEnd, _ := parseCSSEasing("steps(4, jump-end)")
	assertNear(t, sEnd.eval(0.0), 0.0, 1e-6, "jump-end 0")
	assertNear(t, sEnd.eval(0.24), 0.0, 1e-6, "jump-end 0.24")
	assertNear(t, sEnd.eval(0.25), 0.25, 1e-6, "jump-end 0.25")
	assertNear(t, sEnd.eval(0.99), 0.75, 1e-6, "jump-end 0.99")
	assertNear(t, sEnd.eval(1.0), 1.0, 1e-6, "jump-end 1")

	sStart, _ := parseCSSEasing("steps(4, jump-start)")
	assertNear(t, sStart.eval(0.0), 0.25, 1e-6, "jump-start 0")
	assertNear(t, sStart.eval(0.24), 0.25, 1e-6, "jump-start 0.24")
	assertNear(t, sStart.eval(0.25), 0.50, 1e-6, "jump-start 0.25")
	assertNear(t, sStart.eval(0.99), 1.0, 1e-6, "jump-start 0.99")

	sNone, _ := parseCSSEasing("steps(4, jump-none)")
	assertNear(t, sNone.eval(0.0), 0.0, 1e-6, "jump-none 0")
	assertNear(t, sNone.eval(0.25), 1.0/3.0, 1e-6, "jump-none 0.25")
	assertNear(t, sNone.eval(0.75), 1.0, 1e-6, "jump-none 0.75")

	sBoth, _ := parseCSSEasing("steps(4, jump-both)")
	assertNear(t, sBoth.eval(0.0), 0.2, 1e-6, "jump-both 0")
	assertNear(t, sBoth.eval(0.25), 0.4, 1e-6, "jump-both 0.25")
	assertNear(t, sBoth.eval(0.99), 0.8, 1e-6, "jump-both 0.99")
	assertNear(t, sBoth.eval(1.0), 1.0, 1e-6, "jump-both 1")

	// linear(...) rejected
	if _, err := parseCSSEasing("linear(0, 0.5 50%, 1)"); err == nil {
		t.Error("linear(...) should return error")
	}

	// SMIL keySplines
	ks, err := parseKeySplines("0.42 0 0.58 1; 0,0,1,1;")
	if err != nil || len(ks) != 2 {
		t.Fatalf("parseKeySplines: len=%d err=%v", len(ks), err)
	}
	assertNear(t, ks[0].eval(0.5), 0.5, 1e-4, "ease-in-out midpoint")
}

// --- 5. Clock Parsing & Timing ---

func TestClockParsing(t *testing.T) {
	smilCases := map[string]float64{
		"1.5s":       1.5,
		"200ms":      0.2,
		"2min":       120,
		"1h":         3600,
		"01:30":      90,
		"00:01:30.5": 90.5,
		"2.5":        2.5,
		"-0.5s":      -0.5,
	}
	for raw, want := range smilCases {
		got, err := parseSMILClock(raw)
		if err != nil {
			t.Errorf("parseSMILClock(%q) err: %v", raw, err)
			continue
		}
		assertNear(t, got, want, 1e-6, "clock "+raw)
	}

	// CSS time: s, ms, unitless 0; nonzero unitless rejected.
	if v, err := parseCSSTime("0"); err != nil || v != 0 {
		t.Errorf("parseCSSTime(0) = %v, %v", v, err)
	}
	if _, err := parseCSSTime("2"); err == nil {
		t.Error("parseCSSTime(2) without unit should fail")
	}
}

func TestSMILSyncbaseCyclicAndRestart(t *testing.T) {
	sc := &fakeScene{}
	root := sc.add("svg", "", nil, -1, nil)
	c1 := sc.add("circle", "c1", nil, root, nil)
	c2 := sc.add("circle", "c2", nil, root, nil)

	b := NewBuilder()
	// Classic cyclic loader loop: a runs [0,1], [2,3], ...; b runs [1,2], [3,4], ...
	b.SMILElement(smilStart("animate",
		"id", "a", "attributeName", "opacity",
		"from", "0", "to", "1", "dur", "1s",
		"begin", "0s; b.end", "fill", "freeze",
	), c1, false)
	b.SMILElement(smilStart("animate",
		"id", "b", "attributeName", "opacity",
		"from", "0", "to", "1", "dur", "1s",
		"begin", "a.end", "fill", "freeze",
	), c2, false)

	as, _, diags := b.Compile(sc)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if !as.Animated() {
		t.Fatal("expected Animated() == true")
	}

	tl := as.Timeline()
	if !tl.Infinite {
		t.Error("cyclic syncbase should be Infinite")
	}
	if tl.Loop != 2*time.Second {
		t.Errorf("Loop = %v, want 2s", tl.Loop)
	}

	base := Value{Kind: ValNumber, Num: 0}
	// Evaluate far in the future via detected period!
	vA, ok := as.Eval(c1, PropOpacity, 10000.25, base, nil)
	if !ok {
		t.Fatal("c1 should be active at 10000.25")
	}
	assertNear(t, vA.Num, 0.25, 1e-4, "c1 at 10000.25")

	vB, ok := as.Eval(c2, PropOpacity, 10001.75, base, nil)
	if !ok {
		t.Fatal("c2 should be active at 10001.75")
	}
	assertNear(t, vB.Num, 0.75, 1e-4, "c2 at 10001.75")

	// Restart modes: always, whenNotActive, never.
	sc2 := &fakeScene{}
	eAlways := sc2.add("rect", "r1", nil, -1, nil)
	eNotAct := sc2.add("rect", "r2", nil, -1, nil)
	eNever := sc2.add("rect", "r3", nil, -1, nil)
	b2 := NewBuilder()
	b2.SMILElement(smilStart("animate", "attributeName", "x", "from", "0", "to", "100", "dur", "2s", "begin", "0s; 1s; 4s", "restart", "always"), eAlways, false)
	b2.SMILElement(smilStart("animate", "attributeName", "x", "from", "0", "to", "100", "dur", "2s", "begin", "0s; 1s; 4s", "restart", "whenNotActive"), eNotAct, false)
	b2.SMILElement(smilStart("animate", "attributeName", "x", "from", "0", "to", "100", "dur", "2s", "begin", "0s; 1s; 4s", "restart", "never"), eNever, false)
	as2, _, _ := b2.Compile(sc2)
	baseX := Value{Kind: ValLength, Num: 0}

	// At t=1.5: always restarted at 1s -> 25; whenNotActive ignored 1s -> 75; never ignored 1s -> 75.
	va, _ := as2.Eval(eAlways, PropX, 1.5, baseX, nil)
	vn, _ := as2.Eval(eNotAct, PropX, 1.5, baseX, nil)
	vx, _ := as2.Eval(eNever, PropX, 1.5, baseX, nil)
	assertNear(t, va.Num, 25, 1e-5, "restart=always at 1.5")
	assertNear(t, vn.Num, 75, 1e-5, "restart=whenNotActive at 1.5")
	assertNear(t, vx.Num, 75, 1e-5, "restart=never at 1.5")

	// At t=4.5: always active -> 25; whenNotActive active -> 25; never inactive!
	va, okA := as2.Eval(eAlways, PropX, 4.5, baseX, nil)
	vn, okN := as2.Eval(eNotAct, PropX, 4.5, baseX, nil)
	_, okX := as2.Eval(eNever, PropX, 4.5, baseX, nil)
	if !okA || !okN || okX {
		t.Errorf("at t=4.5: okA=%v okN=%v okX=%v", okA, okN, okX)
	}
	assertNear(t, va.Num, 25, 1e-5, "always at 4.5")
	assertNear(t, vn.Num, 25, 1e-5, "whenNotActive at 4.5")
}

// --- 6. SMIL Forms, Sandwich, Additive, Accumulate, Discrete, Diagnostics ---

func TestSMILFormsSandwichAndAccumulate(t *testing.T) {
	sc := &fakeScene{}
	root := sc.add("svg", "", nil, -1, nil)
	rect := sc.add("rect", "box", nil, root, nil)
	circ := sc.add("circle", "dot", nil, root, nil)

	b := NewBuilder()
	// 1. Fractional repeatCount=2.5 + fill=freeze + accumulate=sum on x: from=0 to=10 dur=1s
	// Iter 0: [0..10], Iter 1: [10..20], Iter 2 (0..0.5): [20..25] -> freezes at 25!
	b.SMILElement(smilStart("animate",
		"attributeName", "x", "from", "0", "to", "10", "dur", "1s",
		"repeatCount", "2.5", "accumulate", "sum", "fill", "freeze",
	), rect, false)

	// 2. Discrete without keyTimes: 3 values split into 3 equal intervals [0,1/3), [1/3,2/3), [2/3,1].
	b.SMILElement(smilStart("animate",
		"attributeName", "y", "values", "10; 20; 30", "dur", "3s",
		"calcMode", "discrete", "fill", "freeze",
	), rect, false)

	// 3. Sandwich on r (via xlink:href="#dot"):
	//    Track 1 (begin=0s): replace 10->20 (at t=1.5 -> 17.5)
	//    Track 2 (begin=1s): additive=sum by=10 dur=1s (at t=1.5 -> +5 -> total 22.5)
	b.SMILElement(smilStart("animate",
		"xlink:href", "#dot", "attributeName", "r",
		"from", "10", "to", "20", "dur", "2s", "begin", "0s",
	), root, false)
	b.SMILElement(smilStart("animate",
		"href", "#dot", "attributeName", "r",
		"by", "10", "dur", "1s", "begin", "1s",
	), root, false)

	// 4. <set> and <animateTransform> (rotate around pivot with accumulate=sum, plus additive=sum translate)
	b.SMILElement(smilStart("set",
		"attributeName", "visibility", "to", "hidden", "begin", "1s", "dur", "2s",
	), circ, false)
	b.SMILElement(smilStart("animateTransform",
		"attributeName", "transform", "type", "rotate",
		"from", "0 50 50", "to", "90 50 50", "dur", "1s",
		"repeatCount", "2", "accumulate", "sum", "fill", "freeze",
	), circ, false)
	b.SMILElement(smilStart("animateTransform",
		"attributeName", "transform", "type", "translate",
		"from", "0 0", "to", "20 0", "dur", "2s",
		"additive", "sum", "fill", "freeze",
	), circ, false)

	// 5. Unsupported elements / attrs produce diagnostics leniently.
	b.SMILElement(smilStart("animateMotion", "path", "M0,0 L10,10", "dur", "1s"), rect, false)
	b.SMILElement(smilStart("animate", "attributeName", "stop-color", "from", "red", "to", "blue", "dur", "1s"), rect, false)
	b.SMILElement(smilStart("animate", "attributeName", "opacity", "from", "0", "to", "1", "dur", "1s", "begin", "click"), rect, false)

	as, _, diags := b.Compile(sc)
	if len(diags) < 3 {
		t.Errorf("expected >= 3 diagnostics for unsupported SMIL, got %d: %v", len(diags), diags)
	}

	// Verify fractional repeat + accumulate + freeze
	vx, ok := as.Eval(rect, PropX, 5.0, Value{Kind: ValLength, Num: 0}, nil)
	if !ok {
		t.Fatal("rect x should be frozen at t=5")
	}
	assertNear(t, vx.Num, 25, 1e-5, "accumulated frozen x")

	// Verify discrete equal n intervals
	vy0, _ := as.Eval(rect, PropY, 0.9, Value{Kind: ValLength, Num: 0}, nil)
	vy1, _ := as.Eval(rect, PropY, 1.1, Value{Kind: ValLength, Num: 0}, nil)
	vy2, _ := as.Eval(rect, PropY, 2.1, Value{Kind: ValLength, Num: 0}, nil)
	assertNear(t, vy0.Num, 10, 1e-5, "discrete [0,1)")
	assertNear(t, vy1.Num, 20, 1e-5, "discrete [1,2)")
	assertNear(t, vy2.Num, 30, 1e-5, "discrete [2,3]")

	// Verify sandwich on dot r at t=1.5: 17.5 + 5 = 22.5
	vr, ok := as.Eval(circ, PropR, 1.5, Value{Kind: ValLength, Num: 5}, nil)
	if !ok {
		t.Fatal("circ r should be active at 1.5")
	}
	assertNear(t, vr.Num, 22.5, 1e-5, "sandwich r")

	// Verify <set> visibility: inactive at 0.5, hidden at 1.5, removed at 3.5.
	if _, ok := as.Eval(circ, PropVisibility, 0.5, Value{Kind: ValKeyword, Keyword: "visible"}, nil); ok {
		t.Error("set should be inactive before begin")
	}
	vVis, ok := as.Eval(circ, PropVisibility, 1.5, Value{Kind: ValKeyword, Keyword: "visible"}, nil)
	if !ok || vVis.Keyword != "hidden" {
		t.Errorf("set at 1.5: got (%v, %v), want hidden", vVis, ok)
	}
	if _, ok := as.Eval(circ, PropVisibility, 3.5, Value{Kind: ValKeyword, Keyword: "visible"}, nil); ok {
		t.Error("set should revert after dur with fill=remove")
	}

	// Verify animateTransform rotate accumulate (180deg around 50,50) + additive translate(20,0) at t=2.0:
	// Post-multiply: R(180 around 50,50) * T(20, 0)
	// Point (50, 50) -> T(20,0) -> (70, 50) -> R(180 around 50,50) -> (30, 50)!
	vTf, ok := as.Eval(circ, PropTransform, 2.0, Value{Kind: ValTransformList}, nil)
	if !ok {
		t.Fatal("circ transform should be frozen at 2.0")
	}
	tx, ty := vTf.ToMatrix(nil).Transform(50, 50)
	assertNear(t, tx, 30, 1e-4, "accumulated rotate + additive translate x")
	assertNear(t, ty, 50, 1e-4, "accumulated rotate + additive translate y")
}

// --- 7. CSS Tokenizer, Cascade, @media, @keyframes, Shorthand, List Cycling ---

func TestCSSCascadeAndClassDecls(t *testing.T) {
	sc := &fakeScene{}
	svg := sc.add("svg", "", nil, -1, nil)
	g := sc.add("g", "grp", []string{"wrap"}, svg, nil)
	p := sc.add("path", "hero", []string{"a", "b"}, g, nil)

	css := `
	/* comment with { braces } and ; */
	@import url("ignored.css");
	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}
	@media (prefers-reduced-motion: reduce) {
		.a { fill: gray; }
	}
	@media (prefers-color-scheme: dark) {
		#hero { stroke: white; }
	}
	@media screen and (max-width: 600px) {
		.a { fill: purple; }
	}
	* { opacity: 0.1; }
	path { fill: red; stroke-width: 2; }
	.a { fill: green; content: "a{b}c"; }
	g > path.a.b { fill: blue; }
	#hero { fill: orange; }
	.a { fill: yellow !important; }
	.a:hover, .b[data-x="1"], .a + .b { fill: black; }
	`

	b := NewBuilder()
	b.Stylesheet(css)
	_, sheet, diags := b.Compile(sc)

	// Unsupported selectors should produce diags without breaking valid rules.
	if len(diags) != 3 {
		t.Errorf("expected 3 unsupported selector diags, got %d: %v", len(diags), diags)
	}

	// Stored @media blocks (inactive by default).
	ss := sheet.impl.(*stylesheetImpl)
	if len(ss.mediaBlocks) != 2 {
		t.Fatalf("expected 2 stored media blocks, got %d", len(ss.mediaBlocks))
	}

	// DeclsFor cascade order: normal ascending specificity (* < path < .a < g>path.a.b < #hero), then !important (.a).
	decls := sheet.DeclsFor(sc, p)
	var fills []string
	for _, d := range decls {
		if d.Name == "fill" {
			fills = append(fills, d.Value)
		}
	}
	wantFills := []string{"red", "green", "blue", "orange", "yellow"}
	if strings.Join(fills, ",") != strings.Join(wantFills, ",") {
		t.Errorf("cascade fills = %v, want %v", fills, wantFills)
	}

	// ClassDecls legacy compatibility: only pure single-.class rules, never corrupted by @keyframes!
	cls := sheet.ClassDecls()
	if len(cls["a"]) != 3 { // green, content:"a{b}c", yellow !important
		t.Errorf("ClassDecls['a'] = %+v, want 3 decls", cls["a"])
	}
	if _, hasFrom := cls["from"]; hasFrom {
		t.Error("@keyframes 'from' leaked into ClassDecls!")
	}
}

func TestCSSKeyframesAnimationFeatures(t *testing.T) {
	sc := &fakeScene{}
	svg := sc.add("svg", "", nil, -1, nil)
	e1 := sc.add("rect", "e1", []string{"fade"}, svg, nil)
	e2 := sc.add("rect", "e2", []string{"paused"}, svg, nil)
	e3 := sc.add("rect", "e3", []string{"multi"}, svg, nil)
	e4 := sc.add("rect", "e4", nil, svg, map[string]string{
		"style": "animation: inlinePulse 2s linear 0s 1 both",
	})

	css := `
	/* Duplicate @keyframes name: last wins! */
	@keyframes fadeAnim {
		0% { opacity: 0.9; }
		100% { opacity: 0.9; }
	}
	@-webkit-keyframes fadeAnim {
		50% { opacity: 0.2; animation-timing-function: linear; }
		100% { opacity: 1 !important; opacity: 0.8; }
		/* missing 0% -> synthesized from base! */
	}
	@keyframes pulse {
		from { opacity: 0; }
		to { opacity: 1; }
	}
	@keyframes slide {
		from { transform: translate(0px, 0px); }
		to { transform: translate(100px, 0px); }
	}
	@keyframes inlinePulse {
		from { opacity: 0.2; }
		to { opacity: 0.6; }
	}
	@keyframes bad3d {
		from { transform: rotateX(0deg); }
		to { transform: rotateX(90deg); }
	}

	.fade {
		animation: 2s linear 1s 2 alternate both fadeAnim;
	}
	.paused {
		animation: pulse 4s linear -1s paused;
	}
	.multi {
		animation-name: pulse, slide, bad3d;
		animation-duration: 2s, 4s;
		animation-timing-function: linear;
		animation-fill-mode: forwards;
	}
	`

	b := NewBuilder()
	b.Stylesheet(css)
	// Also add a SMIL opacity animation on e4 to verify CSS overrides SMIL on top!
	b.SMILElement(smilStart("animate", "attributeName", "opacity", "from", "0.9", "to", "0.9", "dur", "5s"), e4, false)

	as, _, diags := b.Compile(sc)
	if len(diags) == 0 {
		t.Error("expected diag for bad3d rotateX")
	}

	// e1: delay=1s, dur=2s, 2 iterations alternate, fill=both.
	// Base opacity = 0.6 -> synthesized 0% = 0.6, 50% = 0.2, 100% = 0.8 (!important ignored).
	base06 := Value{Kind: ValNumber, Num: 0.6}
	// t=0.5 (before delay=1s): backwards fill -> 0% = base = 0.6
	v, ok := as.Eval(e1, PropOpacity, 0.5, base06, nil)
	if !ok {
		t.Fatal("e1 should have backwards fill at 0.5")
	}
	assertNear(t, v.Num, 0.6, 1e-5, "e1 backwards fill")
	// t=2.0 (iter 0, progress 0.5): 50% -> 0.2
	v, _ = as.Eval(e1, PropOpacity, 2.0, base06, nil)
	assertNear(t, v.Num, 0.2, 1e-5, "e1 at 2.0")
	// t=3.5 (iter 1 reverse, rawP=0.25 -> dirP=0.75): midpoint of [50%=0.2, 100%=0.8] = 0.5
	v, _ = as.Eval(e1, PropOpacity, 3.5, base06, nil)
	assertNear(t, v.Num, 0.5, 1e-5, "e1 alternate reverse at 3.5")
	// t=10.0 (after end=5s, forwards fill on iter 1 reverse -> dirP=0 -> 0% = 0.6)
	v, _ = as.Eval(e1, PropOpacity, 10.0, base06, nil)
	assertNear(t, v.Num, 0.6, 1e-5, "e1 forwards fill after alternate even count")

	// e2: paused with negative delay -1s and dur 4s -> pinned at progress 0.25 regardless of t!
	for _, ts := range []float64{-5, 0, 2, 100} {
		vp, ok := as.Eval(e2, PropOpacity, ts, Value{Kind: ValNumber, Num: 0}, nil)
		if !ok {
			t.Fatalf("e2 paused should be active at t=%v", ts)
		}
		assertNear(t, vp.Num, 0.25, 1e-5, "e2 paused")
	}

	// e3: list cycling (pulse dur=2s, slide dur=4s, bad3d dropped).
	vp3, _ := as.Eval(e3, PropOpacity, 1.0, Value{Kind: ValNumber, Num: 0}, nil)
	assertNear(t, vp3.Num, 0.5, 1e-5, "e3 pulse opacity")
	vt3, _ := as.Eval(e3, PropTransform, 1.0, Value{Kind: ValTransformList}, nil)
	x3, _ := vt3.ToMatrix(nil).Transform(0, 0)
	assertNear(t, x3, 25, 1e-5, "e3 slide transform")

	// e4: inline style CSS overrides SMIL!
	v4, _ := as.Eval(e4, PropOpacity, 1.0, Value{Kind: ValNumber, Num: 0}, nil)
	assertNear(t, v4.Num, 0.4, 1e-5, "CSS inline overrides SMIL")
}

// --- 8. Timeline & NextChange ---

func TestTimelineAndNextChange(t *testing.T) {
	sc := &fakeScene{}
	e1 := sc.add("rect", "r1", []string{"a"}, -1, nil)
	e2 := sc.add("rect", "r2", []string{"b"}, -1, nil)
	e3 := sc.add("rect", "r3", []string{"c"}, -1, nil)

	css := `
	@keyframes k { from { opacity: 0; } to { opacity: 1; } }
	.a { animation: k 1.5s linear 0s infinite; }
	.b { animation: k 1s linear 0s infinite alternate; }
	.c { animation: k 2s linear 3s 1 forwards; }
	`
	// Periods: .a = 1.5s, .b = 2*1s = 2s -> LCM(1.5s, 2s) = 6s!
	// Finite .c ends at 5s -> End = 5s, LoopStart = 5s, Span = max(1.5, 2, 5) = 5s.
	b := NewBuilder()
	b.Stylesheet(css)
	as, _, _ := b.Compile(sc)

	tl := as.Timeline()
	if !tl.Infinite {
		t.Error("expected Infinite=true")
	}
	if tl.End != 5*time.Second {
		t.Errorf("End = %v, want 5s", tl.End)
	}
	if tl.Loop != 6*time.Second {
		t.Errorf("Loop = %v, want 6s", tl.Loop)
	}
	if tl.LoopStart != 5*time.Second {
		t.Errorf("LoopStart = %v, want 5s", tl.LoopStart)
	}
	if tl.Span != 5*time.Second {
		t.Errorf("Span = %v, want 5s", tl.Span)
	}
	_ = e1
	_ = e2
	_ = e3

	// LCM > 60s cap -> Loop = 0, LoopStart = 0.
	scCap := &fakeScene{}
	scCap.add("rect", "", []string{"p1"}, -1, nil)
	scCap.add("rect", "", []string{"p2"}, -1, nil)
	bCap := NewBuilder()
	bCap.Stylesheet(`
		@keyframes k { from { opacity: 0; } to { opacity: 1; } }
		.p1 { animation: k 7s linear infinite; }
		.p2 { animation: k 11s linear infinite; }
	`)
	asCap, _, _ := bCap.Compile(scCap)
	tlCap := asCap.Timeline()
	if !tlCap.Infinite || tlCap.Loop != 0 || tlCap.LoopStart != 0 {
		t.Errorf("LCM>60s cap failed: %+v", tlCap)
	}
	if tlCap.Span != 11*time.Second {
		t.Errorf("Span = %v, want 11s", tlCap.Span)
	}

	// NextChange for delayed, stepped, discrete, and finished tracks.
	scN := &fakeScene{}
	en := scN.add("rect", "", []string{"stepped"}, -1, nil)
	bN := NewBuilder()
	bN.Stylesheet(`
		@keyframes k { from { opacity: 0; } to { opacity: 1; } }
		.stepped { animation: k 4s steps(4, end) 2s 1 forwards; }
	`)
	asN, _, _ := bN.Compile(scN)
	_ = en

	// Before delay (t=0.5): next change at begin=2.0
	nt, ok := asN.NextChange(0.5)
	if !ok {
		t.Fatal("expected NextChange before delay")
	}
	assertNear(t, nt, 2.0, 1e-6, "next change before delay")

	// Inside stepped interval (t=2.2): next step at 3.0
	nt, ok = asN.NextChange(2.2)
	if !ok {
		t.Fatal("expected NextChange inside steps")
	}
	assertNear(t, nt, 3.0, 1e-6, "next change step 1")

	// After End (t=6.5): no more changes
	if _, ok := asN.NextChange(6.5); ok {
		t.Error("expected no NextChange after End")
	}
}

// --- 9. Concurrency / Race Safety ---

func TestConcurrentEvalAndCascade(t *testing.T) {
	sc := &fakeScene{}
	root := sc.add("svg", "", nil, -1, nil)
	e := sc.add("circle", "c", []string{"spin"}, root, nil)

	b := NewBuilder()
	b.Stylesheet(`
		@keyframes spin { from { transform: rotate(0deg); opacity: 0; } to { transform: rotate(360deg); opacity: 1; } }
		.spin { animation: spin 2s ease-in-out infinite; fill: #ff0000; }
	`)
	b.SMILElement(smilStart("animate", "attributeName", "r", "from", "5", "to", "15", "dur", "1s", "repeatCount", "indefinite"), e, false)
	as, sheet, _ := b.Compile(sc)

	ctx := &EvalCtx{Viewport: Rect{0, 0, 100, 100}, Color: [4]float64{1, 1, 1, 1}}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range 100 {
				ts := float64(id*100+i) * 0.017
				as.Eval(e, PropTransform, ts, Value{Kind: ValTransformList}, ctx)
				as.Eval(e, PropOpacity, ts, Value{Kind: ValNumber, Num: 1}, ctx)
				as.Eval(e, PropR, ts, Value{Kind: ValLength, Num: 5}, ctx)
				as.NextChange(ts)
				as.Timeline()
				as.AnimatedProps(e)
				sheet.DeclsFor(sc, e)
			}
		}(g)
	}
	wg.Wait()
}

// --- 10. Additional Edge Cases: SMIL to/by/spline/min/max/end/repeatDur, CSS visibility/zero-dur/reversed-easing, Length %, NextChange ---

func TestAdditionalEdgeCases(t *testing.T) {
	// 1. SMIL to-only, from+by (transform & number), by-only scale, keyTimes+keySplines, min/max/end/repeatDur, animateColor, paced diag.
	sc := &fakeScene{}
	eTo := sc.add("rect", "to", nil, -1, nil)
	eFromBy := sc.add("rect", "fb", nil, -1, nil)
	eScaleBy := sc.add("rect", "sb", nil, -1, nil)
	eSpline := sc.add("rect", "sp", nil, -1, nil)
	eMinMax := sc.add("rect", "mm", nil, -1, nil)
	eColor := sc.add("rect", "cl", nil, -1, nil)

	b := NewBuilder()
	// to-only ignores additive=sum and interpolates from base (20 -> 100)
	b.SMILElement(smilStart("animate",
		"attributeName", "width", "to", "100", "dur", "2s", "additive", "sum",
	), eTo, false)
	// from+by on animateTransform rotate: 30 -> 120 around (10, 20)
	b.SMILElement(smilStart("animateTransform",
		"attributeName", "transform", "type", "rotate",
		"from", "30 10 20", "by", "90", "dur", "2s",
	), eFromBy, false)
	// by-only on animateTransform scale: additive post-multiply scale(1..2)
	b.SMILElement(smilStart("animateTransform",
		"attributeName", "transform", "type", "scale",
		"by", "1", "dur", "2s",
	), eScaleBy, false)
	// keyTimes + keySplines (calcMode=spline)
	b.SMILElement(smilStart("animate",
		"attributeName", "opacity",
		"values", "0; 0.5; 1", "keyTimes", "0; 0.25; 1",
		"calcMode", "spline", "keySplines", "0 0 1 1; 0 0 1 1",
		"dur", "4s",
	), eSpline, false)
	// end + repeatDur + max + paced (diag)
	b.SMILElement(smilStart("animate",
		"attributeName", "x", "from", "0", "to", "100", "dur", "1s",
		"repeatCount", "10", "repeatDur", "5s", "end", "2.5s", "max", "2s",
		"calcMode", "paced", "fill", "freeze",
	), eMinMax, false)
	// animateColor
	b.SMILElement(smilStart("animateColor",
		"attributeName", "fill", "from", "#ff0000", "to", "#0000ff", "dur", "2s",
	), eColor, false)

	as, _, diags := b.Compile(sc)
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "paced") {
		t.Errorf("expected 1 paced diag, got %v", diags)
	}

	// to-only at t=1.0 from base=20 -> 60 (NOT 80!)
	vTo, ok := as.Eval(eTo, PropWidth, 1.0, Value{Kind: ValLength, Num: 20}, nil)
	if !ok {
		t.Fatal("eTo should be active")
	}
	assertNear(t, vTo.Num, 60, 1e-5, "to-only midpoint")

	// from+by rotate at t=1.0 -> 75 deg around (10,20)
	vFB, ok := as.Eval(eFromBy, PropTransform, 1.0, Value{Kind: ValTransformList}, nil)
	if !ok || len(vFB.Xf) != 1 {
		t.Fatalf("eFromBy expected 1 transform, got %+v", vFB)
	}
	assertNear(t, vFB.Xf[0].Args[0], 75, 1e-5, "from+by rotate angle")
	assertNear(t, vFB.Xf[0].Args[1], 10, 1e-5, "from+by rotate cx")

	// by-only scale at t=1.0 on base scale(2) -> scale(2) * scale(1.5) = scale(3)
	baseScale, _ := ParseValue(PropTransform, "scale(2)", SyntaxSVGAttr)
	vSB, ok := as.Eval(eScaleBy, PropTransform, 1.0, baseScale, nil)
	if !ok {
		t.Fatal("eScaleBy should be active")
	}
	sx, sy := vSB.ToMatrix(nil).Transform(1, 1)
	assertNear(t, sx, 3, 1e-5, "by-only scale x")
	assertNear(t, sy, 3, 1e-5, "by-only scale y")

	// keyTimes + keySplines: at t=1.0 (p=0.25) -> 0.5; at t=2.5 (p=0.625, midpoint of [0.25,1]) -> 0.75
	vSp1, _ := as.Eval(eSpline, PropOpacity, 1.0, Value{Kind: ValNumber, Num: 0}, nil)
	vSp2, _ := as.Eval(eSpline, PropOpacity, 2.5, Value{Kind: ValNumber, Num: 0}, nil)
	assertNear(t, vSp1.Num, 0.5, 1e-4, "keyTimes at 0.25")
	assertNear(t, vSp2.Num, 0.75, 1e-4, "keyTimes at 0.625")

	// max=2s caps end=2.5s and repeatDur=5s: frozen at end of iter 1 (p=1.0 -> 100)
	vMM, ok := as.Eval(eMinMax, PropX, 3.0, Value{Kind: ValLength, Num: 0}, nil)
	if !ok {
		t.Fatal("eMinMax should be frozen")
	}
	assertNear(t, vMM.Num, 100, 1e-5, "eMinMax frozen")

	// animateColor midpoint red->blue = (0.5, 0, 0.5, 1)
	vCl, ok := as.Eval(eColor, PropFill, 1.0, Value{Kind: ValPaint, Paint: PaintColor}, nil)
	if !ok {
		t.Fatal("eColor should be active")
	}
	assertColorNear(t, vCl.Color, [4]float64{0.5, 0, 0.5, 1}, 1e-4, "animateColor")

	// 2. Mixed length units (px + %) resolved per-axis against Viewport.
	ctx := &EvalCtx{Viewport: Rect{0, 0, 200, 100}}
	lx1, _ := ParseValue(PropX, "10px", SyntaxCSS)
	lx2, _ := ParseValue(PropX, "50%", SyntaxCSS) // 50% of 200 = 100
	midX := interpolateValues(lx1, lx2, 0.5, PropX, true, ctx)
	assertNear(t, midX.Num, 55, 1e-5, "mixed px/% X")

	ly1, _ := ParseValue(PropY, "0px", SyntaxCSS)
	ly2, _ := ParseValue(PropY, "50%", SyntaxCSS) // 50% of 100 = 50
	midY := interpolateValues(ly1, ly2, 0.5, PropY, true, ctx)
	assertNear(t, midY.Num, 25, 1e-5, "mixed px/% Y")

	// 3. CSS visibility interpolation, zero-dur, zero-iterations, reversed easing, none-before-name shorthand.
	scCSS := &fakeScene{}
	eVis := scCSS.add("rect", "", []string{"vis"}, -1, nil)
	eZero := scCSS.add("rect", "", []string{"zero"}, -1, nil)
	eRev := scCSS.add("rect", "", []string{"rev"}, -1, nil)
	eNoneFirst := scCSS.add("rect", "", []string{"nf"}, -1, nil)

	bCSS := NewBuilder()
	bCSS.Stylesheet(`
		@keyframes visAnim { from { visibility: hidden; } to { visibility: visible; } }
		@keyframes opAnim { from { opacity: 0; } to { opacity: 1; } }
		.vis { animation: visAnim 2s linear 0s 1 forwards; }
		.zero { animation: opAnim 0s linear 1s 1 forwards; }
		.rev { animation: opAnim 2s ease-in 0s 1 reverse forwards; }
		.nf { animation: none 2s linear opAnim; }
	`)
	asCSS, _, _ := bCSS.Compile(scCSS)

	// CSS visibility: visible throughout (0, 1) when either endpoint is visible!
	vV0, _ := asCSS.Eval(eVis, PropVisibility, 0.0, Value{Kind: ValKeyword, Keyword: "hidden"}, nil)
	vV1, _ := asCSS.Eval(eVis, PropVisibility, 0.05, Value{Kind: ValKeyword, Keyword: "hidden"}, nil)
	if vV0.Keyword != "hidden" || vV1.Keyword != "visible" {
		t.Errorf("CSS visibility: at 0=%q, at 0.05=%q (want hidden, visible)", vV0.Keyword, vV1.Keyword)
	}

	// Zero duration with delay=1s and forwards fill: inactive at 0.5, end value (1.0) at 1.0!
	if _, ok := asCSS.Eval(eZero, PropOpacity, 0.5, Value{Kind: ValNumber, Num: 0}, nil); ok {
		t.Error("zero-dur should be inactive before delay")
	}
	vZ, ok := asCSS.Eval(eZero, PropOpacity, 1.0, Value{Kind: ValNumber, Num: 0}, nil)
	if !ok {
		t.Fatal("zero-dur forwards should be active at delay")
	}
	assertNear(t, vZ.Num, 1.0, 1e-5, "zero-dur forwards value")

	// Reversed easing: ease-in(1 - 0.25) = ease-in(0.75) < 0.75 -> at t=0.5 (rawP=0.25),
	// directed progress is 0.75, ease-in(0.75) ≈ 0.568 (drops fast initially, like ease-out!).
	vR, _ := asCSS.Eval(eRev, PropOpacity, 0.5, Value{Kind: ValNumber, Num: 0}, nil)
	if vR.Num >= 0.70 || vR.Num <= 0.40 {
		t.Errorf("reversed ease-in at 0.25 expected ~0.57, got %.4f", vR.Num)
	}

	// Shorthand "none 2s linear opAnim": fill-mode=none, name=opAnim!
	vNF, ok := asCSS.Eval(eNoneFirst, PropOpacity, 1.0, Value{Kind: ValNumber, Num: 0}, nil)
	if !ok {
		t.Fatal("none-before-name shorthand should still animate opAnim")
	}
	assertNear(t, vNF.Num, 0.5, 1e-5, "none-before-name shorthand")

	// 4. NextChange for SMIL discrete returns next boundary.
	scNC := &fakeScene{}
	eDisc := scNC.add("rect", "", nil, -1, nil)
	bNC := NewBuilder()
	bNC.SMILElement(smilStart("animate",
		"attributeName", "x", "values", "0; 10; 20", "dur", "3s",
		"calcMode", "discrete", "fill", "remove",
	), eDisc, false)
	asNC, _, _ := bNC.Compile(scNC)
	nt, ok := asNC.NextChange(0.5)
	if !ok {
		t.Fatal("discrete NextChange at 0.5")
	}
	assertNear(t, nt, 1.0, 1e-5, "discrete next change 1")
	nt, ok = asNC.NextChange(2.5)
	if !ok {
		t.Fatal("discrete NextChange at 2.5 (remove at 3.0)")
	}
	assertNear(t, nt, 3.0, 1e-5, "discrete next change end remove")
	if _, ok := asNC.NextChange(3.5); ok {
		t.Error("discrete NextChange after end should be false")
	}
}

// --- 11. Code Review Fixes: NextChange Contract, UsesCurrentColor, ParseDecls, Unused @keyframes, Shorthand Edge Cases ---

func TestNextChangeContract(t *testing.T) {
	// 1. Continuous animation: delayed begin returns begin > t, active returns (0, false), settled returns (0, false).
	scCont := &fakeScene{}
	eCont := scCont.add("rect", "", []string{"c"}, -1, nil)
	bCont := NewBuilder()
	bCont.Stylesheet(`
		@keyframes fade { from { opacity: 0; } to { opacity: 1; } }
		.c { animation: fade 2s linear 1s 1 forwards; }
	`)
	asCont, _, _ := bCont.Compile(scCont)
	_ = eCont

	if nt, ok := asCont.NextChange(0.0); !ok || !approx(nt, 1.0, 1e-6) {
		t.Errorf("delayed continuous at t=0: got (%v, %v), want (1.0, true)", nt, ok)
	}
	if nt, ok := asCont.NextChange(1.0); ok || nt != 0 {
		t.Errorf("active continuous at t=1.0: got (%v, %v), want (0, false)", nt, ok)
	}
	if nt, ok := asCont.NextChange(2.0); ok || nt != 0 {
		t.Errorf("active continuous at t=2.0: got (%v, %v), want (0, false)", nt, ok)
	}
	if nt, ok := asCont.NextChange(3.0); ok || nt != 0 {
		t.Errorf("settled continuous at t=3.0: got (%v, %v), want (0, false)", nt, ok)
	}
	if nt, ok := asCont.NextChange(5.0); ok || nt != 0 {
		t.Errorf("settled continuous at t=5.0: got (%v, %v), want (0, false)", nt, ok)
	}

	// 2. Continuous SMIL to-only animation also returns (0, false) while active.
	scTo := &fakeScene{}
	eTo := scTo.add("rect", "", nil, -1, nil)
	bTo := NewBuilder()
	bTo.SMILElement(smilStart("animate",
		"attributeName", "width", "to", "100", "begin", "1s", "dur", "2s", "fill", "freeze",
	), eTo, false)
	asTo, _, _ := bTo.Compile(scTo)
	if nt, ok := asTo.NextChange(0.5); !ok || !approx(nt, 1.0, 1e-6) {
		t.Errorf("SMIL to-only before begin: got (%v, %v), want (1.0, true)", nt, ok)
	}
	if nt, ok := asTo.NextChange(1.5); ok || nt != 0 {
		t.Errorf("SMIL to-only active continuous: got (%v, %v), want (0, false)", nt, ok)
	}
	if nt, ok := asTo.NextChange(3.5); ok || nt != 0 {
		t.Errorf("SMIL to-only settled: got (%v, %v), want (0, false)", nt, ok)
	}

	// 3. Mixed timeline: continuous track [1, 3) + discrete track [0, 4).
	scMix := &fakeScene{}
	eMix := scMix.add("rect", "", nil, -1, nil)
	bMix := NewBuilder()
	bMix.SMILElement(smilStart("animate",
		"attributeName", "opacity", "from", "0", "to", "1", "begin", "1s", "dur", "2s",
	), eMix, false)
	bMix.SMILElement(smilStart("animate",
		"attributeName", "x", "values", "0; 50", "begin", "0s", "dur", "4s",
		"calcMode", "discrete", "fill", "remove",
	), eMix, false)
	asMix, _, _ := bMix.Compile(scMix)

	if nt, ok := asMix.NextChange(0.5); !ok || !approx(nt, 1.0, 1e-6) {
		t.Errorf("mixed at t=0.5: got (%v, %v), want (1.0, true)", nt, ok)
	}
	if nt, ok := asMix.NextChange(1.5); ok || nt != 0 {
		t.Errorf("mixed during continuous at t=1.5: got (%v, %v), want (0, false)", nt, ok)
	}
	if nt, ok := asMix.NextChange(3.5); !ok || !approx(nt, 4.0, 1e-6) {
		t.Errorf("mixed after continuous settled at t=3.5: got (%v, %v), want (4.0, true)", nt, ok)
	}
	if nt, ok := asMix.NextChange(4.0); ok || nt != 0 {
		t.Errorf("mixed settled at t=4.0: got (%v, %v), want (0, false)", nt, ok)
	}

	// 4. SMIL <set>: remove vs freeze.
	scSet := &fakeScene{}
	eSetRem := scSet.add("rect", "", nil, -1, nil)
	bSetRem := NewBuilder()
	bSetRem.SMILElement(smilStart("set",
		"attributeName", "visibility", "to", "hidden", "begin", "1s", "dur", "2s", "fill", "remove",
	), eSetRem, false)
	asSetRem, _, _ := bSetRem.Compile(scSet)

	if nt, ok := asSetRem.NextChange(0.0); !ok || !approx(nt, 1.0, 1e-6) {
		t.Errorf("<set> before begin: got (%v, %v), want (1.0, true)", nt, ok)
	}
	if nt, ok := asSetRem.NextChange(1.0); !ok || !approx(nt, 3.0, 1e-6) {
		t.Errorf("<set> at begin: got (%v, %v), want (3.0, true)", nt, ok)
	}
	if nt, ok := asSetRem.NextChange(2.0); !ok || !approx(nt, 3.0, 1e-6) {
		t.Errorf("<set> during active: got (%v, %v), want (3.0, true)", nt, ok)
	}
	if nt, ok := asSetRem.NextChange(3.0); ok || nt != 0 {
		t.Errorf("<set> at end: got (%v, %v), want (0, false)", nt, ok)
	}

	bSetFrz := NewBuilder()
	bSetFrz.SMILElement(smilStart("set",
		"attributeName", "visibility", "to", "hidden", "begin", "1s", "dur", "2s", "fill", "freeze",
	), eSetRem, false)
	asSetFrz, _, _ := bSetFrz.Compile(scSet)
	if nt, ok := asSetFrz.NextChange(0.5); !ok || !approx(nt, 1.0, 1e-6) {
		t.Errorf("<set freeze> before begin: got (%v, %v), want (1.0, true)", nt, ok)
	}
	if nt, ok := asSetFrz.NextChange(1.0); ok || nt != 0 {
		t.Errorf("<set freeze> at begin: got (%v, %v), want (0, false)", nt, ok)
	}

	// 5. SMIL calcMode="discrete": strictly > t at every boundary.
	scDisc := &fakeScene{}
	eDisc := scDisc.add("rect", "", nil, -1, nil)
	bDisc := NewBuilder()
	bDisc.SMILElement(smilStart("animate",
		"attributeName", "x", "values", "0; 10; 20", "begin", "1s", "dur", "3s",
		"calcMode", "discrete", "fill", "remove",
	), eDisc, false)
	asDisc, _, _ := bDisc.Compile(scDisc)

	for _, tc := range []struct {
		t, want float64
		ok      bool
	}{
		{0.0, 1.0, true},
		{1.0, 2.0, true},
		{1.5, 2.0, true},
		{2.0, 3.0, true},
		{3.0, 4.0, true},
		{4.0, 0.0, false},
	} {
		nt, ok := asDisc.NextChange(tc.t)
		if ok != tc.ok || (ok && !approx(nt, tc.want, 1e-6)) {
			t.Errorf("discrete NextChange(%v): got (%v, %v), want (%v, %v)", tc.t, nt, ok, tc.want, tc.ok)
		}
		if ok && nt <= tc.t {
			t.Errorf("discrete NextChange(%v) = %v is not strictly > t", tc.t, nt)
		}
	}

	// 6. CSS steps(4, end): strictly > t across every step including final forwards jump at t=6.0.
	scSteps := &fakeScene{}
	scSteps.add("rect", "", []string{"stepped"}, -1, nil)
	bSteps := NewBuilder()
	bSteps.Stylesheet(`
		@keyframes k { from { opacity: 0; } to { opacity: 1; } }
		.stepped { animation: k 4s steps(4, end) 2s 1 forwards; }
	`)
	asSteps, _, _ := bSteps.Compile(scSteps)

	for _, tc := range []struct {
		t, want float64
		ok      bool
	}{
		{0.5, 2.0, true},
		{2.0, 3.0, true},
		{2.5, 3.0, true},
		{3.0, 4.0, true},
		{4.0, 5.0, true},
		{5.5, 6.0, true},
		{6.0, 0.0, false},
		{6.5, 0.0, false},
	} {
		nt, ok := asSteps.NextChange(tc.t)
		if ok != tc.ok || (ok && !approx(nt, tc.want, 1e-6)) {
			t.Errorf("steps(4,end) NextChange(%v): got (%v, %v), want (%v, %v)", tc.t, nt, ok, tc.want, tc.ok)
		}
		if ok && nt <= tc.t {
			t.Errorf("steps(4,end) NextChange(%v) = %v is not strictly > t", tc.t, nt)
		}
	}
}

func TestUsesCurrentColor(t *testing.T) {
	// Nil AnimSet -> false.
	var nilAS *AnimSet
	if nilAS.UsesCurrentColor() {
		t.Error("nil AnimSet.UsesCurrentColor() = true, want false")
	}

	// No currentColor -> false.
	scPlain := &fakeScene{}
	ePlain := scPlain.add("rect", "", []string{"a"}, -1, nil)
	bPlain := NewBuilder()
	bPlain.Stylesheet(`
		@keyframes k { from { fill: #ff0000; opacity: 0; } to { fill: #0000ff; opacity: 1; } }
		.a { animation: k 2s linear infinite; }
	`)
	bPlain.SMILElement(smilStart("animate", "attributeName", "stroke", "from", "red", "to", "green", "dur", "1s"), ePlain, false)
	asPlain, _, _ := bPlain.Compile(scPlain)
	if asPlain.UsesCurrentColor() {
		t.Error("plain colors AnimSet.UsesCurrentColor() = true, want false")
	}

	// SMIL fill currentColor -> true.
	scSMILCC := &fakeScene{}
	eSMILCC := scSMILCC.add("rect", "", nil, -1, nil)
	bSMILCC := NewBuilder()
	bSMILCC.SMILElement(smilStart("animate", "attributeName", "fill", "from", "red", "to", "currentColor", "dur", "1s"), eSMILCC, false)
	asSMILCC, _, _ := bSMILCC.Compile(scSMILCC)
	if !asSMILCC.UsesCurrentColor() {
		t.Error("SMIL fill=currentColor: UsesCurrentColor() = false, want true")
	}

	// SMIL PropColor -> true.
	scSMILColor := &fakeScene{}
	eSMILColor := scSMILColor.add("g", "", nil, -1, nil)
	bSMILColor := NewBuilder()
	bSMILColor.SMILElement(smilStart("animate", "attributeName", "color", "from", "red", "to", "blue", "dur", "1s"), eSMILColor, false)
	asSMILColor, _, _ := bSMILColor.Compile(scSMILColor)
	if !asSMILColor.UsesCurrentColor() {
		t.Error("SMIL color animation: UsesCurrentColor() = false, want true")
	}

	// CSS stroke currentColor -> true.
	scCSSCC := &fakeScene{}
	scCSSCC.add("path", "", []string{"cc"}, -1, nil)
	bCSSCC := NewBuilder()
	bCSSCC.Stylesheet(`
		@keyframes k { from { stroke: currentColor; } to { stroke: #00ff00; } }
		.cc { animation: k 1s linear; }
	`)
	asCSSCC, _, _ := bCSSCC.Compile(scCSSCC)
	if !asCSSCC.UsesCurrentColor() {
		t.Error("CSS stroke=currentColor: UsesCurrentColor() = false, want true")
	}

	// CSS PropColor -> true.
	scCSSColor := &fakeScene{}
	scCSSColor.add("g", "", []string{"col"}, -1, nil)
	bCSSColor := NewBuilder()
	bCSSColor.Stylesheet(`
		@keyframes k { from { color: red; } to { color: blue; } }
		.col { animation: k 1s linear; }
	`)
	asCSSColor, _, _ := bCSSColor.Compile(scCSSColor)
	if !asCSSColor.UsesCurrentColor() {
		t.Error("CSS color animation: UsesCurrentColor() = false, want true")
	}
}

func TestParseDecls(t *testing.T) {
	raw := `/* ignored: bad; fill: black; */ fill: red !important; content: "a;b/*keep*/c"; background: url(data:image/svg+xml;utf8,<svg/>); stroke-width: 2px /* inline */ ! IMPORTANT; font-family: 'foo;bar'`
	got := ParseDecls(raw)
	want := []Decl{
		{Name: "fill", Value: "red", Important: true},
		{Name: "content", Value: `"a;b/*keep*/c"`, Important: false},
		{Name: "background", Value: "url(data:image/svg+xml;utf8,<svg/>)", Important: false},
		{Name: "stroke-width", Value: "2px", Important: true},
		{Name: "font-family", Value: "'foo;bar'", Important: false},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseDecls len=%d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ParseDecls[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestUnusedKeyframesNotAnimated(t *testing.T) {
	sc := &fakeScene{}
	root := sc.add("svg", "", nil, -1, nil)
	sc.add("rect", "r1", []string{"static"}, root, nil)
	sc.add("circle", "c1", []string{"noneAnim"}, root, nil)

	b := NewBuilder()
	b.Stylesheet(`
		@keyframes unusedSpin {
			from { transform: rotate(0deg); }
			to { transform: rotate(360deg); }
		}
		.static { fill: blue; }
		.noneAnim { animation-name: none; }
	`)
	as, _, diags := b.Compile(sc)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if as.Animated() {
		t.Error("unused @keyframes should NOT mark AnimSet.Animated() == true")
	}

	// Referencing the keyframe marks Animated() == true.
	scUsed := &fakeScene{}
	scUsed.add("rect", "r1", []string{"used"}, -1, nil)
	bUsed := NewBuilder()
	bUsed.Stylesheet(`
		@keyframes usedSpin {
			from { opacity: 0; }
			to { opacity: 1; }
		}
		.used { animation: usedSpin 1s linear; }
	`)
	asUsed, _, _ := bUsed.Compile(scUsed)
	if !asUsed.Animated() {
		t.Error("referenced @keyframes should mark AnimSet.Animated() == true")
	}
}

func TestCSSAnimationShorthandEdgeCases(t *testing.T) {
	// 1. @keyframes infs / nans / -infs / infms must NOT be parsed as time tokens.
	for _, name := range []string{"infs", "nans", "-infs", "+infs", "infms", "nanms"} {
		sh := parseAnimationShorthandItem(name + " 2s linear")
		if sh.name != name || sh.duration != "2s" || sh.timing != "linear" {
			t.Errorf("shorthand(%q 2s linear) = %+v, want name=%q dur=2s timing=linear", name, sh, name)
		}
	}

	// End-to-end evaluation with @keyframes infs.
	scInf := &fakeScene{}
	eInf := scInf.add("rect", "", []string{"infs"}, -1, nil)
	bInf := NewBuilder()
	bInf.Stylesheet(`
		@keyframes infs { from { opacity: 0; } to { opacity: 1; } }
		.infs { animation: infs 2s linear; }
	`)
	asInf, _, diags := bInf.Compile(scInf)
	if len(diags) != 0 {
		t.Fatalf("@keyframes infs diags: %v", diags)
	}
	vInf, ok := asInf.Eval(eInf, PropOpacity, 1.0, Value{Kind: ValNumber, Num: 0}, nil)
	if !ok {
		t.Fatal("@keyframes infs should animate")
	}
	assertNear(t, vInf.Num, 0.5, 1e-5, "@keyframes infs midpoint")

	// 2. SVGO-minified unitless 0 as delay when iteration-count is present vs iteration-count=0 when alone.
	shSVGO := parseAnimationShorthandItem("spin 2s linear 0 infinite")
	if shSVGO.name != "spin" || shSVGO.duration != "2s" || shSVGO.timing != "linear" || shSVGO.delay != "0" || shSVGO.iteration != "infinite" {
		t.Errorf("shorthand(spin 2s linear 0 infinite) = %+v", shSVGO)
	}

	shSVGOCount := parseAnimationShorthandItem("spin 2s linear 0 3")
	if shSVGOCount.name != "spin" || shSVGOCount.duration != "2s" || shSVGOCount.delay != "0" || shSVGOCount.iteration != "3" {
		t.Errorf("shorthand(spin 2s linear 0 3) = %+v", shSVGOCount)
	}

	shIterAfter := parseAnimationShorthandItem("spin 2s infinite 0")
	if shIterAfter.name != "spin" || shIterAfter.duration != "2s" || shIterAfter.delay != "0" || shIterAfter.iteration != "infinite" {
		t.Errorf("shorthand(spin 2s infinite 0) = %+v", shIterAfter)
	}

	shZeroIter := parseAnimationShorthandItem("spin 2s 0")
	if shZeroIter.name != "spin" || shZeroIter.duration != "2s" || shZeroIter.delay != "0s" || shZeroIter.iteration != "0" {
		t.Errorf("shorthand(spin 2s 0) = %+v", shZeroIter)
	}

	// End-to-end check: "animation: spin 2s linear 0 infinite" compiles and repeats infinitely.
	scSVGO := &fakeScene{}
	eSVGO := scSVGO.add("rect", "", []string{"s"}, -1, nil)
	bSVGO := NewBuilder()
	bSVGO.Stylesheet(`
		@keyframes spin { from { opacity: 0; } to { opacity: 1; } }
		.s { animation: spin 2s linear 0 infinite; }
	`)
	asSVGO, _, diags := bSVGO.Compile(scSVGO)
	if len(diags) != 0 {
		t.Fatalf("SVGO shorthand diags: %v", diags)
	}
	if !asSVGO.Timeline().Infinite {
		t.Error("spin 2s linear 0 infinite should be Infinite")
	}
	vS, ok := asSVGO.Eval(eSVGO, PropOpacity, 5.0, Value{Kind: ValNumber, Num: 0}, nil)
	if !ok {
		t.Fatal("spin 2s linear 0 infinite should be active at t=5.0")
	}
	assertNear(t, vS.Num, 0.5, 1e-5, "SVGO infinite at t=5.0")
}
