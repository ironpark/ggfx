// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package rasterx

import (
	"fmt"
	"image"
	"math"
	"testing"

	"golang.org/x/image/math/fixed"
)

// maxCoverageError is how far the scanner's mask, as a fraction of full
// coverage, may be from a polygon's exact area in each pixel: 1e-4, about
// six steps of the 16-bit mask and a fortieth of one of an 8-bit pixel.
// It is the rounding of float32 arithmetic over coordinates up to about
// a thousand pixels; the worst seen here is under 5e-5. (Fixed point,
// which vector chose up to 512 pixels a side, stepped x down a line row
// by row, rounding each step the same way: along these edges it drifted
// by up to 0.89 of full coverage.)
const maxCoverageError = 1e-4

// The scanner's mask is each pixel's exact coverage by a polygon, within
// maxCoverageError, by either rule and at any size: along long steep
// edges, where stepping x row by row in fixed point drifted, and across
// slivers thinner than a pixel.
func TestCoverageIsExact(t *testing.T) {
	type poly [][2]float64
	// Each is drawn at both scales, in a target that fits it: under 512
	// pixels a side and over.
	shapes := []struct {
		name string
		p    poly
	}{
		{"rect", poly{{10.25, 20.375}, {300.6875, 20.375}, {300.6875, 410.8125}, {10.25, 410.8125}}},
		// A right triangle 480 rows tall and 3.5 pixels wide: its
		// hypotenuse crosses a pixel column every 137 rows.
		{"steepTriangle", poly{{100.25, 10.5}, {103.765625, 490.75}, {100.25, 490.75}}},
		// And leaning the other way, from the right.
		{"steepTriangleLeft", poly{{400.75, 6.25}, {400.75, 497.5}, {396.015625, 497.5}}},
		// A shallow one, 400 pixels long and 2 tall.
		{"shallowTriangle", poly{{20.5, 200.25}, {420.75, 200.25}, {20.5, 202.3125}}},
		// Slivers a fraction of a pixel wide, steep and diagonal.
		{"steepSliver", poly{{50.5, 5}, {50.8125, 5}, {70.8125, 495.5}, {70.5, 495.5}}},
		{"diagonalSliver", poly{{10.125, 10.25}, {10.375, 10.25}, {480.375, 470.75}, {480.125, 470.75}}},
		{"hairSliver", poly{{200.015625, 3}, {200.046875, 3}, {260.046875, 499.984375}, {260.015625, 499.984375}}},
	}
	for _, scale := range []float64{1, 2} {
		size := image.Pt(int(510*scale), int(505*scale))
		for _, sh := range shapes {
			p := make(poly, len(sh.p))
			for i, v := range sh.p {
				p[i] = [2]float64{v[0] * scale, v[1] * scale}
			}
			for _, evenOdd := range []bool{false, true} {
				name := fmt.Sprintf("%v/%s/evenOdd=%v", size, sh.name, evenOdd)
				worst, at, got, want := coverageError(size, p, evenOdd)
				t.Logf("%s: off by at most %.6f (%.2f in 255 where exact is %.2f, at %v)", name, worst, float64(got)/0x101, want*255, at)
				if worst > maxCoverageError {
					t.Errorf("%s: coverage off by %.6f at %v: mask %#x, exact %.6f; want within %.6f", name, worst, at, got, want, maxCoverageError)
				}
			}
		}
	}
}

// coverageError draws polygon p into a target of size and returns the
// greatest difference of its mask from p's exact coverage, and where.
func coverageError(size image.Point, p [][2]float64, evenOdd bool) (worst float64, at image.Point, got uint32, want float64) {
	s := NewScannerGV(size.X, size.Y, nil, image.Rectangle{Max: size})
	s.Clear()
	s.SetWinding(!evenOdd)
	for i, v := range p {
		fp := fixed.Point26_6{X: fixed.Int26_6(v[0] * 64), Y: fixed.Int26_6(v[1] * 64)}
		if float64(fp.X)/64 != v[0] || float64(fp.Y)/64 != v[1] {
			panic("a point between 64ths of a pixel")
		}
		if i == 0 {
			s.Start(fp)
		} else {
			s.Line(fp)
		}
	}
	s.Line(fixed.Point26_6{X: fixed.Int26_6(p[0][0] * 64), Y: fixed.Int26_6(p[0][1] * 64)})
	win := s.window().Intersect(image.Rectangle{Max: size})
	s.rasterize(win)
	for y := win.Min.Y; y < win.Max.Y; y++ {
		for i, a := range s.z.rowAlphas(y) {
			x := win.Min.X + i
			exact := pixelCoverage(p, float64(x), float64(y))
			if d := math.Abs(float64(a)/0xffff - exact); d > worst {
				worst, at, got, want = d, image.Pt(x, y), a, exact
			}
		}
	}
	return worst, at, got, want
}

// pixelCoverage is the area of polygon p within the pixel at x, y: p
// clipped to it, Sutherland and Hodgman's way, by its four sides.
func pixelCoverage(p [][2]float64, x, y float64) float64 {
	clip := func(p [][2]float64, inside func([2]float64) bool, cross func(a, b [2]float64) [2]float64) [][2]float64 {
		var out [][2]float64
		for i, b := range p {
			a := p[(i+len(p)-1)%len(p)]
			switch ina, inb := inside(a), inside(b); {
			case ina && inb:
				out = append(out, b)
			case ina:
				out = append(out, cross(a, b))
			case inb:
				out = append(out, cross(a, b), b)
			}
		}
		return out
	}
	atX := func(x float64) func(a, b [2]float64) [2]float64 {
		return func(a, b [2]float64) [2]float64 {
			return [2]float64{x, a[1] + (b[1]-a[1])*(x-a[0])/(b[0]-a[0])}
		}
	}
	atY := func(y float64) func(a, b [2]float64) [2]float64 {
		return func(a, b [2]float64) [2]float64 {
			return [2]float64{a[0] + (b[0]-a[0])*(y-a[1])/(b[1]-a[1]), y}
		}
	}
	p = clip(p, func(v [2]float64) bool { return v[0] >= x }, atX(x))
	p = clip(p, func(v [2]float64) bool { return v[0] <= x+1 }, atX(x+1))
	p = clip(p, func(v [2]float64) bool { return v[1] >= y }, atY(y))
	p = clip(p, func(v [2]float64) bool { return v[1] <= y+1 }, atY(y+1))
	area := 0.0
	for i, b := range p {
		a := p[(i+len(p)-1)%len(p)]
		area += a[0]*b[1] - b[0]*a[1]
	}
	return math.Abs(area) / 2
}
