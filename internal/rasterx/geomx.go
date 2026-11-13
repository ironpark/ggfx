// geomx adds some geometry functions needed by rasterx
// Copyright 2017 by the rasterx Authors. All rights reserved.
// Created: 2/12/2017 by S.R.Wiley

package rasterx

import (
	"fmt"
	"math"

	"golang.org/x/image/math/fixed"
)

// Invert  returns the point inverted around the origin
func Invert(v fixed.Point26_6) fixed.Point26_6 {
	return fixed.Point26_6{X: -v.X, Y: -v.Y}
}

// turnStarboard90 returns the vector 90 degrees starboard (right in direction heading)
func turnStarboard90(v fixed.Point26_6) fixed.Point26_6 {
	return fixed.Point26_6{X: -v.Y, Y: v.X}
}

// turnPort90 returns the vector 90 degrees port (left in direction heading)
func turnPort90(v fixed.Point26_6) fixed.Point26_6 {
	return fixed.Point26_6{X: v.Y, Y: -v.X}
}

// DotProd returns the inner product of p and q
func DotProd(p fixed.Point26_6, q fixed.Point26_6) fixed.Int52_12 {
	return fixed.Int52_12(int64(p.X)*int64(q.X) + int64(p.Y)*int64(q.Y))
}

// Length is the distance from the origin of the point
func Length(v fixed.Point26_6) fixed.Int26_6 {
	vx, vy := float64(v.X), float64(v.Y)
	return fixed.Int26_6(math.Sqrt(float64(vx*vx) + float64(vy*vy)))
}

// PathCommand is the type for the path command token
type PathCommand fixed.Int26_6

// Human readable path constants
const (
	PathMoveTo PathCommand = iota
	PathLineTo
	PathQuadTo
	PathCubicTo
	PathClose
)

// A Path starts with a PathCommand value followed by zero to three fixed
// int points.
type Path []fixed.Int26_6

// ToSVGPath returns a string representation of the path
func (p Path) ToSVGPath() string {
	s := ""
	for i := 0; i < len(p); {
		if i != 0 {
			s += " "
		}
		switch PathCommand(p[i]) {
		case PathMoveTo:
			s += fmt.Sprintf("M%4.3f,%4.3f", float32(p[i+1])/64, float32(p[i+2])/64)
			i += 3
		case PathLineTo:
			s += fmt.Sprintf("L%4.3f,%4.3f", float32(p[i+1])/64, float32(p[i+2])/64)
			i += 3
		case PathQuadTo:
			s += fmt.Sprintf("Q%4.3f,%4.3f,%4.3f,%4.3f", float32(p[i+1])/64, float32(p[i+2])/64,
				float32(p[i+3])/64, float32(p[i+4])/64)
			i += 5
		case PathCubicTo:
			s += "C" + fmt.Sprintf("C%4.3f,%4.3f,%4.3f,%4.3f,%4.3f,%4.3f", float32(p[i+1])/64, float32(p[i+2])/64,
				float32(p[i+3])/64, float32(p[i+4])/64, float32(p[i+5])/64, float32(p[i+6])/64)
			i += 7
		case PathClose:
			s += "Z"
			i++
		default:
			panic("freetype/rasterx: bad pather")
		}
	}
	return s
}

// String returns a readable representation of a Path.
func (p Path) String() string {
	return p.ToSVGPath()
}

// Clear zeros the path slice
func (p *Path) Clear() {
	*p = (*p)[:0]
}

// Start starts a new curve at the given point.
func (p *Path) Start(a fixed.Point26_6) {
	*p = append(*p, fixed.Int26_6(PathMoveTo), a.X, a.Y)
}

// Line adds a linear segment to the current curve.
func (p *Path) Line(b fixed.Point26_6) {
	*p = append(*p, fixed.Int26_6(PathLineTo), b.X, b.Y)
}

// QuadBezier adds a quadratic segment to the current curve.
func (p *Path) QuadBezier(b, c fixed.Point26_6) {
	*p = append(*p, fixed.Int26_6(PathQuadTo), b.X, b.Y, c.X, c.Y)
}

// CubeBezier adds a cubic segment to the current curve.
func (p *Path) CubeBezier(b, c, d fixed.Point26_6) {
	*p = append(*p, fixed.Int26_6(PathCubicTo), b.X, b.Y, c.X, c.Y, d.X, d.Y)
}

