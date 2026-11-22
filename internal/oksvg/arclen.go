// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"math"

	"github.com/ironpark/ggfx/internal/rasterx"
)

type arcSegKind uint8

const (
	arcLine arcSegKind = iota
	arcQuad
	arcCubic
)

type arcSeg struct {
	kind arcSegKind
	pts  [8]float64 // line: x0,y0,x1,y1; quad: x0,y0,x1,y1,x2,y2; cubic: x0,y0,x1,y1,x2,y2,x3,y3
	len  float64
	cum  float64    // cumulative arc length at the end of this segment
	lut  [9]float64 // cumulative arc length within this segment at t = 0/8..8/8
}

type arcSubpath struct {
	start, end float64
	startPt    [2]float64
	closed     bool
	hasLine    bool
	segs       []arcSeg
}

type lengthTable struct {
	subpaths []arcSubpath
	total    float64
}

// 5-point Gauss-Legendre quadrature nodes and weights on [-1, 1].
var (
	gl5X = [5]float64{
		-0.9061798459386640,
		-0.5384693101056831,
		0.0,
		0.5384693101056831,
		0.9061798459386640,
	}
	gl5W = [5]float64{
		0.23692688505618908,
		0.47862867049936647,
		0.5688888888888889,
		0.47862867049936647,
		0.23692688505618908,
	}
)

func buildLengthTable(p rasterx.Path) *lengthTable {
	lt := &lengthTable{}
	if len(p) == 0 {
		return lt
	}
	pt := func(i int) (float64, float64) {
		return float64(p[i]) / 64, float64(p[i+1]) / 64
	}
	var (
		cur      *arcSubpath
		cx, cy   float64
		cumTotal float64
	)
	flush := func() {
		if cur != nil {
			cur.end = cumTotal
			cur = nil
		}
	}
	addSeg := func(s arcSeg) {
		if cur == nil {
			lt.subpaths = append(lt.subpaths, arcSubpath{
				start:   cumTotal,
				startPt: [2]float64{cx, cy},
			})
			cur = &lt.subpaths[len(lt.subpaths)-1]
		}
		cur.hasLine = true
		switch s.kind {
		case arcLine:
			s.len = math.Hypot(s.pts[2]-s.pts[0], s.pts[3]-s.pts[1])
		case arcQuad, arcCubic:
			s.lut[0] = 0
			var acc float64
			for k := 1; k <= 8; k++ {
				t0 := float64(k-1) * 0.125
				t1 := float64(k) * 0.125
				acc += adaptiveSegLen(&s, t0, t1, 1e-4/8, 0)
				s.lut[k] = acc
			}
			s.len = acc
		}
		cumTotal += s.len
		s.cum = cumTotal
		cur.segs = append(cur.segs, s)
	}

	for i := 0; i < len(p); {
		switch rasterx.PathCommand(p[i]) {
		case rasterx.PathMoveTo:
			flush()
			cx, cy = pt(i + 1)
			lt.subpaths = append(lt.subpaths, arcSubpath{
				start:   cumTotal,
				startPt: [2]float64{cx, cy},
			})
			cur = &lt.subpaths[len(lt.subpaths)-1]
			i += 3
		case rasterx.PathLineTo:
			x1, y1 := pt(i + 1)
			addSeg(arcSeg{kind: arcLine, pts: [8]float64{cx, cy, x1, y1}})
			cx, cy = x1, y1
			i += 3
		case rasterx.PathQuadTo:
			x1, y1 := pt(i + 1)
			x2, y2 := pt(i + 3)
			addSeg(arcSeg{kind: arcQuad, pts: [8]float64{cx, cy, x1, y1, x2, y2}})
			cx, cy = x2, y2
			i += 5
		case rasterx.PathCubicTo:
			x1, y1 := pt(i + 1)
			x2, y2 := pt(i + 3)
			x3, y3 := pt(i + 5)
			addSeg(arcSeg{kind: arcCubic, pts: [8]float64{cx, cy, x1, y1, x2, y2, x3, y3}})
			cx, cy = x3, y3
			i += 7
		case rasterx.PathClose:
			if cur != nil {
				if cx != cur.startPt[0] || cy != cur.startPt[1] {
					addSeg(arcSeg{kind: arcLine, pts: [8]float64{cx, cy, cur.startPt[0], cur.startPt[1]}})
					cx, cy = cur.startPt[0], cur.startPt[1]
				}
				cur.closed = true
				flush()
			}
			i++
		default:
			flush()
			lt.total = cumTotal
			return lt
		}
	}
	flush()
	lt.total = cumTotal
	return lt
}

