// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"encoding/xml"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/ironpark/ggfx/internal/rasterx"
	"github.com/ironpark/ggfx/internal/trig"
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
func (c *iconCursor) addMarkers(tag string, p rasterx.Path, style PathStyle) error {
	if style.markers == [3]string{} || !markerShapes[tag] {
		return nil
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
		start := len(c.icon.SVGPaths)
		if _, err := c.expandCaptured(def, markerAt(def, v, first, style), true); err != nil {
			if errors.Is(err, ErrExpansionLimit) || errors.Is(err, ErrNestingDepth) {
				return err
			}
			return nil
		}
		if style.clip != nil {
			for j := start; j < len(c.icon.SVGPaths); j++ {
				c.icon.SVGPaths[j].clip = chainClip(c.icon.SVGPaths[j].clip, style.clip)
			}
		}
	}
	return nil
}

type markerOrientKind uint8

const (
	markerOrientFixed markerOrientKind = iota
	markerOrientAuto
	markerOrientAutoStartReverse
)

// parsedMarker caches the parsed geometric attributes of a <marker> element
// so placement at each vertex only performs matrix math.
type parsedMarker struct {
	refX, refY  float64
	sx, sy      float64
	strokeUnits bool
	orientKind  markerOrientKind
	angleRad    float64
}

func parseMarkerAttrs(attrs []xml.Attr) parsedMarker {
	refX, refY, w, h := 0.0, 0.0, 3.0, 3.0
	orient, strokeUnits := "0", true
	var box [4]float64
	boxN := 0
	for _, a := range attrs {
		switch a.Name.Local {
		case "refX":
			refX, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(a.Value), "px"), 64)
		case "refY":
			refY, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(a.Value), "px"), 64)
		case "markerWidth":
			w, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(a.Value), "px"), 64)
		case "markerHeight":
			h, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(a.Value), "px"), 64)
		case "orient":
			orient = strings.TrimSpace(a.Value)
		case "markerUnits":
			strokeUnits = a.Value != "userSpaceOnUse"
		case "viewBox":
			boxN = 0
			for _, f := range splitOnCommaOrSpace(a.Value) {
				if n, err := strconv.ParseFloat(f, 64); err == nil {
					if boxN < 4 {
						box[boxN] = n
					}
					boxN++
				}
			}
		}
	}
	sx, sy := 1.0, 1.0
	if boxN == 4 && box[2] > 0 && box[3] > 0 {
		// Meet: the box's shape kept, its larger side filling the marker.
		// refX and refY are already in the marker's viewBox coordinate system (SVG 1.1 §11.6.2).
		s := min(w/box[2], h/box[3])
		sx, sy = s, s
	}
	pm := parsedMarker{
		refX:        refX,
		refY:        refY,
		sx:          sx,
		sy:          sy,
		strokeUnits: strokeUnits,
	}
	switch orient {
	case "auto":
		pm.orientKind = markerOrientAuto
	case "auto-start-reverse":
		pm.orientKind = markerOrientAutoStartReverse
	default:
		pm.orientKind = markerOrientFixed
		deg, _ := strconv.ParseFloat(strings.TrimSuffix(orient, "deg"), 64)
		pm.angleRad = deg * math.Pi / 180
	}
	return pm
}

func (pm *parsedMarker) place(shapeWorld rasterx.Matrix2D, v vertex, first bool, lineWidth float64) rasterx.Matrix2D {
	sx, sy := pm.sx, pm.sy
	if pm.strokeUnits {
		sx *= lineWidth
		sy *= lineWidth
	}
	var angle float64
	switch pm.orientKind {
	case markerOrientAuto:
		angle = v.angle
	case markerOrientAutoStartReverse:
		angle = v.angle
		if first {
			angle += math.Pi
		}
	default:
		angle = pm.angleRad
	}
	return shapeWorld.Translate(v.x, v.y).Rotate(angle).Scale(sx, sy).Translate(-pm.refX, -pm.refY)
}

// markerAt is where def's shapes go for the vertex v of a shape drawn in
// style: the shape's user space, moved to v, turned, and scaled.
func markerAt(def *captured, v vertex, first bool, style PathStyle) rasterx.Matrix2D {
	return def.marker.place(style.m, v, first, style.LineWidth)
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
	dir := func(a, b [2]float64) float64 { return trig.Atan2(b[1]-a[1], b[0]-a[0]) }
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
				vs[n-1].angle = lastOut + float64(d/2) // not fused, as on arm64
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
