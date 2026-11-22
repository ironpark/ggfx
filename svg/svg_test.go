// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/svg"
)

func TestDrawMapsTheViewBoxThroughTheGeoM(t *testing.T) {
	d := parse(t, svgDoc("10 10 10 10", `<rect x="10" y="10" width="5" height="10" fill="#fff"/>`))
	if x, y, w, h := d.ViewBox(); x != 10 || y != 10 || w != 10 || h != 10 {
		t.Fatalf("viewBox %v %v %v %v; want 10 10 10 10", x, y, w, h)
	}
	var g ggfx.GeoM
	g.Translate(-10, -10)
	g.Scale(2, 2)
	dst := drawInto(d, image.Rect(0, 0, 20, 20), g)
	if a := dst.RGBAAt(5, 10).A; a != 0xff {
		t.Errorf("inside the rect alpha %d; want opaque", a)
	}
	if a := dst.RGBAAt(15, 10).A; a != 0 {
		t.Errorf("outside the rect alpha %d; want clear", a)
	}
}

func TestParseRefusesWhatItCannotDraw(t *testing.T) {
	src := `<svg viewBox="0 0 1 1"><rect width="1" height="1" fill="hsl(0,,)"/></svg>`
	if _, err := svg.Parse(strings.NewReader(src)); err == nil {
		t.Errorf("%q parsed", src)
	}
}

func TestParseSurvivesAUseOfItself(t *testing.T) {
	src := svgDoc("0 0 1 1", `<g id="a"><use xlink:href="#a"/></g>`)
	if d, err := svg.Parse(strings.NewReader(src)); err == nil {
		drawInto(d, image.Rect(0, 0, 4, 4), ggfx.GeoM{})
	}
}

// draw draws src at s into a w by h image.
func draw(t *testing.T, src string, s float64, w, h int) *image.RGBA {
	t.Helper()
	return drawInto(parse(t, src), image.Rect(0, 0, w, h), scale(s))
}

// A rect given one radius rounds its corners by it both ways.
func TestARectWithOneRadiusIsRounded(t *testing.T) {
	dst := draw(t, svgDoc("0 0 40 20", `<rect width="40" height="20" rx="8" fill="#fff"/>`), 2, 80, 40)
	if a := dst.RGBAAt(0, 0).A; a != 0 {
		t.Errorf("the corner alpha %d; want clear", a)
	}
	if a := dst.RGBAAt(40, 20).A; a != 0xff {
		t.Errorf("the middle alpha %d; want opaque", a)
	}
}

// A shape drawn through a clip shows only where the clip's shapes are, in
// the user space of the element that names it; through a mask, as light as
// the mask is there.
func TestClipsAndMasksLetThroughWhatTheyCover(t *testing.T) {
	dst := draw(t, svgDoc("0 0 40 20", `
<defs><clipPath id="c"><rect width="10" height="10"/></clipPath>
<mask id="m"><rect width="20" height="20" fill="#fff"/><rect x="20" width="20" height="20" fill="#000"/></mask></defs>
<g clip-path="url(#c)" transform="translate(5,5)"><rect width="40" height="20" fill="#f00"/></g>
<rect y="16" width="40" height="4" fill="#0f0" mask="url(#m)"/>`), 2, 80, 40)
	if c := dst.RGBAAt(20, 20); c.R != 0xff || c.A != 0xff {
		t.Errorf("inside the clip, moved with its group: %v; want red", c)
	}
	if c := dst.RGBAAt(4, 4); c.A != 0 {
		t.Errorf("before the moved clip: %v; want clear", c)
	}
	if c := dst.RGBAAt(36, 10); c.A != 0 {
		t.Errorf("past the clip: %v; want clear", c)
	}
	if c := dst.RGBAAt(20, 36); c.G != 0xff {
		t.Errorf("under the mask's white: %v; want green", c)
	}
	if c := dst.RGBAAt(60, 36); c.A != 0 {
		t.Errorf("under the mask's black: %v; want clear", c)
	}
}