func (s *arcSeg) speed(t float64) float64 {
	mt := 1 - t
	if s.kind == arcQuad {
		dx := 2 * (mt*(s.pts[2]-s.pts[0]) + t*(s.pts[4]-s.pts[2]))
		dy := 2 * (mt*(s.pts[3]-s.pts[1]) + t*(s.pts[5]-s.pts[3]))
		return math.Hypot(dx, dy)
	}
	// Cubic
	dx := 3 * (mt*mt*(s.pts[2]-s.pts[0]) + 2*mt*t*(s.pts[4]-s.pts[2]) + t*t*(s.pts[6]-s.pts[4]))
	dy := 3 * (mt*mt*(s.pts[3]-s.pts[1]) + 2*mt*t*(s.pts[5]-s.pts[3]) + t*t*(s.pts[7]-s.pts[5]))
	return math.Hypot(dx, dy)
}

func gl5SegLen(s *arcSeg, a, b float64) float64 {
	half := 0.5 * (b - a)
	mid := 0.5 * (a + b)
	var sum float64
	for i := range 5 {
		sum += gl5W[i] * s.speed(mid+half*gl5X[i])
	}
	return sum * half
}

func adaptiveSegLen(s *arcSeg, a, b, tol float64, depth int) float64 {
	whole := gl5SegLen(s, a, b)
	mid := 0.5 * (a + b)
	left := gl5SegLen(s, a, mid)
	right := gl5SegLen(s, mid, b)
	lr := left + right
	if depth >= 12 || math.Abs(whole-lr) < tol {
		return lr
	}
	return adaptiveSegLen(s, a, mid, tol*0.5, depth+1) + adaptiveSegLen(s, mid, b, tol*0.5, depth+1)
}

// paramAtLength inverts local arc length d in [0, s.len] to curve parameter t in [0, 1].
func (s *arcSeg) paramAtLength(d float64) float64 {
	if s.len <= 0 || d <= 0 {
		return 0
	}
	if d >= s.len {
		return 1
	}
	if s.kind == arcLine {
		return d / s.len
	}
	// Binary search LUT (9 entries: indices 0..8).
	lo, hi := 0, 8
	for lo+1 < hi {
		mid := (lo + hi) >> 1
		if s.lut[mid] <= d {
			lo = mid
		} else {
			hi = mid
		}
	}
	tLo := float64(lo) * 0.125
	tHi := float64(hi) * 0.125
	span := s.lut[hi] - s.lut[lo]
	t := tLo
	if span > 0 {
		t = tLo + (d-s.lut[lo])/span*(tHi-tLo)
	}
	// 2 Newton steps with bracketing fallback.
	aLo, aHi := tLo, tHi
	for range 3 {
		cur := s.lut[lo] + gl5SegLen(s, tLo, t)
		diff := cur - d
		if math.Abs(diff) < 1e-5 {
			break
		}
		if diff < 0 {
			aLo = t
		} else {
			aHi = t
		}
		sp := s.speed(t)
		if sp > 1e-9 {
			next := t - diff/sp
			if next > aLo && next < aHi {
				t = next
				continue
			}
		}
		t = 0.5 * (aLo + aHi)
	}
	return max(0, min(t, 1))
}

