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

// unpremultiply is the 16-bit v premultiplied by a, not premultiplied,
// rounded so that an 8-bit color survives premultiplying.
func unpremultiply(v, a uint32) uint32 {
	return min((v*0xffff+a/2)/a, 0xffff)
}

type (
	// GradTable holds a gradient's decoded stops at a fixed opacity so it can
	// be cached across shapes and draws without per-draw allocations.
	GradTable struct {
		stops       []stop
		spread      SpreadMethod
		opacity     float64
		first, last color.RGBA64 // the first and last stops at opacity
		allOpaque   bool
		monotonic   bool
	}
	// stop is a stop's 16-bit color and alpha, not premultiplied, its
	// opacity, and that with its color's alpha in it, which blends.
	stop struct {
		offset  float64
		r, g, b uint32
		a       uint32
		rf      float64
		gf      float64
		bf      float64
		opacity float64
		blend   float64
	}

	gradSpanKind uint8

	// GradSpan is a reusable SpanPainter for linear and radial gradients.
	GradSpan struct {
		table  *GradTable
		kind   gradSpanKind
		toGrad Matrix2D
		p1x    float64
		p1y    float64
		dx     float64
		dy     float64
		d      float64
		cx, cy float64
		fx, fy float64
		r      float64
		gx, gy float64
		den    float64
	}
)

const (
	gradSpanLinear gradSpanKind = iota
	gradSpanRadialCentered
	gradSpanRadialFocal
)

// BuildGradTable decodes g's stops at opacity into a reusable GradTable.
func (g *Gradient) BuildGradTable(opacity float64) *GradTable {
	stops := make([]stop, len(g.Stops))
	allOpaque := opacity >= 1.0
	for i, s := range g.Stops {
		r, gr, b, a := nrgba64(s.StopColor)
		// Colors and opacities blend apart, not premultiplied, as SVG
		// has it; a stop color's own alpha is part of its opacity.
		op := max(0.0, min(s.Opacity, 1.0))
		if a != 0xffff || op < 1.0 {
			allOpaque = false
		}
		stops[i] = stop{
			offset:  s.Offset,
			r:       r,
			g:       gr,
			b:       b,
			a:       a,
			rf:      float64(r),
			gf:      float64(gr),
			bf:      float64(b),
			opacity: op,
			blend:   op * (float64(a) / 0xffff),
		}
	}
	monotonic := true
	for i := 1; i < len(stops); i++ {
		if stops[i].offset < stops[i-1].offset {
			monotonic = false
			break
		}
	}
	st := &GradTable{stops: stops, spread: g.Spread, opacity: opacity, allOpaque: allOpaque, monotonic: monotonic}
	if len(stops) > 0 {
		st.first, st.last = st.at(&stops[0]), st.at(&stops[len(stops)-1])
	}
	return st
}

// at is s's color at the paint's opacity.
func (st *GradTable) at(s *stop) color.RGBA64 {
	return ColorPaint(applyOpacity64(s.r, s.g, s.b, s.a, s.opacity*st.opacity)).Color
}