// Stop joins the ends of the path
func (p *Path) Stop(closeLoop bool) {
	if closeLoop {
		*p = append(*p, fixed.Int26_6(PathClose))
	}
}

// AddTo adds the Path p to q.
func (p Path) AddTo(q Adder) {
	for i := 0; i < len(p); {
		switch PathCommand(p[i]) {
		case PathMoveTo:
			q.Stop(false) // Fixes issues #1 by described by Djadala; implicit close if currently in path.
			q.Start(fixed.Point26_6{X: p[i+1], Y: p[i+2]})
			i += 3
		case PathLineTo:
			q.Line(fixed.Point26_6{X: p[i+1], Y: p[i+2]})
			i += 3
		case PathQuadTo:
			q.QuadBezier(fixed.Point26_6{X: p[i+1], Y: p[i+2]}, fixed.Point26_6{X: p[i+3], Y: p[i+4]})
			i += 5
		case PathCubicTo:
			q.CubeBezier(fixed.Point26_6{X: p[i+1], Y: p[i+2]},
				fixed.Point26_6{X: p[i+3], Y: p[i+4]}, fixed.Point26_6{X: p[i+5], Y: p[i+6]})
			i += 7
		case PathClose:
			q.Stop(true)
			i++
		default:
			panic("AddTo: bad path")
		}
	}
	q.Stop(false)
}

// Bounds is the extent of the path's geometry, in the path's units, not
// fixed point: its curves' own, not their control points'. It is empty
// for an empty path.
func (p Path) Bounds() (minX, minY, maxX, maxY float64) {
	minX, minY, maxX, maxY = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	add := func(x, y float64) {
		minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
	}
	var x0, y0 float64 // the pen
	pt := func(i int) (float64, float64) { return float64(float64(p[i]) / 64), float64(float64(p[i+1]) / 64) }
	for i := 0; i < len(p); {
		switch PathCommand(p[i]) {
		case PathMoveTo, PathLineTo:
			x0, y0 = pt(i + 1)
			add(x0, y0)
			i += 3
		case PathQuadTo:
			x1, y1 := pt(i + 1)
			x2, y2 := pt(i + 3)
			for _, t := range quadExtrema(x0, x1, x2, y0, y1, y2) {
				if t > 0 && t < 1 {
					add(quadAt(x0, x1, x2, t), quadAt(y0, y1, y2, t))
				}
			}
			x0, y0 = x2, y2
			add(x0, y0)
			i += 5
		case PathCubicTo:
			x1, y1 := pt(i + 1)
			x2, y2 := pt(i + 3)
			x3, y3 := pt(i + 5)
			for _, t := range cubicExtrema(x0, x1, x2, x3, y0, y1, y2, y3) {
				if t > 0 && t < 1 {
					add(cubicAt(x0, x1, x2, x3, t), cubicAt(y0, y1, y2, y3, t))
				}
			}
			x0, y0 = x3, y3
			add(x0, y0)
			i += 7
		case PathClose:
			i++
		default:
			panic("Bounds: bad path")
		}
	}
	if minX > maxX {
		return 0, 0, 0, 0
	}
	return minX, minY, maxX, maxY
}

// quadExtrema is where the quadratic curve from a over b to c, in x and
// in y, turns: where its derivative, 2((b-a)(1-t) + (c-b)t), is 0.
func quadExtrema(ax, bx, cx, ay, by, cy float64) [2]float64 {
	at := func(a, b, c float64) float64 {
		if d := a - 2*b + c; d != 0 {
			return (a - b) / d
		}
		return -1
	}
	return [2]float64{at(ax, bx, cx), at(ay, by, cy)}
}

func quadAt(a, b, c, t float64) float64 {
	mt := 1 - t
	return float64(float64(mt*mt)*a) + float64(float64(2*mt*t)*b) + float64(float64(t*t)*c)
}