// A path's markers are drawn at its ends, turned as it runs there and
// sized by its stroke.
func TestMarkersAreDrawnAtAPathsEnds(t *testing.T) {
	dst := draw(t, goldenDoc(`
<defs><marker id="dot" viewBox="0 0 10 10" refX="5" refY="5" markerWidth="4" markerHeight="4"><rect width="10" height="10" fill="#00f"/></marker>
<marker id="arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="5" markerHeight="5" orient="auto"><path d="M0,0 L10,5 L0,10 z" fill="#f00"/></marker></defs>
<path d="M10,50 L90,50" stroke="#000" stroke-width="2" marker-start="url(#dot)" marker-end="url(#arrow)"/>`), 1, 100, 100)
	if c := dst.RGBAAt(10, 50); c.B != 0xff {
		t.Errorf("at the start: %v; want the blue marker", c)
	}
	if c := dst.RGBAAt(86, 50); c.R != 0xff {
		t.Errorf("before the end: %v; want the red arrow", c)
	}
	if c := dst.RGBAAt(86, 44); c.R == 0xff {
		t.Errorf("beside the arrow, outside it: %v", c)
	}
}

// Drawing changes nothing in a document, so goroutines can draw it at
// once, each drawing what it would alone.
func TestDrawFromGoroutinesAtOnce(t *testing.T) {
	d := parse(t, iconSVG)
	drawAt := func(size int) *image.RGBA {
		return drawInto(d, image.Rect(0, 0, size, size), scale(float64(size)/100))
	}
	sizes := []int{24, 100, 256, 600}
	want := map[int][]byte{}
	for _, size := range sizes {
		want[size] = drawAt(size).Pix
	}
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			size := sizes[i%len(sizes)]
			if got := drawAt(size).Pix; !bytes.Equal(got, want[size]) {
				t.Errorf("drawn at %d from a goroutine: differs", size)
			}
		})
	}
	wg.Wait()
}

// A gradient inside a mask ends with it: what follows is read as it would
// be without it, here a definition that a use then places.
func TestAGradientInAMaskEndsWithIt(t *testing.T) {
	dst := draw(t, goldenDoc(`
<mask id="m"><linearGradient id="fade"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#000"/></linearGradient><rect width="100" height="40" fill="url(#fade)"/></mask>
<rect width="100" height="40" fill="#c00" mask="url(#m)"/>
<defs><g id="later"><rect width="20" height="20" fill="#06c"/></g></defs>
<use href="#later" x="40" y="60"/>`), 1, 100, 100)
	if c := dst.RGBAAt(50, 70); c != (color.RGBA{0, 0x66, 0xcc, 0xff}) {
		t.Errorf("where the use places the definition: %v; want it", c)
	}
	if c := dst.RGBAAt(10, 50); c.A != 0 {
		t.Errorf("where the definition is, undrawn: %v; want clear", c)
	}
	if c := dst.RGBAAt(5, 20); c.R < 0xb0 || c.G != 0 {
		t.Errorf("under the mask's white: %v; want red", c)
	}
}

// A gradient inside a clip or mask is the document's, as one in defs: a
// shape outside it is filled with it, whether the clip or mask is used
// after the shape or never.
func TestAGradientInAClipIsTheDocuments(t *testing.T) {
	dst := draw(t, goldenDoc(`
<clipPath id="never"><linearGradient id="l"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient><rect width="10" height="10"/></clipPath>
<mask id="later"><linearGradient id="m"><stop offset="0" stop-color="#0f0"/><stop offset="1" stop-color="#0f0"/></linearGradient><rect width="100" height="100" fill="#fff"/></mask>
<rect width="100" height="50" fill="url(#l)"/>
<rect y="50" width="100" height="25" fill="url(#m)"/>
<rect y="75" width="100" height="25" fill="#00f" mask="url(#later)"/>`), 1, 100, 100)
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{0, 25, color.RGBA{0xfe, 0, 0x01, 0xff}}, {99, 25, color.RGBA{0x01, 0, 0xfe, 0xff}},
		{50, 60, color.RGBA{0, 0xff, 0, 0xff}},
		{50, 90, color.RGBA{0, 0, 0xff, 0xff}},
	} {
		if c := dst.RGBAAt(tc.x, tc.y); c != tc.want {
			t.Errorf("(%d, %d): %v; want %v", tc.x, tc.y, c, tc.want)
		}
	}
}

