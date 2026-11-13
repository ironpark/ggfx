// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"bytes"
	"image"
	"image/color"
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
