// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"encoding/xml"
	"errors"
	"math"
	"slices"

	"golang.org/x/image/math/fixed"

	"github.com/ironpark/ggfx/internal/rasterx"
)

type geomKind uint8

const (
	geomNone geomKind = iota
	geomRect
	geomCircle
	geomEllipse
	geomLine
	geomPolyline
	geomPolygon
	geomPath
	geomUse
)

// geometry stores the parsed shape parameters for an element so that its path
// can be compiled once statically and rebuilt at draw time when geometry
// attributes are animated.
type geometry struct {
	kind geomKind

	// Rect: x, y, w, h, rx, ry, setRx, setRy
	// Circle/Ellipse: cx, cy, rx, ry (for circle, r is stored in rx and ry)
	// Line: x1, y1, x2, y2
	// Use: x, y, href
	x, y, w, h   float64
	rx, ry       float64
	setRx, setRy bool

	points []float64
	d      string
	href   string
	bad    bool // parse error on static geometry (legacy skips)
}

func buildRect(dst *rasterx.Path, x, y, w, h, rx, ry float64, setRx, setRy bool) {
	if w <= 0 || h <= 0 {
		return
	}
	if rx < 0 {
		rx = 0
	}
	if ry < 0 {
		ry = 0
	}
	// One radius given is both, as the specification has it.
	if setRx && !setRy {
		ry = rx
	} else if setRy && !setRx {
		rx = ry
	}
	rasterx.AddRoundRect(x, y, w+x, h+y, rx, ry, 0, rasterx.RoundGap, dst)
}

func buildEllipse(dst *rasterx.Path, cx, cy, rx, ry float64) {
	if rx <= 0 || ry <= 0 {
		return
	}
	px, py := cx+rx, cy
	pts := [7]float64{rx, ry, 0.0, 1.0, 0.0, px, py}
	dst.Start(fixed.Point26_6{
		X: fixed.Int26_6(px * 64),
		Y: fixed.Int26_6(py * 64),
	})
	rasterx.AddArc(pts[:], cx, cy, px, py, dst)
	dst.Stop(true)
}

func buildLine(dst *rasterx.Path, x1, y1, x2, y2 float64) {
	dst.Start(fixed.Point26_6{
		X: fixed.Int26_6(x1 * 64),
		Y: fixed.Int26_6(y1 * 64),
	})
	dst.Line(fixed.Point26_6{
		X: fixed.Int26_6(x2 * 64),
		Y: fixed.Int26_6(y2 * 64),
	})
}

func buildPoly(dst *rasterx.Path, pts []float64, closed bool) {
	if len(pts) >= 4 {
		dst.Start(fixed.Point26_6{
			X: fixed.Int26_6(pts[0] * 64),
			Y: fixed.Int26_6(pts[1] * 64),
		})
		for i := 2; i < len(pts)-1; i += 2 {
			dst.Line(fixed.Point26_6{
				X: fixed.Int26_6(pts[i] * 64),
				Y: fixed.Int26_6(pts[i+1] * 64),
			})
		}
		if closed {
			dst.Stop(true)
		}
	}
}

func buildPathString(dst *rasterx.Path, d string) error {
	var pc pathCursor
	err := pc.CompilePath(d)
	if len(pc.Path) > 0 {
		*dst = append(*dst, pc.Path...)
	}
	return err
}

func (g *geometry) buildInto(dst *rasterx.Path) {
	if g.bad {
		return
	}
	switch g.kind {
	case geomRect:
		buildRect(dst, g.x, g.y, g.w, g.h, g.rx, g.ry, g.setRx, g.setRy)
	case geomCircle, geomEllipse:
		buildEllipse(dst, g.x, g.y, g.rx, g.ry)
	case geomLine:
		buildLine(dst, g.x, g.y, g.w, g.h)
	case geomPolyline:
		buildPoly(dst, g.points, false)
	case geomPolygon:
		buildPoly(dst, g.points, true)
	case geomPath:
		_ = buildPathString(dst, g.d)
	}
}