// colorAt evaluates st at t, updating *seg to speed up subsequent monotonic lookups.
func (st *GradTable) colorAt(t float64, seg *int) color.RGBA64 {
	switch st.spread {
	case RepeatSpread:
		t -= math.Floor(t)
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
	s := st.stops
	if !(t > s[0].offset) {
		return st.first
	}
	n := len(s)
	var place int
	if st.monotonic {
		if t > s[n-1].offset {
			return st.last
		}
		place = *seg
		if place < 1 || place >= n {
			place = 1
		}
		for place > 1 && !(t > s[place-1].offset) {
			place--
		}
		for place < n && t > s[place].offset {
			place++
		}
		if place == n {
			return st.last
		}
		*seg = place
	} else {
		place = 1
		for place != n && t > s[place].offset {
			place++
		}
		if place == n {
			return st.last
		}
	}
	s1, s2 := &s[place-1], &s[place]
	tp := (t - s1.offset) / (s2.offset - s1.offset)
	r8 := uint8((float64(float64(s1.r)*(1-tp)) + float64(float64(s2.r)*tp)) / 256)
	g8 := uint8((float64(float64(s1.g)*(1-tp)) + float64(float64(s2.g)*tp)) / 256)
	b8 := uint8((float64(float64(s1.b)*(1-tp)) + float64(float64(s2.b)*tp)) / 256)
	if st.allOpaque {
		return color.RGBA64{
			R: uint16(r8) * 0x101,
			G: uint16(g8) * 0x101,
			B: uint16(b8) * 0x101,
			A: 0xffff,
		}
	}
	op := max(0, min((float64(s1.blend*(1-tp))+float64(s2.blend*tp))*st.opacity, 1))
	a8 := uint8(op * 0xff)
	a16 := uint32(a8) * 0x101
	return color.RGBA64{
		R: uint16((uint32(r8) * 0x101 * a16) / 0xffff),
		G: uint16((uint32(g8) * 0x101 * a16) / 0xffff),
		B: uint16((uint32(b8) * 0x101 * a16) / 0xffff),
		A: uint16(a16),
	}
}

// BlendSpan implements SpanPainter for linear and radial gradients, evaluating
// the exact per-pixel projection and stop interpolation while avoiding closures,
// per-pixel blending calls, and redundant stop searches.
func (sp *GradSpan) BlendSpan(pix []uint8, mask []uint32, alphaMask []uint8, x0, y int) {
	st := sp.table
	toGrad := sp.toGrad
	fy := float64(y) + 0.5
	fyC := float64(fy * toGrad.C)
	fyD := float64(fy * toGrad.D)
	stops := st.stops
	nStops := len(stops)
	seg := 1
	allOpaque := st.allOpaque && st.monotonic
	spread := st.spread
	firstR8, firstG8, firstB8 := uint8(st.first.R>>8), uint8(st.first.G>>8), uint8(st.first.B>>8)
	lastR8, lastG8, lastB8 := uint8(st.last.R>>8), uint8(st.last.G>>8), uint8(st.last.B>>8)
	s0Off := stops[0].offset
	sLastOff := stops[nStops-1].offset

	switch sp.kind {
	case gradSpanLinear:
		dx, dy, d := sp.dx, sp.dy, sp.d
		p1x, p1y := sp.p1x, sp.p1y
		if alphaMask == nil && allOpaque {
			s1, s2 := &stops[0], &stops[1]
			off1, off2, span := s1.offset, s2.offset, s2.offset-s1.offset
			r1, g1, b1 := s1.rf, s1.gf, s1.bf
			r2, g2, b2 := s2.rf, s2.gf, s2.bf
			for i, ma := range mask {
				if ma == 0 {
					continue
				}
				fx := float64(x0+i) + 0.5
				gx := (float64(fx*toGrad.A) + fyC) + toGrad.E
				gy := (float64(fx*toGrad.B) + fyD) + toGrad.F
				t := (float64(dx*(gx-p1x)) + float64(dy*(gy-p1y))) / d
				var r8, g8, b8 uint8
				switch spread {
				case RepeatSpread:
					t -= math.Floor(t)
				case ReflectSpread:
					if t -= float64(2 * math.Floor(t/2)); t > 1 {
						t = 2 - t
					}
				default:
					if t >= 1 {
						r8, g8, b8 = lastR8, lastG8, lastB8
						goto writeLinear
					}
					if t <= 0 {
						r8, g8, b8 = firstR8, firstG8, firstB8
						goto writeLinear
					}
				}
				if !(t > s0Off) {
					r8, g8, b8 = firstR8, firstG8, firstB8
				} else if t > sLastOff {
					r8, g8, b8 = lastR8, lastG8, lastB8
				} else {
					if !(t > off1) || t > off2 {
						for seg > 1 && !(t > stops[seg-1].offset) {
							seg--
						}
						for seg < nStops-1 && t > stops[seg].offset {
							seg++
						}
						s1, s2 = &stops[seg-1], &stops[seg]
						off1, off2, span = s1.offset, s2.offset, s2.offset-s1.offset
						r1, g1, b1 = s1.rf, s1.gf, s1.bf
						r2, g2, b2 = s2.rf, s2.gf, s2.bf
					}
					tp := (t - off1) / span
					om := 1 - tp
					r8 = uint8((float64(r1*om) + float64(r2*tp)) / 256)
					g8 = uint8((float64(g1*om) + float64(g2*tp)) / 256)
					b8 = uint8((float64(b1*om) + float64(b2*tp)) / 256)
				}
			writeLinear:
				q := pix[4*i : 4*i+4 : 4*i+4]
				if ma == 0xffff {
					q[0], q[1], q[2], q[3] = r8, g8, b8, 0xff
					continue
				}
				a := 0xffff - ma
				q[0] = uint8(uint16((uint32(q[0])*0x101*a+uint32(r8)*0x101*ma)/0xffff) >> 8)
				q[1] = uint8(uint16((uint32(q[1])*0x101*a+uint32(g8)*0x101*ma)/0xffff) >> 8)
				q[2] = uint8(uint16((uint32(q[2])*0x101*a+uint32(b8)*0x101*ma)/0xffff) >> 8)
				q[3] = uint8(uint16((uint32(q[3])*0x101*a+0xffff*ma)/0xffff) >> 8)
			}
			return
		}
		for i, ma := range mask {
			if ma == 0 {
				continue
			}
			am := uint32(255)
			if alphaMask != nil {
				am = uint32(alphaMask[i])
				if am == 0 {
					continue
				}
			}
			fx := float64(x0+i) + 0.5
			gx := (float64(fx*toGrad.A) + fyC) + toGrad.E
			gy := (float64(fx*toGrad.B) + fyD) + toGrad.F
			t := (float64(dx*(gx-p1x)) + float64(dy*(gy-p1y))) / d
			c := st.colorAt(t, &seg)
			if am < 255 {
				c = color.RGBA64{
					R: uint16(uint32(c.R) * am / 255),
					G: uint16(uint32(c.G) * am / 255),
					B: uint16(uint32(c.B) * am / 255),
					A: uint16(uint32(c.A) * am / 255),
				}
			}
			blendOne(pix[4*i:4*i+4:4*i+4], ma, c)
		}

	case gradSpanRadialCentered:
		cx, cy := sp.cx, sp.cy
		r2 := sp.r * sp.r
		if alphaMask == nil && allOpaque {
			s1, s2 := &stops[0], &stops[1]
			off1, off2, span := s1.offset, s2.offset, s2.offset-s1.offset
			r1, g1, b1 := s1.rf, s1.gf, s1.bf
			r2c, g2c, b2c := s2.rf, s2.gf, s2.bf
			for i, ma := range mask {
				if ma == 0 {
					continue
				}
				fx := float64(x0+i) + 0.5
				dx := ((float64(fx*toGrad.A) + fyC) + toGrad.E) - cx
				dy := ((float64(fx*toGrad.B) + fyD) + toGrad.F) - cy
				t := math.Sqrt(float64(dx*dx/r2) + float64((dy*dy)/r2))
				var r8, g8, b8 uint8
				switch spread {
				case RepeatSpread:
					t -= math.Floor(t)
				case ReflectSpread:
					if t -= float64(2 * math.Floor(t/2)); t > 1 {
						t = 2 - t
					}
				default:
					if t >= 1 {
						r8, g8, b8 = lastR8, lastG8, lastB8
						goto writeCentered
					}
					if t <= 0 {
						r8, g8, b8 = firstR8, firstG8, firstB8
						goto writeCentered
					}
				}
				if !(t > s0Off) {
					r8, g8, b8 = firstR8, firstG8, firstB8
				} else if t > sLastOff {
					r8, g8, b8 = lastR8, lastG8, lastB8
				} else {
					if !(t > off1) || t > off2 {
						for seg > 1 && !(t > stops[seg-1].offset) {
							seg--
						}
						for seg < nStops-1 && t > stops[seg].offset {
							seg++
						}
						s1, s2 = &stops[seg-1], &stops[seg]
						off1, off2, span = s1.offset, s2.offset, s2.offset-s1.offset
						r1, g1, b1 = s1.rf, s1.gf, s1.bf
						r2c, g2c, b2c = s2.rf, s2.gf, s2.bf
					}
					tp := (t - off1) / span
					om := 1 - tp
					r8 = uint8((float64(r1*om) + float64(r2c*tp)) / 256)
					g8 = uint8((float64(g1*om) + float64(g2c*tp)) / 256)
					b8 = uint8((float64(b1*om) + float64(b2c*tp)) / 256)
				}
			writeCentered:
				q := pix[4*i : 4*i+4 : 4*i+4]
				if ma == 0xffff {
					q[0], q[1], q[2], q[3] = r8, g8, b8, 0xff
					continue
				}
				a := 0xffff - ma
				q[0] = uint8(uint16((uint32(q[0])*0x101*a+uint32(r8)*0x101*ma)/0xffff) >> 8)
				q[1] = uint8(uint16((uint32(q[1])*0x101*a+uint32(g8)*0x101*ma)/0xffff) >> 8)
				q[2] = uint8(uint16((uint32(q[2])*0x101*a+uint32(b8)*0x101*ma)/0xffff) >> 8)
				q[3] = uint8(uint16((uint32(q[3])*0x101*a+0xffff*ma)/0xffff) >> 8)
			}
			return
		}
		for i, ma := range mask {
			if ma == 0 {
				continue
			}
			am := uint32(255)
			if alphaMask != nil {
				am = uint32(alphaMask[i])
				if am == 0 {
					continue
				}
			}
			fx := float64(x0+i) + 0.5
			dx := ((float64(fx*toGrad.A) + fyC) + toGrad.E) - cx
			dy := ((float64(fx*toGrad.B) + fyD) + toGrad.F) - cy
			t := math.Sqrt(float64(dx*dx/r2) + float64((dy*dy)/r2))
			c := st.colorAt(t, &seg)
			if am < 255 {
				c = color.RGBA64{
					R: uint16(uint32(c.R) * am / 255),
					G: uint16(uint32(c.G) * am / 255),
					B: uint16(uint32(c.B) * am / 255),
					A: uint16(uint32(c.A) * am / 255),
				}
			}
			blendOne(pix[4*i:4*i+4:4*i+4], ma, c)
		}

	case gradSpanRadialFocal:
		fxp, fyp, r := sp.fx, sp.fy, sp.r
		gx, gy, den := sp.gx, sp.gy, sp.den
		if alphaMask == nil && allOpaque {
			s1, s2 := &stops[0], &stops[1]
			off1, off2, span := s1.offset, s2.offset, s2.offset-s1.offset
			r1, g1, b1 := s1.rf, s1.gf, s1.bf
			r2, g2, b2 := s2.rf, s2.gf, s2.bf
			for i, ma := range mask {
				if ma == 0 {
					continue
				}
				fx := float64(x0+i) + 0.5
				px := (float64(fx*toGrad.A) + fyC) + toGrad.E
				py := (float64(fx*toGrad.B) + fyD) + toGrad.F
				dx, dy := px/r-fxp, py/r-fyp
				gd := float64(gx*dx) + float64(gy*dy)
				dd := float64(dx*dx) + float64(dy*dy)
				t := (gd + math.Sqrt(float64(gd*gd)+float64(dd*den))) / den
				var r8, g8, b8 uint8
				if dd < float64(epsilonF*t)*t {
					r8, g8, b8 = lastR8, lastG8, lastB8
				} else {
					switch spread {
					case RepeatSpread:
						t -= math.Floor(t)
					case ReflectSpread:
						if t -= float64(2 * math.Floor(t/2)); t > 1 {
							t = 2 - t
						}
					default:
						if t >= 1 {
							r8, g8, b8 = lastR8, lastG8, lastB8
							goto writeFocal
						}
						if t <= 0 {
							r8, g8, b8 = firstR8, firstG8, firstB8
							goto writeFocal
						}
					}
					if !(t > s0Off) {
						r8, g8, b8 = firstR8, firstG8, firstB8
					} else if t > sLastOff {
						r8, g8, b8 = lastR8, lastG8, lastB8
					} else {
						if !(t > off1) || t > off2 {
							for seg > 1 && !(t > stops[seg-1].offset) {
								seg--
							}
							for seg < nStops-1 && t > stops[seg].offset {
								seg++
							}
							s1, s2 = &stops[seg-1], &stops[seg]
							off1, off2, span = s1.offset, s2.offset, s2.offset-s1.offset
							r1, g1, b1 = s1.rf, s1.gf, s1.bf
							r2, g2, b2 = s2.rf, s2.gf, s2.bf
						}
						tp := (t - off1) / span
						om := 1 - tp
						r8 = uint8((float64(r1*om) + float64(r2*tp)) / 256)
						g8 = uint8((float64(g1*om) + float64(g2*tp)) / 256)
						b8 = uint8((float64(b1*om) + float64(b2*tp)) / 256)
					}
				}
			writeFocal:
				q := pix[4*i : 4*i+4 : 4*i+4]
				if ma == 0xffff {
					q[0], q[1], q[2], q[3] = r8, g8, b8, 0xff
					continue
				}
				a := 0xffff - ma
				q[0] = uint8(uint16((uint32(q[0])*0x101*a+uint32(r8)*0x101*ma)/0xffff) >> 8)
				q[1] = uint8(uint16((uint32(q[1])*0x101*a+uint32(g8)*0x101*ma)/0xffff) >> 8)
				q[2] = uint8(uint16((uint32(q[2])*0x101*a+uint32(b8)*0x101*ma)/0xffff) >> 8)
				q[3] = uint8(uint16((uint32(q[3])*0x101*a+0xffff*ma)/0xffff) >> 8)
			}
			return
		}
		for i, ma := range mask {
			if ma == 0 {
				continue
			}
			am := uint32(255)
			if alphaMask != nil {
				am = uint32(alphaMask[i])
				if am == 0 {
					continue
				}
			}
			fx := float64(x0+i) + 0.5
			px := (float64(fx*toGrad.A) + fyC) + toGrad.E
			py := (float64(fx*toGrad.B) + fyD) + toGrad.F
			dx, dy := px/r-fxp, py/r-fyp
			gd := float64(gx*dx) + float64(gy*dy)
			dd := float64(dx*dx) + float64(dy*dy)
			t := (gd + math.Sqrt(float64(gd*gd)+float64(dd*den))) / den
			var c color.RGBA64
			if dd < float64(epsilonF*t)*t {
				c = st.last
			} else {
				c = st.colorAt(t, &seg)
			}
			if am < 255 {
				c = color.RGBA64{
					R: uint16(uint32(c.R) * am / 255),
					G: uint16(uint32(c.G) * am / 255),
					B: uint16(uint32(c.B) * am / 255),
					A: uint16(uint32(c.A) * am / 255),
				}
			}
			blendOne(pix[4*i:4*i+4:4*i+4], ma, c)
		}
	}
}

// At evaluates the gradient color at pixel (xi, yi).
func (sp *GradSpan) At(xi, yi int) color.RGBA64 {
	x, y := sp.toGrad.Transform(float64(xi)+0.5, float64(yi)+0.5)
	seg := 1
	switch sp.kind {
	case gradSpanLinear:
		dfx := x - sp.p1x
		dfy := y - sp.p1y
		return sp.table.colorAt((float64(sp.dx*dfx)+float64(sp.dy*dfy))/sp.d, &seg)
	case gradSpanRadialCentered:
		dx := x - sp.cx
		dy := y - sp.cy
		r2 := sp.r * sp.r
		return sp.table.colorAt(math.Sqrt(float64(dx*dx/r2)+float64((dy*dy)/r2)), &seg)
	case gradSpanRadialFocal:
		dx, dy := x/sp.r-sp.fx, y/sp.r-sp.fy
		gd := float64(sp.gx*dx) + float64(sp.gy*dy)
		dd := float64(dx*dx) + float64(dy*dy)
		t := (gd + math.Sqrt(float64(gd*gd)+float64(dd*sp.den))) / sp.den
		if dd < float64(epsilonF*t)*t {
			return sp.table.last
		}
		return sp.table.colorAt(t, &seg)
	}
	return color.RGBA64{}
}

// GetColorFunction returns the color function
func (g *Gradient) GetColorFunction(opacity float64) Paint {
	return g.GetColorFunctionUS(opacity, Identity)
}

// GetColorFunctionUS returns the paint for g at opacity under objMatrix.
func (g *Gradient) GetColorFunctionUS(opacity float64, objMatrix Matrix2D) Paint {
	sp := new(GradSpan)
	p := g.FillSpan(sp, nil, opacity, objMatrix)
	if p.Span != nil {
		p.Func = sp.At
	}
	return p
}

// FillSpan populates dst with a span paint for g at opacity under objMatrix,
// reusing table when non-nil to avoid per-draw allocations.
func (g *Gradient) FillSpan(dst *GradSpan, table *GradTable, opacity float64, objMatrix Matrix2D) Paint {
	switch len(g.Stops) {
	case 0:
		// SVG 1.1 §13.2.4: a gradient with no stops is rendered as if none were specified.
		return Paint{}
	case 1:
		op := max(0.0, min(g.Stops[0].Opacity, 1.0)) * opacity
		return ColorPaint(ApplyOpacity(g.Stops[0].StopColor, op))
	}
	if table == nil {
		table = g.BuildGradTable(opacity)
	}
	toUser := objMatrix
	if g.Units == ObjectBoundingBox {
		toUser = toUser.Mult(Identity.Translate(g.Bounds.X, g.Bounds.Y).Scale(g.Bounds.W, g.Bounds.H))
	}
	// toGrad maps a pixel's center to where the geometry is.
	toGrad := toUser.Mult(g.Matrix).Invert()
	p := g.Points

	if g.IsRadial {
		cx, cy, fx, fy, r := p[0], p[1], p[2], p[3], p[4]
		if r < 0 {
			return Paint{}
		}
		if r == 0 {
			// SVG 1.1 §13.2.3: r == 0 paints a single color using the final stop.
			return ColorPaint(table.last)
		}
		if cx == fx && cy == fy {
			*dst = GradSpan{
				table:  table,
				kind:   gradSpanRadialCentered,
				toGrad: toGrad,
				cx:     cx,
				cy:     cy,
				r:      r,
			}
			return Paint{Span: dst}
		}
		fx /= r
		fy /= r
		cx /= r
		cy /= r

		dfx := fx - cx
		dfy := fy - cy

		const maxFocus = 1.0 - epsilonF
		if float64(dfx*dfx)+float64(dfy*dfy) >= float64(maxFocus*maxFocus) { // Focus on or outside circle; pull inside as per SVG specs.
			nfx, nfy, intersects := RayCircleIntersectionF(fx, fy, cx, cy, cx, cy, maxFocus)
			fx, fy = nfx, nfy
			if !intersects {
				return ColorPaint(table.last)
			}
		}
		gx, gy := fx-cx, fy-cy
		den := 1 - (float64(gx*gx) + float64(gy*gy))
		*dst = GradSpan{
			table:  table,
			kind:   gradSpanRadialFocal,
			toGrad: toGrad,
			fx:     fx,
			fy:     fy,
			r:      r,
			gx:     gx,
			gy:     gy,
			den:    den,
		}
		return Paint{Span: dst}
	}

	dx := p[2] - p[0]
	dy := p[3] - p[1]
	d := float64(dx*dx) + float64(dy*dy)
	if d == 0 {
		// SVG 1.1 §13.2.2: coincident endpoints paint the final stop color.
		return ColorPaint(table.last)
	}
	*dst = GradSpan{
		table:  table,
		kind:   gradSpanLinear,
		toGrad: toGrad,
		p1x:    p[0],
		p1y:    p[1],
		dx:     dx,
		dy:     dy,
		d:      d,
	}
	return Paint{Span: dst}
}
