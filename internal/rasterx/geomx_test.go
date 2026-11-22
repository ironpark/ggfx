// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package rasterx

import (
	"math"
	"testing"
)

// A path's bounds are its curves' own extent, not their control points'.
func TestPathBounds(t *testing.T) {
	var p Path
	p.Start(ToFixedP(0, 0))
	p.CubeBezier(ToFixedP(0, 100), ToFixedP(100, 100), ToFixedP(100, 0)) // tops out at y = 75
	p.QuadBezier(ToFixedP(150, -40), ToFixedP(100, -20))                 // reaches x = 125, y = -80/3
	p.Stop(true)
	if x0, y0, x1, y1 := p.Bounds(); x0 != 0 || math.Abs(y0+80.0/3) > 1e-9 || x1 != 125 || y1 != 75 {
		t.Errorf("cubic and quadratic: bounds (%v, %v)-(%v, %v); want (0, -80/3)-(125, 75)", x0, y0, x1, y1)
	}
	var c Path
	AddCircle(50, 40, 10, &c)
	const tol = 0.05 // the circle's cubics and its points' fixed point
	if x0, y0, x1, y1 := c.Bounds(); math.Abs(x0-40) > tol || math.Abs(y0-30) > tol || math.Abs(x1-60) > tol || math.Abs(y1-50) > tol {
		t.Errorf("circle: bounds (%v, %v)-(%v, %v); want (40, 30)-(60, 50)", x0, y0, x1, y1)
	}
	if x0, y0, x1, y1 := (Path{}).Bounds(); x0 != 0 || y0 != 0 || x1 != 0 || y1 != 0 {
		t.Errorf("empty: bounds (%v, %v)-(%v, %v); want none", x0, y0, x1, y1)
	}
}

func TestGeomxDegeneratesAndToSVGPath(t *testing.T) {
	var p Path
	p.Start(ToFixedP(0, 0))
	p.CubeBezier(ToFixedP(1, 2), ToFixedP(3, 4), ToFixedP(5, 6))
	if got := p.ToSVGPath(); len(got) < 3 || got[len(got)-1] == 'C' || got == "" {
		t.Fatalf("unexpected ToSVGPath: %q", got)
	}
	if s := p.ToSVGPath(); s != "M0.000,0.000 C1.000,2.000,3.000,4.000,5.000,6.000" {
		t.Errorf("ToSVGPath = %q, want single C prefix", s)
	}

	// RadCurvature with p0 == p1 must not panic with divide-by-zero.
	if r := RadCurvature(ToFixedP(5, 5), ToFixedP(5, 5), ToFixedP(10, 10), 2<<12); r != 0 {
		t.Errorf("RadCurvature(degenerate) = %v, want 0", r)
	}

	// Concentric circles must return intersects=false without NaN.
	if _, _, ok := CircleCircleIntersection(ToFixedP(5, 5), ToFixedP(5, 5), 10<<6, 10<<6); ok {
		t.Error("CircleCircleIntersection(concentric) returned true")
	}

	// Parallel lines in CalcIntersect must not panic.
	_ = CalcIntersect(ToFixedP(0, 0), ToFixedP(10, 0), ToFixedP(0, 5), ToFixedP(10, 5))
}
