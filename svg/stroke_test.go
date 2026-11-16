// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/svg"
)

// drawn is doc drawn through geoM into w by h pixels.
func drawn(t *testing.T, doc string, w, h int, geoM func(*ggfx.GeoM)) *image.RGBA {
	t.Helper()
	d, err := svg.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	var g ggfx.GeoM
	geoM(&g)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	d.Draw(img, g)
	return img
}

// across is how many pixels of img's row y are covered, or of its column x
// when y is negative: the width of whatever crosses it, partly covered
// pixels counting for their part.
func across(img *image.RGBA, x, y int) float64 {
	sum := 0
	if y >= 0 {
		for x := range img.Rect.Dx() {
			sum += int(img.RGBAAt(x, y).A)
		}
	} else {
		for y := range img.Rect.Dy() {
			sum += int(img.RGBAAt(x, y).A)
		}
	}
	return float64(sum) / 255
}

const cross = `<svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg" fill="none" stroke="#000" stroke-width="10">
<path d="M50 10V90"/><path d="M10 50H90"/></svg>`

// A stroke's width is in user units: a picture drawn twice as large has
// lines twice as wide, as a 16 pixel icon has thinner ones than a 32 pixel
// one.
func TestStrokeWidthFollowsTheScale(t *testing.T) {
	for _, k := range []float64{0.16, 0.24, 0.5, 1, 2, 4.5} {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			n := int(math.Ceil(100 * k))
			img := drawn(t, cross, n, n, func(g *ggfx.GeoM) { g.Scale(k, k) })
			// A row and a column a quarter of the way in cross one line each.
			q := n / 4
			if got := across(img, 0, q); math.Abs(got-10*k) > 0.02+0.002*k {
				t.Errorf("the vertical line is %.3f pixels wide, want %.3f", got, 10*k)
			}
			if got := across(img, q, -1); math.Abs(got-10*k) > 0.02+0.002*k {
				t.Errorf("the horizontal line is %.3f pixels wide, want %.3f", got, 10*k)
			}
		})
	}
}

// Stretched along one axis, a stroke is stretched with the picture: wider
// where it runs across that axis, and no wider where it runs along it.
func TestStrokeIsStretchedWithThePicture(t *testing.T) {
	img := drawn(t, cross, 400, 100, func(g *ggfx.GeoM) { g.Scale(4, 1) })
	if got := across(img, 0, 25); math.Abs(got-40) > 0.05 {
		t.Errorf("the vertical line is %.3f pixels wide, want 40", got)
	}
	if got := across(img, 100, -1); math.Abs(got-10) > 0.05 {
		t.Errorf("the horizontal line is %.3f pixels wide, want 10", got)
	}
}

// Turned and mirrored, a stroke keeps the width its scale gives it.
func TestStrokeWidthSurvivesTurningAndMirroring(t *testing.T) {
	views := map[string]func(*ggfx.GeoM){
		"quarter turn": func(g *ggfx.GeoM) { g.Scale(2, 2); g.Rotate(math.Pi / 2); g.Translate(200, 0) },
		"mirrored":     func(g *ggfx.GeoM) { g.Scale(-2, 2); g.Translate(200, 0) },
		"both":         func(g *ggfx.GeoM) { g.Scale(2, -2); g.Rotate(math.Pi / 2); g.Translate(0, 0) },
	}
	for name, view := range views {
		t.Run(name, func(t *testing.T) {
			img := drawn(t, cross, 200, 200, view)
			if got := across(img, 0, 50); math.Abs(got-20) > 0.05 {
				t.Errorf("the line down is %.3f pixels wide, want 20", got)
			}
			if got := across(img, 50, -1); math.Abs(got-20) > 0.05 {
				t.Errorf("the line across is %.3f pixels wide, want 20", got)
			}
		})
	}
}

// An element's own transform scales its stroke as the view's does.
func TestStrokeWidthFollowsAnElementsTransform(t *testing.T) {
	const doc = `<svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg" fill="none" stroke="#000" stroke-width="5">
<g transform="scale(2 1)"><path d="M25 10V90"/><path d="M5 50H45"/></g></svg>`
	img := drawn(t, doc, 100, 100, func(g *ggfx.GeoM) {})
	if got := across(img, 0, 25); math.Abs(got-10) > 0.05 {
		t.Errorf("the vertical line is %.3f pixels wide, want 10", got)
	}
	if got := across(img, 25, -1); math.Abs(got-5) > 0.05 {
		t.Errorf("the horizontal line is %.3f pixels wide, want 5", got)
	}
}

// Dashes are lengths along the path in user units, and scale with it.
func TestDashesFollowTheScale(t *testing.T) {
	const doc = `<svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg" fill="none" stroke="#000" stroke-width="10" stroke-dasharray="10 10">
<path d="M0 50H100"/></svg>`
	for name, view := range map[string]func(*ggfx.GeoM){
		"alike":     func(g *ggfx.GeoM) { g.Scale(3, 3) },
		"stretched": func(g *ggfx.GeoM) { g.Scale(3, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			img := drawn(t, doc, 300, 300, view)
			var g ggfx.GeoM
			view(&g)
			_, mid := g.Apply(0, 50)
			y := int(mid)
			// Five dashes of 30 pixels, each starting 60 after the last.
			if got := across(img, 0, y); math.Abs(got-150) > 0.2 {
				t.Fatalf("%.3f pixels of the line are drawn, want 150", got)
			}
			for x := range 300 {
				want := uint8(0)
				if x%60 < 30 {
					want = 255
				}
				if got := img.RGBAAt(x, y).A; got != want {
					t.Fatalf("pixel %d of the line has alpha %d, want %d", x, got, want)
				}
			}
		})
	}
}

// A circle stretched into an ellipse has the stroke of one: as wide as the
// stretch at its sides, and as wide as it was at its top.
func TestStrokeOfAStretchedCircle(t *testing.T) {
	const doc = `<svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg" fill="none" stroke="#000" stroke-width="6">
<circle cx="50" cy="50" r="30"/></svg>`
	img := drawn(t, doc, 300, 100, func(g *ggfx.GeoM) { g.Scale(3, 1) })
	// The row through the middle crosses both sides, the column the top
	// and the bottom.
	if got := across(img, 0, 50); math.Abs(got-36) > 0.5 {
		t.Errorf("the sides are %.3f pixels wide together, want 36", got)
	}
	if got := across(img, 150, -1); math.Abs(got-12) > 0.2 {
		t.Errorf("the top and bottom are %.3f pixels wide together, want 12", got)
	}
}
