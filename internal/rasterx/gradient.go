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

// color takes the paramaterized value along the gradient's stops and
// returns a color depending on the spreadMethod value of the gradient and
// the gradient's slice of stop values.
func (st gradStops) color(t float64) color.RGBA64 {
	s := st.stops
	d := len(s)
	mod := t
	if st.spread == PadSpread {
		// These cases can be taken care of early on; past them, t is in
		// (0, 1), or NaN, as math.Mod would leave it.
		if t >= 1.0 {
			return st.last
		}
		if t <= 0.0 {
			return st.first
		}
	} else {
		var modRange = 1.0
		if st.spread == ReflectSpread {
			modRange = 2.0
		}
		mod = math.Mod(t, modRange)
		if mod < 0 {
			mod += modRange
		}
	}

	place := 0 // Advance to place where mod is greater than the indicated stop
	for place != d && mod > s[place].offset {
		place++
	}
	switch st.spread {
	case RepeatSpread:
		var s1, s2 *stop
		switch place {
		case 0, d:
			s1, s2 = &s[d-1], &s[0]
		default:
			s1, s2 = &s[place-1], &s[place]
		}
		return st.blend(mod, s1, s2, false)
	case ReflectSpread:
		switch place {
		case 0:
			return st.first
		case d:
			// Advance to place where mod-1 is greater than the stop indicated by place in reverse of the stop slice.
			// Since this is the reflect spead mode, the mod interval is two, allowing the stop list to be
			// iterated in reverse before repeating the sequence.
			for place != d*2 && mod-1 > (1-s[d*2-place-1].offset) {
				place++
			}
			switch place {
			case d:
				return st.last
			case d * 2:
				return st.first
			default:
				return st.blend(mod-1, &s[d*2-place], &s[d*2-place-1], true)
			}
		default:
			return st.blend(mod, &s[place-1], &s[place], false)
		}
	default: // PadSpread
		switch place {
		case 0:
			return st.first
		case d:
			return st.last
		default:
			return st.blend(mod, &s[place-1], &s[place], false)
		}
	}
}

func (st *gradStops) blend(t float64, s1, s2 *stop, flip bool) color.RGBA64 {
	s1off := s1.offset
	if s1.offset > s2.offset && !flip { // happens in repeat spread mode
		s1off--
		if t > 1 {
			t--
		}
	}
	if s2.offset == s1off {
		return st.at(s2)
	}
	if flip {
		t = 1 - t
	}
	tp := (t - s1off) / (s2.offset - s1off)
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
// objMatrix times the box's matrix. Its geometry is in the gradient's own
// space, into which each pixel is mapped back; a UserSpaceOnUse one's is
// mapped forward into the pixels', its radii as vectors, which is right
// only where the matrices neither rotate nor skew.
func (g *Gradient) GetColorFunctionUS(opacity float64, objMatrix Matrix2D) Paint {
	switch len(g.Stops) {
	case 0:
		return ColorPaint(ApplyOpacity(color.RGBA{0, 0, 0, 255}, opacity)) // default error color for gradient w/o stops.
	case 1:
		return ColorPaint(ApplyOpacity(g.Stops[0].StopColor, opacity)) // Illegal, I think, should really should not happen.
	}
	st := g.colors(opacity)
	// toGrad maps a pixel's center to where the geometry is.
	toGrad := Identity
	p := g.Points
	if g.Units == ObjectBoundingBox {
		box := Identity.Translate(g.Bounds.X, g.Bounds.Y).Scale(g.Bounds.W, g.Bounds.H)
		toGrad = objMatrix.Mult(box).Mult(g.Matrix).Invert()
	} else {
		p[0], p[1] = objMatrix.Transform(g.Matrix.Transform(p[0], p[1]))
		p[2], p[3] = objMatrix.Transform(g.Matrix.Transform(p[2], p[3]))
	}

	if g.IsRadial {
		cx, cy, fx, fy, rx, ry := p[0], p[1], p[2], p[3], p[4], p[4]
		if g.Units != ObjectBoundingBox {
			rx, ry = objMatrix.TransformVector(g.Matrix.TransformVector(rx, ry))
		}
		if cx == fx && cy == fy {
			return st.centered(toGrad, cx, cy, rx, ry)
		}
		fx /= rx
		fy /= ry
		cx /= rx
		cy /= ry

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
		return st.focal(toGrad, fx, fy, cx, cy, rx, ry)
	}
	return st.linear(toGrad, p[0], p[1], p[2], p[3])
}

// The paints below take their geometry as parameters, which their
// closures capture by value: a variable assigned after it is captured
// would be moved to the heap. Each maps a pixel's center by toGrad to
// where its geometry is; for Identity, exactly to itself.

// centered is the paint of a radial gradient whose focus is its center:
// t is just distance from center scaled by the bounds aspect ratio times
// r.
func (st gradStops) centered(toGrad Matrix2D, cx, cy, rx, ry float64) Paint {
	return FuncPaint(func(xi, yi int) color.RGBA64 {
		x, y := toGrad.Transform(float64(xi)+0.5, float64(yi)+0.5)
		dx := x - cx
		dy := y - cy
		return st.color(math.Sqrt(float64(dx*dx/(rx*rx)) + float64((dy*dy)/(ry*ry))))
	})
}

// focal is the paint of a radial gradient whose focus, (fx, fy), is not
// its center, (cx, cy), both divided by the radii, the circle then of
// radius 1. A point e is at t = |e-f| / |p-f| along the ray from the
// focus f through it to the circle at p = f + (e-f)/t: with g = f-c and
// d = e-f, |g + d/t| = 1, whose root past the focus, the focus inside, is
// t = (g·d + √((g·d)² + |d|²(1-|g|²))) / (1-|g|²).
func (st gradStops) focal(toGrad Matrix2D, fx, fy, cx, cy, rx, ry float64) Paint {
	gx, gy := fx-cx, fy-cy
	den := 1 - (float64(gx*gx) + float64(gy*gy))
	return FuncPaint(func(xi, yi int) color.RGBA64 {
		x, y := toGrad.Transform(float64(xi)+0.5, float64(yi)+0.5)
		dx, dy := x/rx-fx, y/ry-fy
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