const pentagram = `25,5 36.76,41.18 5.98,18.82 44.02,18.82 13.24,41.18`

// fill-rule, as an attribute, a style property, a class's and a group's,
// leaves a self-intersecting shape's middle empty by evenodd and fills it
// by nonzero; a stroke is filled whatever the rule.
func TestFillRule(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		hole      bool
	}{
		{"default", `<polygon points="` + pentagram + `"/>`, false},
		{"nonzero", `<polygon points="` + pentagram + `" fill-rule="nonzero"/>`, false},
		{"attribute", `<polygon points="` + pentagram + `" fill-rule="evenodd"/>`, true},
		{"style", `<polygon points="` + pentagram + `" style="fill-rule: evenodd"/>`, true},
		{"class", `<defs><style>.eo{fill-rule:evenodd}</style></defs><polygon class="eo" points="` + pentagram + `"/>`, true},
		{"group", `<g fill-rule="evenodd"><g><polygon points="` + pentagram + `"/></g></g>`, true},
		{"overridden", `<g fill-rule="evenodd"><polygon points="` + pentagram + `" fill-rule="nonzero"/></g>`, false},
		{"use", `<defs><polygon id="p" points="` + pentagram + `"/></defs><use href="#p" fill-rule="evenodd"/>`, true},
	} {
		dst := draw(t, svgDoc("0 0 50 50", tc.src), 1, 50, 50)
		if a := dst.RGBAAt(25, 25).A; (a == 0) != tc.hole || (a != 0 && a != 0xff) {
			t.Errorf("%s: the middle's alpha %#x; want a hole %t", tc.name, a, tc.hole)
		}
		if a := dst.RGBAAt(25, 10).A; a != 0xff {
			t.Errorf("%s: a point's alpha %#x; want opaque", tc.name, a)
		}
	}
	// Where a stroke crosses itself, its outline's overlaps are filled.
	dst := draw(t, svgDoc("0 0 50 50", `<polygon points="`+pentagram+`" fill="none" stroke="#000" stroke-width="3" fill-rule="evenodd"/>`), 1, 50, 50)
	// The lines from the top meet the cross bar at x = 19.0 and 31.0.
	for _, x := range []int{18, 31} {
		if a := dst.RGBAAt(x, 18).A; a != 0xff {
			t.Errorf("where the stroke crosses itself at (%d, 18): alpha %#x; want opaque", x, a)
		}
	}
}

// clip-rule, on a clip's shape or on the clipPath, fills it by evenodd; a
// fill-rule there does not.
func TestClipRule(t *testing.T) {
	for _, tc := range []struct {
		name, clip string
		hole       bool
	}{
		{"default", `<clipPath id="c"><polygon points="` + pentagram + `"/></clipPath>`, false},
		{"shape", `<clipPath id="c"><polygon points="` + pentagram + `" clip-rule="evenodd"/></clipPath>`, true},
		{"clipPath", `<clipPath id="c" clip-rule="evenodd"><polygon points="` + pentagram + `"/></clipPath>`, true},
		{"fill-rule", `<clipPath id="c"><polygon points="` + pentagram + `" fill-rule="evenodd"/></clipPath>`, false},
	} {
		dst := draw(t, svgDoc("0 0 50 50", `<defs>`+tc.clip+`</defs><rect width="50" height="50" fill="#f00" clip-path="url(#c)"/>`), 1, 50, 50)
		if a := dst.RGBAAt(25, 25).A; (a == 0) != tc.hole || (a != 0 && a != 0xff) {
			t.Errorf("%s: the middle's alpha %#x; want a hole %t", tc.name, a, tc.hole)
		}
		if c := dst.RGBAAt(25, 10); c != (color.RGBA{0xff, 0, 0, 0xff}) {
			t.Errorf("%s: a point: %v; want red", tc.name, c)
		}
	}
}