// splitSeg extracts the sub-segment over parameter range [t0, t1] via de Casteljau.
func splitSeg(s *arcSeg, t0, t1 float64) (kind arcSegKind, pts [8]float64) {
	if t0 <= 0 && t1 >= 1 {
		return s.kind, s.pts
	}
	kind = s.kind
	switch s.kind {
	case arcLine:
		x0, y0, x1, y1 := s.pts[0], s.pts[1], s.pts[2], s.pts[3]
		pts[0] = x0 + (x1-x0)*t0
		pts[1] = y0 + (y1-y0)*t0
		pts[2] = x0 + (x1-x0)*t1
		pts[3] = y0 + (y1-y0)*t1
	case arcQuad:
		p0x, p0y := s.pts[0], s.pts[1]
		p1x, p1y := s.pts[2], s.pts[3]
		p2x, p2y := s.pts[4], s.pts[5]
		// Split at t1 -> keep left [0, t1]
		if t1 < 1 {
			q0x, q0y := lerp(p0x, p1x, t1), lerp(p0y, p1y, t1)
			q1x, q1y := lerp(p1x, p2x, t1), lerp(p1y, p2y, t1)
			r0x, r0y := lerp(q0x, q1x, t1), lerp(q0y, q1y, t1)
			p1x, p1y = q0x, q0y
			p2x, p2y = r0x, r0y
		}
		// Split [0, t1] at u = t0/t1 -> keep right [t0, t1]
		if t0 > 0 && t1 > 0 {
			u := t0 / t1
			q0x, q0y := lerp(p0x, p1x, u), lerp(p0y, p1y, u)
			q1x, q1y := lerp(p1x, p2x, u), lerp(p1y, p2y, u)
			r0x, r0y := lerp(q0x, q1x, u), lerp(q0y, q1y, u)
			p0x, p0y = r0x, r0y
			p1x, p1y = q1x, q1y
		}
		pts = [8]float64{p0x, p0y, p1x, p1y, p2x, p2y}
	case arcCubic:
		p0x, p0y := s.pts[0], s.pts[1]
		p1x, p1y := s.pts[2], s.pts[3]
		p2x, p2y := s.pts[4], s.pts[5]
		p3x, p3y := s.pts[6], s.pts[7]
		if t1 < 1 {
			q0x, q0y := lerp(p0x, p1x, t1), lerp(p0y, p1y, t1)
			q1x, q1y := lerp(p1x, p2x, t1), lerp(p1y, p2y, t1)
			q2x, q2y := lerp(p2x, p3x, t1), lerp(p2y, p3y, t1)
			r0x, r0y := lerp(q0x, q1x, t1), lerp(q0y, q1y, t1)
			r1x, r1y := lerp(q1x, q2x, t1), lerp(q1y, q2y, t1)
			s0x, s0y := lerp(r0x, r1x, t1), lerp(r0y, r1y, t1)
			p1x, p1y = q0x, q0y
			p2x, p2y = r0x, r0y
			p3x, p3y = s0x, s0y
		}
		if t0 > 0 && t1 > 0 {
			u := t0 / t1
			q0x, q0y := lerp(p0x, p1x, u), lerp(p0y, p1y, u)
			q1x, q1y := lerp(p1x, p2x, u), lerp(p1y, p2y, u)
			q2x, q2y := lerp(p2x, p3x, u), lerp(p2y, p3y, u)
			r0x, r0y := lerp(q0x, q1x, u), lerp(q0y, q1y, u)
			r1x, r1y := lerp(q1x, q2x, u), lerp(q1y, q2y, u)
			s0x, s0y := lerp(r0x, r1x, u), lerp(r0y, r1y, u)
			p0x, p0y = s0x, s0y
			p1x, p1y = r1x, r1y
			p2x, p2y = q2x, q2y
		}
		pts = [8]float64{p0x, p0y, p1x, p1y, p2x, p2y, p3x, p3y}
	}
	return kind, pts
}

func lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}