// cubicExtrema is where the cubic curve from a over b and c to d, in x
// and in y, turns: where its derivative over 3, A t² + 2B t + C with
// A = -a+3b-3c+d, B = a-2b+c and C = b-a, is 0.
func cubicExtrema(ax, bx, cx, dx, ay, by, cy, dy float64) [4]float64 {
	ts := [4]float64{-1, -1, -1, -1}
	roots := func(t []float64, a, b, c, d float64) {
		A, B, C := -a+float64(3*b)-float64(3*c)+d, a-2*b+c, b-a
		if A == 0 {
			if B != 0 {
				t[0] = -C / (2 * B)
			}
			return
		}
		D := float64(B*B) - float64(A*C)
		if D < 0 {
			return
		}
		D = math.Sqrt(D)
		t[0], t[1] = (-B+D)/A, (-B-D)/A
	}
	roots(ts[0:2], ax, bx, cx, dx)
	roots(ts[2:4], ay, by, cy, dy)
	return ts
}

func cubicAt(a, b, c, d, t float64) float64 {
	mt := 1 - t
	return float64(float64(mt*mt*mt)*a) + float64(float64(3*mt*mt*t)*b) + float64(float64(3*mt*t*t)*c) + float64(float64(t*t*t)*d)
}

// ToLength scales the point to the length indicated by ln
func ToLength(p fixed.Point26_6, ln fixed.Int26_6) (q fixed.Point26_6) {
	if ln == 0 || (p.X == 0 && p.Y == 0) {
		return
	}

	pX, pY := float64(p.X), float64(p.Y)
	lnF := float64(ln)
	pLen := math.Sqrt(float64(pX*pX) + float64(pY*pY))

	qX, qY := pX*lnF/pLen, pY*lnF/pLen
	q.X, q.Y = fixed.Int26_6(qX), fixed.Int26_6(qY)
	return
}

// ClosestPortside returns the closest of p1 or p2 on the port side of the
// line from the bow to the stern. (port means left side of the direction you are heading)
// isIntersecting is just convienice to reduce code, and if false returns false, because p1 and p2 are not valid
func ClosestPortside(bow, stern, p1, p2 fixed.Point26_6, isIntersecting bool) (xt fixed.Point26_6, intersects bool) {
	if isIntersecting == false {
		return
	}
	dir := bow.Sub(stern)
	dp1 := p1.Sub(stern)
	dp2 := p2.Sub(stern)
	cp1 := dir.X*dp1.Y - dp1.X*dir.Y
	cp2 := dir.X*dp2.Y - dp2.X*dir.Y
	switch {
	case cp1 < 0 && cp2 < 0:
		return
	case cp1 < 0 && cp2 >= 0:
		return p2, true
	case cp1 >= 0 && cp2 < 0:
		return p1, true
	default: // both points on port side
		dirdot := DotProd(dir, dir)
		// calculate vector rejections of dp1 and dp2 onto dir
		h1 := dp1.Sub(dir.Mul(fixed.Int26_6((DotProd(dp1, dir) << 6) / dirdot)))
		h2 := dp2.Sub(dir.Mul(fixed.Int26_6((DotProd(dp2, dir) << 6) / dirdot)))
		// return point with smallest vector rejection; i.e. closest to dir line
		if (h1.X*h1.X + h1.Y*h1.Y) > (h2.X*h2.X + h2.Y*h2.Y) {
			return p2, true
		}
		return p1, true
	}
}

// RadCurvature returns the curvature of a Bezier curve end point,
// given an end point, the two adjacent control points and the degree.
// The sign of the value indicates if the center of the osculating circle
// is left or right (port or starboard) of the curve in the forward direction.
func RadCurvature(p0, p1, p2 fixed.Point26_6, dm fixed.Int52_12) fixed.Int26_6 {
	a, b := p2.Sub(p1), p1.Sub(p0)
	abdot, bbdot := DotProd(a, b), DotProd(b, b)
	h := a.Sub(b.Mul(fixed.Int26_6((abdot << 6) / bbdot))) // h is the vector rejection of a onto b
	if h.X == 0 && h.Y == 0 {                              // points are co-linear
		return 0
	}
	radCurve := fixed.Int26_6((fixed.Int52_12(a.X*a.X+a.Y*a.Y) * dm / fixed.Int52_12(Length(h)<<6)) >> 6)
	if a.X*b.Y > b.X*a.Y { // xprod sign
		return radCurve
	}
	return -radCurve
}