// A document drawn into an image whose bounds do not start at 0 draws
// there as into one that does.
func TestDrawIntoOffsetBounds(t *testing.T) {
	for _, doc := range goldenDocs {
		var g ggfx.GeoM
		g.Scale(1.1, 1.1)
		g.Translate(2.5, 1.75)
		d := parse(t, doc.src)
		at0 := drawInto(d, image.Rect(0, 0, 100, 80), g)
		off := drawInto(d, image.Rect(7, -11, 107, 69), g)
		if !bytes.Equal(at0.Pix, off.Pix) {
			t.Errorf("%s: drawn into %v differs from into %v", doc.name, off.Rect, at0.Rect)
		}
	}
}

// A gradient's stops keep their order: one at an offset less than the one
// before is at that one's, and offsets are in [0, 1].
func TestGradientStopsKeepTheirOrder(t *testing.T) {
	dst := draw(t, svgDoc("0 0 100 10", `<defs>
<linearGradient id="g" gradientUnits="userSpaceOnUse" x2="100"><stop offset="0.6" stop-color="#f00"/><stop offset="0.3" stop-color="#0f0"/><stop offset="1.5" stop-color="#00f"/></linearGradient>
<linearGradient id="h" gradientUnits="userSpaceOnUse" x2="100"><stop offset="-1" stop-color="#ff0"/><stop offset="0.5" stop-color="#ff0"/><stop offset="0.5" stop-color="#0ff"/></linearGradient></defs>
<rect width="100" height="5" fill="url(#g)"/><rect y="5" width="100" height="5" fill="url(#h)"/>`), 1, 100, 10)
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{30, 2, color.RGBA{0xff, 0, 0, 0xff}}, // before the first stop, at 0.6
		{59, 2, color.RGBA{0xff, 0, 0, 0xff}},
		{99, 2, color.RGBA{0, 3, 252, 0xff}},     // green to blue, from 0.6 to 1
		{49, 7, color.RGBA{0xff, 0xff, 0, 0xff}}, // a hard stop at 0.5
		{50, 7, color.RGBA{0, 0xff, 0xff, 0xff}},
	} {
		if c := dst.RGBAAt(tc.x, tc.y); c != tc.want {
			t.Errorf("(%d, %d): %v; want %v", tc.x, tc.y, c, tc.want)
		}
	}
}

// An objectBoundingBox gradient spans the shape's geometry, without its
// stroke, which takes the same gradient as its fill.
func TestBoundingBoxLeavesOutTheStroke(t *testing.T) {
	dst := draw(t, goldenDoc(`<defs>
<linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient></defs>
<rect x="20" y="20" width="60" height="60" fill="url(#g)" stroke="url(#g)" stroke-width="20"/>
<path d="M20,95 H80" stroke="url(#g)" stroke-width="6"/>`), 1, 100, 100)
	red, blue := color.RGBA{0xff, 0, 0, 0xff}, color.RGBA{0, 0, 0xff, 0xff}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{12, 50, red}, {87, 50, blue}, // the stroke outside the box, padded
	} {
		if c := dst.RGBAAt(tc.x, tc.y); c != tc.want {
			t.Errorf("(%d, %d): %v; want %v", tc.x, tc.y, c, tc.want)
		}
	}
	for _, x := range []int{30, 50, 70} {
		if fill, stroke := dst.RGBAAt(x, 50), dst.RGBAAt(x, 15); fill != stroke {
			t.Errorf("at x = %d the fill is %v, the stroke %v; want one gradient", x, fill, stroke)
		}
	}
	// A line's box has no height, nor so a place for the gradient: SVG
	// leaves it undrawn.
	if c := dst.RGBAAt(50, 95); c.A != 0 {
		t.Errorf("a line stroked by its bounding box: %v; want clear", c)
	}
}

