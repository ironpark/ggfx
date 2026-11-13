// Gradient implementation fo rasterx package
// Copyright 2018 All rights reserved.
// Created: 5/12/2018 by S.R.Wiley

package rasterx

import (
	"image/color"
	"math"
)

// SVG bounds paremater constants
const (
	ObjectBoundingBox GradientUnits = iota
	UserSpaceOnUse
)

// SVG spread parameter constants
const (
	PadSpread SpreadMethod = iota
	ReflectSpread
	RepeatSpread
)

const epsilonF = 1e-5

type (
	// SpreadMethod is the type for spread parameters
	SpreadMethod byte
	// GradientUnits is the type for gradient units
	GradientUnits byte
	// GradStop represents a stop in the SVG 2.0 gradient specification
	GradStop struct {
		StopColor color.Color
		Offset    float64
		Opacity   float64
	}
	// Gradient holds a description of an SVG 2.0 gradient. Its Stops are
	// in order of their offsets.
	Gradient struct {
		Points [5]float64
		Stops  []GradStop
		// Bounds is the box, in the user space GetColorFunctionUS is
		// given, an ObjectBoundingBox gradient is drawn in.
		Bounds   struct{ X, Y, W, H float64 }
		Matrix   Matrix2D
		Spread   SpreadMethod
		Units    GradientUnits
		IsRadial bool
	}
)

// ApplyOpacity is c, not premultiplied, its alpha scaled by opacity.
func ApplyOpacity(c color.Color, opacity float64) color.NRGBA {
	r, g, b, a := nrgba64(c)
	return applyOpacity64(r, g, b, a, opacity)
}

// nrgba64 is c's 16-bit color and alpha, not premultiplied, and all 0
// where it is transparent. Premultiplied to 16 bits, a faint color loses
// bits it has here; and RGBA's values are premultiplied: their low byte,
// which the original took, is the 8-bit color only for an opaque 8-bit
// one.
func nrgba64(c color.Color) (r, g, b, a uint32) {
	switch n := c.(type) {
	case color.NRGBA:
		r, g, b, a = uint32(n.R)*0x101, uint32(n.G)*0x101, uint32(n.B)*0x101, uint32(n.A)*0x101
	case color.NRGBA64:
		r, g, b, a = uint32(n.R), uint32(n.G), uint32(n.B), uint32(n.A)
	default:
		r, g, b, a = c.RGBA()
		if a != 0xffff && a != 0 {
			r, g, b = unpremultiply(r, a), unpremultiply(g, a), unpremultiply(b, a)
		}
	}
	if a == 0 {
		return 0, 0, 0, 0
	}
	return r, g, b, a
}

// applyOpacity64 is ApplyOpacity of the 16-bit r, g, b, a, not
// premultiplied.
func applyOpacity64(r, g, b, a uint32, opacity float64) color.NRGBA {
	opacity = max(0, min(opacity, 1))
	switch a {
	case 0xffff:
		return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(opacity * 0xFF)}
	case 0:
		return color.NRGBA{}
	}
	// a/0x101 is an 8-bit alpha exactly, so an 8-bit color comes back as
	// it was.
	return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(opacity * float64(a) / 0x101)}
}

// applyOpacity is ApplyOpacity of an opaque r, g, b, without boxing it,
// which allocated at each pixel.
func applyOpacity(r, g, b uint8, opacity float64) color.NRGBA {
	opacity = max(0, min(opacity, 1))
	return color.NRGBA{r, g, b, uint8(opacity * 0xFF)}
}

// unpremultiply is the 16-bit v premultiplied by a, not premultiplied,
// rounded so that an 8-bit color survives premultiplying.
func unpremultiply(v, a uint32) uint32 {
	return min((v*0xffff+a/2)/a, 0xffff)
}

type (
	// gradStops are a gradient's stops as a paint of it at an opacity
	// looks its colors up, each stop's decoded once, not at each pixel.
	gradStops struct {
		stops       []stop
		spread      SpreadMethod
		opacity     float64
		first, last color.RGBA64 // the first and last stops at opacity
	}
	// stop is a stop's 16-bit color and alpha, not premultiplied, its
	// opacity, and that with its color's alpha in it, which blends.
	stop struct {
		offset     float64
		r, g, b, a uint32
		opacity    float64
		blend      float64
	}
)