// CircleCircleIntersection calculates the points of intersection of
// two circles or returns with intersects == false if no such points exist.
func CircleCircleIntersection(ct, cl fixed.Point26_6, rt, rl fixed.Int26_6) (xt1, xt2 fixed.Point26_6, intersects bool) {
	dc := cl.Sub(ct)
	d := Length(dc)

	// Check for solvability.
	if d > (rt + rl) {
		return // No solution. Circles do not intersect.
	}
	// check if  d < abs(rt-rl)
	if da := rt - rl; (da > 0 && d < da) || (da < 0 && d < -da) {
		return // No solution. One circle is contained by the other.
	}

	rlf, rtf, df := float64(rl), float64(rt), float64(d)
	af := float64((float64(rtf*rtf)-float64(rlf*rlf)+float64(df*df))/df) / 2.0
	hfd := math.Sqrt(float64(rtf*rtf)-float64(af*af)) / df
	afd := af / df

	rOffx, rOffy := float64(float64(-dc.Y)*hfd), float64(float64(dc.X)*hfd)
	p2x := float64(ct.X) + float64(float64(dc.X)*afd)
	p2y := float64(ct.Y) + float64(float64(dc.Y)*afd)
	xt1x, xt1y := p2x+rOffx, p2y+rOffy
	xt2x, xt2y := p2x-rOffx, p2y-rOffy
	return fixed.Point26_6{X: fixed.Int26_6(xt1x), Y: fixed.Int26_6(xt1y)},
		fixed.Point26_6{X: fixed.Int26_6(xt2x), Y: fixed.Int26_6(xt2y)}, true
}

// CalcIntersect calculates the points of intersection of two fixed point lines
// and panics if the determinate is zero. You have been warned.
func CalcIntersect(a1, a2, b1, b2 fixed.Point26_6) (x fixed.Point26_6) {
	da, db, ds := a2.Sub(a1), b2.Sub(b1), a1.Sub(b1)
	det := float32(da.X*db.Y - db.X*da.Y) // Determinate
	t := float32(ds.Y*db.X-ds.X*db.Y) / det
	x = a1.Add(fixed.Point26_6{X: fixed.Int26_6(float32(da.X) * t), Y: fixed.Int26_6(float32(da.Y) * t)})
	return
}

// RayCircleIntersection calculates the points of intersection of
// a ray starting at s2 passing through s1 and a circle in fixed point.
// Returns intersects == false if no solution is possible. If two
// solutions are possible, the point closest to s2 is returned
func RayCircleIntersection(s1, s2, c fixed.Point26_6, r fixed.Int26_6) (x fixed.Point26_6, intersects bool) {
	fx, fy, intersects := RayCircleIntersectionF(float64(s1.X), float64(s1.Y),
		float64(s2.X), float64(s2.Y), float64(c.X), float64(c.Y), float64(r))
	return fixed.Point26_6{X: fixed.Int26_6(fx),
		Y: fixed.Int26_6(fy)}, intersects

}

// RayCircleIntersectionF calculates in floating point the points of intersection of
// a ray starting at s2 passing through s1 and a circle in fixed point.
// Returns intersects == false if no solution is possible. If two
// solutions are possible, the point closest to s2 is returned
func RayCircleIntersectionF(s1X, s1Y, s2X, s2Y, cX, cY, r float64) (x, y float64, intersects bool) {
	n := s2X - cX // Calculating using 64* rather than divide
	m := s2Y - cY

	e := s2X - s1X
	d := s2Y - s1Y

	// Quadratic normal form coefficients
	A, B, C := float64(e*e)+float64(d*d), float64(-2*(float64(e*n)+float64(m*d))), float64(n*n)+float64(m*m)-float64(r*r)

	D := float64(B*B) - float64(4*A*C)

	if D <= 0 {
		return // No intersection or is tangent
	}

	D = math.Sqrt(D)
	t1, t2 := (-B+D)/(2*A), (-B-D)/(2*A)
	p1OnSide := t1 > 0
	p2OnSide := t2 > 0

	switch {
	case p1OnSide && p2OnSide:
		if t2 < t1 { // both on ray, use closest to s2
			t1 = t2
		}
	case p2OnSide: // Only p2 on ray
		t1 = t2
	case p1OnSide: // only p1 on ray
	default: // Neither solution is on the ray
		return
	}
	return (n - float64(e*t1)) + cX, (m - float64(d*t1)) + cY, true
}