// A userSpaceOnUse radial gradient is a circle in its own space, which the
// shape's transforms and the view turn, skew or stretch: here an ellipse,
// rotated, and a circle scaled unevenly under a rotated view. Each pixel
// is gray by its distance from the center over the radius, mapped back
// from the pixel's center by the inverse of view · transform ·
// gradientTransform, from black at the center to white at the circle.
func TestUserSpaceRadialGradientIsTransformed(t *testing.T) {
	const deg = math.Pi / 180
	for _, tc := range []struct {
		name, transform, gradientTransform string
		// grad is gradientTransform, transform and the view, in turn.
		grad, elem, view func(*ggfx.GeoM)
	}{
		{
			name:              "rotated",
			transform:         "rotate(30,50,50)",
			gradientTransform: "translate(50,50) scale(2,0.5) translate(-50,-50)",
			grad:              func(g *ggfx.GeoM) { g.Translate(-50, -50); g.Scale(2, 0.5); g.Translate(50, 50) },
			elem:              func(g *ggfx.GeoM) { g.Translate(-50, -50); g.Rotate(30 * deg); g.Translate(50, 50) },
			view:              func(g *ggfx.GeoM) { g.Scale(2, 2) },
		},
		{
			name:      "scaled",
			transform: "translate(50,50) scale(1.8,0.6) translate(-50,-50)",
			grad:      func(g *ggfx.GeoM) {},
			elem:      func(g *ggfx.GeoM) { g.Translate(-50, -50); g.Scale(1.8, 0.6); g.Translate(50, 50) },
			view:      func(g *ggfx.GeoM) { g.Translate(-50, -50); g.Rotate(0.4); g.Scale(2, 2); g.Translate(100, 100) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := parse(t, goldenDoc(`<defs><radialGradient id="g" gradientUnits="userSpaceOnUse" cx="50" cy="50" r="40" gradientTransform="`+tc.gradientTransform+`">
<stop offset="0" stop-color="#000"/><stop offset="1" stop-color="#fff"/></radialGradient></defs>
<g transform="`+tc.transform+`"><rect x="-500" y="-500" width="1100" height="1100" fill="url(#g)"/></g>`))
			var view ggfx.GeoM
			tc.view(&view)
			dst := drawInto(d, image.Rect(0, 0, 200, 200), view)
			var toGrad ggfx.GeoM
			tc.grad(&toGrad)
			tc.elem(&toGrad)
			tc.view(&toGrad)
			toGrad.Invert()
			n := 0
			for y := 5; y < 200; y += 19 {
				for x := 3; x < 200; x += 17 {
					gx, gy := toGrad.Apply(float64(x)+0.5, float64(y)+0.5)
					s := math.Hypot(gx-50, gy-50) / 40
					want := 255.0
					if s < 1 {
						want = 255 * s
					}
					if c := dst.RGBAAt(x, y); math.Abs(float64(c.R)-want) > 1 || c.R != c.G || c.R != c.B || c.A != 0xff {
						t.Errorf("(%d, %d), at %.3f of the radius: %v; want gray %.1f", x, y, s, c, want)
					}
					if s < 1 {
						n++
					}
				}
			}
			if n < 20 {
				t.Errorf("only %d samples inside the circle", n)
			}
		})
	}
}