// colors is g's stops for a paint at opacity.
func (g *Gradient) colors(opacity float64) gradStops {
	stops := make([]stop, len(g.Stops))
	for i, s := range g.Stops {
		r, gr, b, a := nrgba64(s.StopColor)
		// Colors and opacities blend apart, not premultiplied, as SVG
		// has it; a stop color's own alpha is part of its opacity.
		stops[i] = stop{s.Offset, r, gr, b, a, s.Opacity, s.Opacity * (float64(a) / 0xffff)}
	}
	st := gradStops{stops: stops, spread: g.Spread, opacity: opacity}
	st.first, st.last = st.at(&stops[0]), st.at(&stops[len(stops)-1])
	return st
}

// at is s's color at the paint's opacity.
func (st *gradStops) at(s *stop) color.RGBA64 {
	return ColorPaint(applyOpacity64(s.r, s.g, s.b, s.a, s.opacity*st.opacity)).Color
}

// color is the color at t along the gradient: past its ends, the first or
// last stop's for PadSpread; t modulo 1 for RepeatSpread, which wraps from
// the last stop's color to the first's with no blend between them; and t
// modulo 2 mirrored into [0, 1] for ReflectSpread (SVG 1.1 13.2.4).
func (st gradStops) color(t float64) color.RGBA64 {
	switch st.spread {
	case RepeatSpread:
		t -= math.Floor(t) // exact where t >= 0
	case ReflectSpread:
		if t -= float64(2 * math.Floor(t/2)); t > 1 {
			t = 2 - t
		}
	default:
		if t >= 1 {
			return st.last
		}
		if t <= 0 {
			return st.first
		}
	}
	// Past here, t is in [0, 1], or NaN, which no comparison takes.
	s := st.stops
	if !(t > s[0].offset) {
		return st.first
	}
	place := 1 // the first stop t is not past
	for place != len(s) && t > s[place].offset {
		place++
	}
	if place == len(s) {
		return st.last
	}
	return st.blend(t, &s[place-1], &s[place])
}

// blend is the color at t between s1 and s2, t past s1's offset and not
// past s2's.
func (st *gradStops) blend(t float64, s1, s2 *stop) color.RGBA64 {
	tp := (t - s1.offset) / (s2.offset - s1.offset)
	return ColorPaint(applyOpacity(
		uint8((float64(float64(s1.r)*(1-tp))+float64(float64(s2.r)*tp))/256),
		uint8((float64(float64(s1.g)*(1-tp))+float64(float64(s2.g)*tp))/256),
		uint8((float64(float64(s1.b)*(1-tp))+float64(float64(s2.b)*tp))/256),
		(float64(s1.blend*(1-tp))+float64(s2.blend*tp))*st.opacity)).Color
}

// GetColorFunction returns the color function
func (g *Gradient) GetColorFunction(opacity float64) Paint {
	return g.GetColorFunctionUS(opacity, Identity)
}

