// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"math"
	"strconv"
	"strings"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// A shape that names markers, as a diagram's edges name the arrowheads
// they end in, has each marker's shapes drawn after it at the vertex it is
// for: turned as the shape runs there (or as the marker's orient says),
// sized by the shape's stroke and the marker's view box, with its refX and
// refY on the vertex. Its shapes take their style from the default, as
// they would from the marker's ancestors, not from the shape.

// markerShapes are the elements markers may be put on.
var markerShapes = map[string]bool{"path": true, "line": true, "polyline": true, "polygon": true}

// addMarkers draws, after the shape tag drew as p in style, the markers
// the style names.
func (c *iconCursor) addMarkers(tag string, p rasterx.Path, style PathStyle) {
	if style.markers == [3]string{} || !markerShapes[tag] {
		return
	}
	vs := vertices(p)
	for i, v := range vs {
		which, first := 1, false
		switch i {
		case 0:
			which, first = 0, true
		case len(vs) - 1:
			which = 2
		}
		def := c.icon.markers[style.markers[which]]
		if def == nil {
			continue
		}
		if _, err := c.expandCaptured(def, markerAt(def, v, first, style), true); err != nil {
			return
		}
	}
}

// markerAt is where def's shapes go for the vertex v of a shape drawn in
// style: the shape's user space, moved to v, turned, and scaled.
func markerAt(def *captured, v vertex, first bool, style PathStyle) rasterx.Matrix2D {
	refX, refY, w, h := 0.0, 0.0, 3.0, 3.0
	orient, strokeUnits := "0", true
	var box []float64
	for _, a := range def.attrs {
		n, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(a.Value), "px"), 64)
		switch a.Name.Local {
		case "refX":
			refX = n
		case "refY":
			refY = n
		case "markerWidth":
			w = n
		case "markerHeight":
			h = n
		case "orient":
			orient = strings.TrimSpace(a.Value)
		case "markerUnits":
			strokeUnits = a.Value != "userSpaceOnUse"
		case "viewBox":
			for _, f := range splitOnCommaOrSpace(a.Value) {
				if n, err := strconv.ParseFloat(f, 64); err == nil {
					box = append(box, n)
				}
			}
		}
	}
	sx, sy := 1.0, 1.0
	if len(box) == 4 && box[2] > 0 && box[3] > 0 {
		// Meet: the box's shape kept, its larger side filling the marker.
		s := min(w/box[2], h/box[3])
		sx, sy = s, s
		refX -= box[0]
		refY -= box[1]
	}
	if strokeUnits {
		sx *= style.LineWidth
		sy *= style.LineWidth
	}
	angle := v.angle
	switch orient {
	case "auto":
	case "auto-start-reverse":
		if first {
			angle += math.Pi
		}
	default:
		deg, _ := strconv.ParseFloat(strings.TrimSuffix(orient, "deg"), 64)
		angle = deg * math.Pi / 180
	}
	return style.mAdder.M.Translate(v.x, v.y).Rotate(angle).Scale(sx, sy).Translate(-refX, -refY)
}

// vertex is where a marker goes, and the way the shape runs there, in
// radians.
type vertex struct{ x, y, angle float64 }

// vertices are a path's vertices, in order, each turned as the path runs
// through it: halfway between the segments either side of an inner one. A
// curve runs toward and away from its control points.
func vertices(p rasterx.Path) []vertex {
	var vs []vertex
	var cur, start [2]float64
	var lastOut float64
	pt := func(i int) [2]float64 { return [2]float64{float64(p[i]) / 64, float64(p[i+1]) / 64} }
	dir := func(a, b [2]float64) float64 { return math.Atan2(b[1]-a[1], b[0]-a[0]) }
	// seg is a segment from cur to to, leaving toward in and arriving from
	// out: a line's are its ends.
	seg := func(in, out, to [2]float64) {
		din, dout := dir(cur, in), dir(out, to)
		if in == cur {
			din = dir(cur, to)
		}
		if out == to {
			dout = dir(cur, to)
		}
		if n := len(vs); n > 0 && vs[n-1].x == cur[0] && vs[n-1].y == cur[1] {
			if n > 1 {
				d := math.Mod(din-lastOut+3*math.Pi, 2*math.Pi) - math.Pi
				vs[n-1].angle = lastOut + d/2
			} else {
				vs[n-1].angle = din
			}
		} else {
			vs = append(vs, vertex{cur[0], cur[1], din})
		}
		vs = append(vs, vertex{to[0], to[1], dout})
		lastOut = dout
		cur = to
	}
	for i := 0; i < len(p); {
		switch rasterx.PathCommand(p[i]) {
		case rasterx.PathMoveTo:
			cur = pt(i + 1)
			start = cur
			i += 3
		case rasterx.PathLineTo:
			to := pt(i + 1)
			seg(to, cur, to)
			i += 3
		case rasterx.PathQuadTo:
			ctl, to := pt(i+1), pt(i+3)
			seg(ctl, ctl, to)
			i += 5
		case rasterx.PathCubicTo:
			c1, c2, to := pt(i+1), pt(i+3), pt(i+5)
			seg(c1, c2, to)
			i += 7
		case rasterx.PathClose:
			if cur != start {
				seg(start, cur, start)
			}
			i++
		default:
			return vs
		}
	}
	return vs
}