// A command of several arcs draws each with its own radii, flags and end,
// as one command for each would: Lucide's pin is drawn so.
func TestArcsOfOneCommandAreEachTheirOwn(t *testing.T) {
	stroke := func(d string) *image.RGBA {
		doc := parse(t, svgDoc("0 0 24 24", `<path d="`+d+`" fill="none" stroke="#fff" stroke-width="2"/>`))
		var g ggfx.GeoM
		g.Scale(4, 4)
		return drawInto(doc, image.Rect(0, 0, 96, 96), g)
	}
	one := stroke("M15 7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1")
	each := stroke("M15 7a1 1 0 0 1 1-1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4a1 1 0 0 1 1 1")
	if !bytes.Equal(one.Pix, each.Pix) {
		t.Fatal("the arcs of one command are drawn otherwise than one command each")
	}
}

func TestZeroAndNilDocumentSafe(t *testing.T) {
	var zero svg.Document
	var nilDoc *svg.Document
	for _, d := range []*svg.Document{&zero, nilDoc} {
		if x, y, w, h := d.ViewBox(); x != 0 || y != 0 || w != 0 || h != 0 {
			t.Errorf("ViewBox = %v %v %v %v, want zeros", x, y, w, h)
		}
		dst := image.NewRGBA(image.Rect(0, 0, 8, 8))
		d.Draw(dst, ggfx.GeoM{})
		d.Draw(nil, ggfx.GeoM{})
		b, err := d.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary: %v", err)
		}
		var back svg.Document
		if err := back.UnmarshalBinary(b); err != nil {
			t.Fatalf("UnmarshalBinary: %v", err)
		}
	}
}

func TestDegenerateQuadArcJoinAndLargeMiter(t *testing.T) {
	// Degenerate quadratic Bezier (control point equals endpoint) with stroke-linejoin="arc"
	// must not panic with divide-by-zero in RadCurvature.
	dst := draw(t, svgDoc("0 0 20 20", `
<path d="M2,10 Q2,10 10,10 Q18,10 18,18" fill="none" stroke="#f00" stroke-width="2" stroke-linejoin="arc"/>`), 1, 20, 20)
	if dst.RGBAAt(10, 10).R == 0 {
		t.Error("degenerate quad arc join drew nothing")
	}

	// Large coordinates (> 724 px) with miter join must not overflow 32-bit determinants.
	d := parse(t, svgDoc("0 0 2000 2000", `<path d="M100,100 L1800,100 L1800,1800" fill="none" stroke="#0f0" stroke-width="40" stroke-linejoin="miter"/>`))
	var g ggfx.GeoM
	g.Translate(-1750, -50)
	big := drawInto(d, image.Rect(0, 0, 100, 100), g)
	if big.RGBAAt(50, 50).G == 0 {
		t.Error("large miter corner drew nothing")
	}
}

func TestHugeAndNegativeDashOffset(t *testing.T) {
	// Huge positive and negative dashoffset must normalize in O(len(dashes)) and wrap properly.
	dstPos := draw(t, svgDoc("0 0 40 10", `<line x1="0" y1="5" x2="40" y2="5" stroke="#fff" stroke-width="4" stroke-dasharray="10 10" stroke-dashoffset="1000000000"/>`), 1, 40, 10)
	dstZero := draw(t, svgDoc("0 0 40 10", `<line x1="0" y1="5" x2="40" y2="5" stroke="#fff" stroke-width="4" stroke-dasharray="10 10" stroke-dashoffset="0"/>`), 1, 40, 10)
	if !bytes.Equal(dstPos.Pix, dstZero.Pix) {
		t.Error("huge dashoffset multiple of period differs from offset 0")
	}

	// dashoffset=-10 on "10 10" wraps to +10 (starts with a 10px gap, then 10px dash).
	dstNeg := draw(t, svgDoc("0 0 40 10", `<line x1="0" y1="5" x2="40" y2="5" stroke="#fff" stroke-width="4" stroke-dasharray="10 10" stroke-dashoffset="-10"/>`), 1, 40, 10)
	if dstNeg.RGBAAt(5, 5).A != 0 {
		t.Errorf("negative dashoffset gap pixel alpha = %d, want 0", dstNeg.RGBAAt(5, 5).A)
	}
	if dstNeg.RGBAAt(15, 5).A != 0xff {
		t.Errorf("negative dashoffset dash pixel alpha = %d, want 255", dstNeg.RGBAAt(15, 5).A)
	}
}

