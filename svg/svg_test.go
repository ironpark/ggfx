// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"image"
	"strings"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/svg"
)

func TestDrawMapsTheViewBoxThroughTheGeoM(t *testing.T) {
	d, err := svg.Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="10 10 10 10"><rect x="10" y="10" width="5" height="10" fill="#fff"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if x, y, w, h := d.ViewBox(); x != 10 || y != 10 || w != 10 || h != 10 {
		t.Fatalf("viewBox %v %v %v %v; want 10 10 10 10", x, y, w, h)
	}
	dst := image.NewRGBA(image.Rect(0, 0, 20, 20))
	var g ggfx.GeoM
	g.Translate(-10, -10)
	g.Scale(2, 2)
	d.Draw(dst, g)
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
	src := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 1 1"><g id="a"><use xlink:href="#a"/></g></svg>`
	if d, err := svg.Parse(strings.NewReader(src)); err == nil {
		d.Draw(image.NewRGBA(image.Rect(0, 0, 4, 4)), ggfx.GeoM{})
	}
}

// draw draws src at scale into a w by h image.
func draw(t *testing.T, src string, scale float64, w, h int) *image.RGBA {
	t.Helper()
	d, err := svg.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	var g ggfx.GeoM
	g.Scale(scale, scale)
	d.Draw(dst, g)
	return dst
}

// A rect given one radius rounds its corners by it both ways.
func TestARectWithOneRadiusIsRounded(t *testing.T) {
	dst := draw(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 20"><rect width="40" height="20" rx="8" fill="#fff"/></svg>`, 2, 80, 40)
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
	dst := draw(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 20">
<defs><clipPath id="c"><rect width="10" height="10"/></clipPath>
<mask id="m"><rect width="20" height="20" fill="#fff"/><rect x="20" width="20" height="20" fill="#000"/></mask></defs>
<g clip-path="url(#c)" transform="translate(5,5)"><rect width="40" height="20" fill="#f00"/></g>
<rect y="16" width="40" height="4" fill="#0f0" mask="url(#m)"/></svg>`, 2, 80, 40)
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
	dst := draw(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
<defs><marker id="dot" viewBox="0 0 10 10" refX="5" refY="5" markerWidth="4" markerHeight="4"><rect width="10" height="10" fill="#00f"/></marker>
<marker id="arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="5" markerHeight="5" orient="auto"><path d="M0,0 L10,5 L0,10 z" fill="#f00"/></marker></defs>
<path d="M10,50 L90,50" stroke="#000" stroke-width="2" marker-start="url(#dot)" marker-end="url(#arrow)"/></svg>`, 1, 100, 100)
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