func parseGeometry(tag string, attrs []xml.Attr) (geometry, rasterx.Path) {
	var g geometry
	var err error
	switch tag {
	case "rect":
		g.kind = geomRect
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "x":
				g.x, err = parseFloat(attr.Value, 64)
			case "y":
				g.y, err = parseFloat(attr.Value, 64)
			case "width":
				g.w, err = parseFloat(attr.Value, 64)
			case "height":
				g.h, err = parseFloat(attr.Value, 64)
			case "rx":
				g.rx, err = parseFloat(attr.Value, 64)
				g.setRx = true
			case "ry":
				g.ry, err = parseFloat(attr.Value, 64)
				g.setRy = true
			}
			if err != nil {
				g.bad = true
				return g, nil
			}
		}
	case "circle", "ellipse":
		if tag == "circle" {
			g.kind = geomCircle
		} else {
			g.kind = geomEllipse
		}
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "cx":
				g.x, err = parseFloat(attr.Value, 64)
			case "cy":
				g.y, err = parseFloat(attr.Value, 64)
			case "r":
				g.rx, err = parseFloat(attr.Value, 64)
				g.ry = g.rx
			case "rx":
				g.rx, err = parseFloat(attr.Value, 64)
			case "ry":
				g.ry, err = parseFloat(attr.Value, 64)
			}
			if err != nil {
				g.bad = true
				return g, nil
			}
		}
	case "line":
		g.kind = geomLine
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "x1":
				g.x, err = parseFloat(attr.Value, 64)
			case "y1":
				g.y, err = parseFloat(attr.Value, 64)
			case "x2":
				g.w, err = parseFloat(attr.Value, 64)
			case "y2":
				g.h, err = parseFloat(attr.Value, 64)
			}
			if err != nil {
				g.bad = true
				return g, nil
			}
		}
	case "polyline", "polygon":
		if tag == "polyline" {
			g.kind = geomPolyline
		} else {
			g.kind = geomPolygon
		}
		var pc pathCursor
		for _, attr := range attrs {
			if attr.Name.Local == "points" {
				err = pc.GetPoints(attr.Value)
				if err == nil && len(pc.points)%2 != 0 {
					err = errors.New("polygon has odd number of points")
				}
			}
			if err != nil {
				g.bad = true
				return g, nil
			}
		}
		if len(pc.points) > 0 {
			g.points = slices.Clone(pc.points)
		}
	case "path":
		g.kind = geomPath
		var p rasterx.Path
		for _, attr := range attrs {
			if attr.Name.Local == "d" {
				g.d = attr.Value
				var pc pathCursor
				err = pc.CompilePath(attr.Value)
				if len(pc.Path) > 0 {
					p = append(p, pc.Path...)
				}
				if err != nil {
					// Legacy swallows CompilePath errors and keeps whatever partial
					// path was accumulated before the error.
					return g, p
				}
			}
		}
		return g, p
	case "use":
		g.kind = geomUse
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "href":
				g.href = attr.Value
			case "x":
				g.x, err = parseFloat(attr.Value, 64)
			case "y":
				g.y, err = parseFloat(attr.Value, 64)
			}
			if err != nil {
				g.bad = true
				return g, nil
			}
		}
		return g, nil
	default:
		return g, nil
	}

	var p rasterx.Path
	g.buildInto(&p)
	return g, p
}

// pathBoundsTransformed computes the exact axis-aligned bounding box of p
// after transforming every point and Bezier curve by m.
func pathBoundsTransformed(p rasterx.Path, m rasterx.Matrix2D) (minX, minY, maxX, maxY float64, ok bool) {
	if len(p) == 0 {
		return 0, 0, 0, 0, false
	}
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	add := func(x, y float64) {
		minX = min(minX, x)
		minY = min(minY, y)
		maxX = max(maxX, x)
		maxY = max(maxY, y)
	}
	pt := func(i int) (float64, float64) {
		return m.Transform(float64(p[i])/64, float64(p[i+1])/64)
	}
	var x0, y0 float64
	for i := 0; i < len(p); {
		switch rasterx.PathCommand(p[i]) {
		case rasterx.PathMoveTo, rasterx.PathLineTo:
			x0, y0 = pt(i + 1)
			add(x0, y0)
			i += 3
		case rasterx.PathQuadTo:
			x1, y1 := pt(i + 1)
			x2, y2 := pt(i + 3)
			addQuadBounds(x0, x1, x2, y0, y1, y2, add)
			x0, y0 = x2, y2
			add(x0, y0)
			i += 5
		case rasterx.PathCubicTo:
			x1, y1 := pt(i + 1)
			x2, y2 := pt(i + 3)
			x3, y3 := pt(i + 5)
			addCubicBounds(x0, x1, x2, x3, y0, y1, y2, y3, add)
			x0, y0 = x3, y3
			add(x0, y0)
			i += 7
		case rasterx.PathClose:
			i++
		default:
			return 0, 0, 0, 0, false
		}
	}
	if minX > maxX {
		return 0, 0, 0, 0, false
	}
	return minX, minY, maxX, maxY, true
}

func addQuadBounds(ax, bx, cx, ay, by, cy float64, add func(x, y float64)) {
	eval := func(a, b, c, t float64) float64 {
		mt := 1 - t
		return mt*mt*a + 2*mt*t*b + t*t*c
	}
	if d := ax - 2*bx + cx; d != 0 {
		if t := (ax - bx) / d; t > 0 && t < 1 {
			add(eval(ax, bx, cx, t), eval(ay, by, cy, t))
		}
	}
	if d := ay - 2*by + cy; d != 0 {
		if t := (ay - by) / d; t > 0 && t < 1 {
			add(eval(ax, bx, cx, t), eval(ay, by, cy, t))
		}
	}
}

func addCubicBounds(ax, bx, cx, dx, ay, by, cy, dy float64, add func(x, y float64)) {
	eval := func(a, b, c, d, t float64) float64 {
		mt := 1 - t
		return mt*mt*mt*a + 3*mt*mt*t*b + 3*mt*t*t*c + t*t*t*d
	}
	solve := func(a, b, c, d float64) {
		A := -a + 3*b - 3*c + d
		B := a - 2*b + c
		C := b - a
		if A == 0 {
			if B != 0 {
				if t := -C / (2 * B); t > 0 && t < 1 {
					add(eval(ax, bx, cx, dx, t), eval(ay, by, cy, dy, t))
				}
			}
			return
		}
		disc := B*B - A*C
		if disc < 0 {
			return
		}
		sd := math.Sqrt(disc)
		for _, t := range [2]float64{(-B + sd) / A, (-B - sd) / A} {
			if t > 0 && t < 1 {
				add(eval(ax, bx, cx, dx, t), eval(ay, by, cy, dy, t))
			}
		}
	}
	solve(ax, bx, cx, dx)
	solve(ay, by, cy, dy)
}