func TestGradientEdgeCases(t *testing.T) {
	dst := draw(t, svgDoc("0 0 50 10", `
<defs>
<linearGradient id="empty"/>
<linearGradient id="one"><stop offset="0" stop-color="#f00" stop-opacity="0.5"/></linearGradient>
<linearGradient id="coinc" x1="0.5" y1="0.5" x2="0.5" y2="0.5"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#0f0"/></linearGradient>
<radialGradient id="zeroR" r="0"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></radialGradient>
<radialGradient id="edgeF" cx="0.5" cy="0.5" r="0.5" fx="1" fy="0.5"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#0ff"/></radialGradient>
</defs>
<rect x="0" y="0" width="10" height="10" fill="url(#empty)"/>
<rect x="10" y="0" width="10" height="10" fill="url(#one)"/>
<rect x="20" y="0" width="10" height="10" fill="url(#coinc)"/>
<rect x="30" y="0" width="10" height="10" fill="url(#zeroR)"/>
<rect x="40" y="0" width="10" height="10" fill="url(#edgeF)"/>`), 1, 50, 10)

	if got := dst.RGBAAt(5, 5); got.A != 0 {
		t.Errorf("0-stop gradient = %v, want transparent", got)
	}
	if got := dst.RGBAAt(15, 5); got.A < 120 || got.A > 135 {
		t.Errorf("1-stop gradient with stop-opacity=0.5 = %v, want ~50%% alpha", got)
	}
	if got := dst.RGBAAt(25, 5); got != (color.RGBA{0, 0xff, 0, 0xff}) {
		t.Errorf("coincident linear gradient = %v, want green", got)
	}
	if got := dst.RGBAAt(35, 5); got != (color.RGBA{0, 0, 0xff, 0xff}) {
		t.Errorf("r=0 radial gradient = %v, want blue", got)
	}
	if got := dst.RGBAAt(45, 5); got.A != 0xff {
		t.Errorf("focus-on-boundary radial gradient = %v, want opaque", got)
	}
}

