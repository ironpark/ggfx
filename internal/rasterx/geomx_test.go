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
