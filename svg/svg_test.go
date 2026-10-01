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