func TestNestedDefsChainedClipsMarkersCSSAndSpecFixes(t *testing.T) {
	// 1. Nested <defs><g id="a"><rect id="b"/></g></defs> keeps both #a and #b intact.
	dstDefs := draw(t, svgDoc("0 0 30 10", `
<defs><g id="a"><rect id="b" width="10" height="10" fill="#f00"/><circle cx="15" cy="5" r="4" fill="#0f0"/></g></defs>
<use href="#a" x="0" y="0"/>
<use href="#b" x="20" y="0"/>`), 1, 30, 10)
	if dstDefs.RGBAAt(5, 5).R != 0xff || dstDefs.RGBAAt(15, 5).G != 0xff || dstDefs.RGBAAt(25, 5).R != 0xff {
		t.Errorf("nested defs failed: (5,5)=%v (15,5)=%v (25,5)=%v", dstDefs.RGBAAt(5, 5), dstDefs.RGBAAt(15, 5), dstDefs.RGBAAt(25, 5))
	}

	// 2. Nested clip-path intersects outer and inner clips.
	dstClip := draw(t, svgDoc("0 0 20 20", `
<defs>
<clipPath id="c1"><rect x="0" y="0" width="12" height="20"/></clipPath>
<clipPath id="c2"><rect x="8" y="0" width="12" height="20"/></clipPath>
</defs>
<g clip-path="url(#c1)"><rect width="20" height="20" fill="#f00" clip-path="url(#c2)"/></g>`), 1, 20, 20)
	if dstClip.RGBAAt(4, 10).A != 0 || dstClip.RGBAAt(10, 10).R != 0xff || dstClip.RGBAAt(16, 10).A != 0 {
		t.Errorf("chained clip failed: (4,10)=%v (10,10)=%v (16,10)=%v", dstClip.RGBAAt(4, 10), dstClip.RGBAAt(10, 10), dstClip.RGBAAt(16, 10))
	}

	// 3. Marker with non-zero viewBox min-x/min-y aligns refX/refY to the vertex and respects clip-path.
	dstMark := draw(t, svgDoc("0 0 40 20", `
<defs>
<clipPath id="mc"><rect x="0" y="0" width="20" height="20"/></clipPath>
<marker id="m" viewBox="-5 -5 10 10" refX="0" refY="0" markerWidth="10" markerHeight="10" markerUnits="userSpaceOnUse">
<circle cx="0" cy="0" r="5" fill="#00f"/>
</marker>
</defs>
<g clip-path="url(#mc)">
<path d="M10,10 L30,10" stroke="#000" stroke-width="1" marker-start="url(#m)" marker-end="url(#m)"/>
</g>`), 1, 40, 20)
	if dstMark.RGBAAt(10, 10).B != 0xff {
		t.Errorf("marker center at (10,10) = %v, want blue", dstMark.RGBAAt(10, 10))
	}
	if dstMark.RGBAAt(30, 10).A != 0 {
		t.Errorf("clipped end marker at (30,10) = %v, want transparent", dstMark.RGBAAt(30, 10))
	}

	// 4. 2-point polyline, stroke-dasharray="none", quoted url('#g'), multi-class + inline style precedence,
	// scientific notation, negative rect/circle, and degenerate arcs.
	dstSpec := draw(t, svgDoc("0 0 60 20", `
<style>.a { fill: #f00; } .b { stroke: #0f0; }</style>
<style>.c { fill: #00f; }</style>
<defs><linearGradient id="g"><stop offset="0" stop-color="#0ff"/><stop offset="1" stop-color="#0ff"/></linearGradient></defs>
<rect x="0" y="0" width="10" height="10" class="a b" style="fill:#ff0"/>
<rect x="10" y="0" width="10" height="10" class="c" fill="#f00"/>
<rect x="20" y="0" width="10" height="10" fill="url('#g')"/>
<g stroke="#fff" stroke-width="4" stroke-dasharray="1 99">
<polyline points="30,5 40,5" fill="none" stroke-dasharray="none"/>
</g>
<path d="M4E1,1E+1 A0,5 0 0 1 5e+1,1e+1 A-5,-5 0 0 1 5e+1,1e+1" fill="none" stroke="#f0f" stroke-width="4"/>
<rect x="0" y="10" width="-10" height="10" fill="#fff"/>
<circle cx="5" cy="15" r="-5" fill="#fff"/>`), 1, 60, 20)

	if got := dstSpec.RGBAAt(5, 5); got != (color.RGBA{0xff, 0xff, 0, 0xff}) {
		t.Errorf("inline style over class = %v, want yellow", got)
	}
	if got := dstSpec.RGBAAt(15, 5); got != (color.RGBA{0, 0, 0xff, 0xff}) {
		t.Errorf("class over presentation attr (2nd style block) = %v, want blue", got)
	}
	if got := dstSpec.RGBAAt(25, 5); got != (color.RGBA{0, 0xff, 0xff, 0xff}) {
		t.Errorf("quoted gradient url = %v, want cyan", got)
	}
	if got := dstSpec.RGBAAt(35, 5); got != (color.RGBA{0xff, 0xff, 0xff, 0xff}) {
		t.Errorf("2-point polyline + dasharray=none = %v, want white", got)
	}
	if got := dstSpec.RGBAAt(45, 10); got != (color.RGBA{0xff, 0, 0xff, 0xff}) {
		t.Errorf("scientific notation + zero-radius arc line = %v, want magenta", got)
	}
	if got := dstSpec.RGBAAt(5, 15); got.A != 0 {
		t.Errorf("negative rect/circle = %v, want transparent", got)
	}
}