// GetColorFunctionUS returns the color function using the User Space
// objMatrix. An ObjectBoundingBox gradient is drawn in Bounds, a box in
// that user space: as SVG has it, it is a UserSpaceOnUse gradient under
// objMatrix times the box's matrix. Either way its geometry stays in the
// gradient's own space, into which each pixel is mapped back, so that a
// radial one under a rotation, skew or uneven scale is an ellipse turned
// as the shape is.
func (g *Gradient) GetColorFunctionUS(opacity float64, objMatrix Matrix2D) Paint {
	switch len(g.Stops) {
	case 0:
		return ColorPaint(ApplyOpacity(color.RGBA{0, 0, 0, 255}, opacity)) // default error color for gradient w/o stops.
	case 1:
		return ColorPaint(ApplyOpacity(g.Stops[0].StopColor, opacity)) // Illegal, I think, should really should not happen.
	}
	st := g.colors(opacity)
	toUser := objMatrix
	if g.Units == ObjectBoundingBox {
		toUser = toUser.Mult(Identity.Translate(g.Bounds.X, g.Bounds.Y).Scale(g.Bounds.W, g.Bounds.H))
	}
	// toGrad maps a pixel's center to where the geometry is.
	toGrad := toUser.Mult(g.Matrix).Invert()
	p := g.Points

	if g.IsRadial {
		cx, cy, fx, fy, r := p[0], p[1], p[2], p[3], p[4]
		if cx == fx && cy == fy {
			return st.centered(toGrad, cx, cy, r)
		}
		fx /= r
		fy /= r
		cx /= r
		cy /= r

		dfx := fx - cx
		dfy := fy - cy

		if float64(dfx*dfx)+float64(dfy*dfy) > 1 { // Focus outside of circle; use intersection
			// point of line from center to focus and circle as per SVG specs.
			nfx, nfy, intersects := RayCircleIntersectionF(fx, fy, cx, cy, cx, cy, 1.0-epsilonF)
			fx, fy = nfx, nfy
			if intersects == false {
				return ColorPaint(color.RGBA{255, 255, 0, 255}) // should not happen
			}
		}
		return st.focal(toGrad, fx, fy, cx, cy, r)
	}
	return st.linear(toGrad, p[0], p[1], p[2], p[3])
}

// The paints below take their geometry as parameters, which their
// closures capture by value: a variable assigned after it is captured
// would be moved to the heap. Each maps a pixel's center by toGrad to
// where its geometry is; for Identity, exactly to itself.

// centered is the paint of a radial gradient whose focus is its center:
// t is the distance from the center over r.
func (st gradStops) centered(toGrad Matrix2D, cx, cy, r float64) Paint {
	return FuncPaint(func(xi, yi int) color.RGBA64 {
		x, y := toGrad.Transform(float64(xi)+0.5, float64(yi)+0.5)
		dx := x - cx
		dy := y - cy
		return st.color(math.Sqrt(float64(dx*dx/(r*r)) + float64((dy*dy)/(r*r))))
	})
}

// focal is the paint of a radial gradient whose focus, (fx, fy), is not
// its center, (cx, cy), both divided by the radius r, the circle then of
// radius 1. A point e is at t = |e-f| / |p-f| along the ray from the
// focus f through it to the circle at p = f + (e-f)/t: with g = f-c and
// d = e-f, |g + d/t| = 1, whose root past the focus, the focus inside, is
// t = (g·d + √((g·d)² + |d|²(1-|g|²))) / (1-|g|²).
func (st gradStops) focal(toGrad Matrix2D, fx, fy, cx, cy, r float64) Paint {
	gx, gy := fx-cx, fy-cy
	den := 1 - (float64(gx*gx) + float64(gy*gy))
	return FuncPaint(func(xi, yi int) color.RGBA64 {
		x, y := toGrad.Transform(float64(xi)+0.5, float64(yi)+0.5)
		dx, dy := x/r-fx, y/r-fy
		gd := float64(gx*dx) + float64(gy*dy)
		dd := float64(dx*dx) + float64(dy*dy)
		t := (gd + math.Sqrt(float64(gd*gd)+float64(dd*den))) / den
		if dd < float64(epsilonF*t)*t {
			// p is all but on the focus: the circle's edge, where the
			// focus is on it.
			return st.last
		}
		return st.color(t)
	})
}

// linear is the paint of a linear gradient from (p1x, p1y) to (p2x, p2y).
func (st gradStops) linear(toGrad Matrix2D, p1x, p1y, p2x, p2y float64) Paint {
	dx := p2x - p1x
	dy := p2y - p1y
	d := float64(dx*dx) + float64(dy*dy) // self inner prod
	return FuncPaint(func(xi, yi int) color.RGBA64 {
		x, y := toGrad.Transform(float64(xi)+0.5, float64(yi)+0.5)
		dfx := x - p1x
		dfy := y - p1y
		return st.color((float64(dx*dfx) + float64(dy*dfy)) / d)
	})
}
