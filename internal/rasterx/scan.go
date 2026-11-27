// Package rasterx implements a rasterizer in go.
// ScannerGV renders images with golang.org/x/image/vector's line
// rasterization; see raster.go.
//
// Copyright 2018 All rights reserved.
// Created: 5/12/2018 by S.R.Wiley
package rasterx

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/math/fixed"
)

type (
	// SpanPainter blends a horizontal row span of pixels over pix using
	// coverage alphas in mask (0..0xffff) and an optional per-pixel alpha mask
	// row in alphaMask (0..0xff, or nil if unmasked). x0 and y are in paint
	// coordinates.
	SpanPainter interface {
		BlendSpan(pix []uint8, mask []uint32, alphaMask []uint8, x0, y int)
	}

	// Paint is what a path is drawn with: Span if non-nil, else Func's color at
	// each pixel, or where both are nil, Color at every one.
	Paint struct {
		Color color.RGBA64 // alpha-premultiplied, as color.Color.RGBA gives it
		Func  ColorFunc
		Span  SpanPainter
	}

	// ScannerGV rasterizes paths with golang.org/x/image/vector's line
	// rasterization, each in a window around it, so that a path costs
	// its extent, not the target's area, and draws them as vector's Draw
	// does. See raster.go.
	ScannerGV struct {
		Dest *image.RGBA
		// DestAlpha, when non-nil and Dest is nil, receives coverage directly
		// as single-channel alpha (used for clipPath masks).
		DestAlpha *image.Alpha
		// Targ is where the mask goes in Dest: its origin at Targ.Min,
		// as far as Targ.Max. Dest may hold only part of it.
		Targ image.Rectangle
		// Offset is added to a pixel's place in the mask to give the
		// place at which Paint.Func is asked its color.
		Offset image.Point
		// Touched is the part of Targ that a Draw changed or, with Dest
		// nil, the window it would draw in without rasterizing; callers
		// reset it.
		Touched image.Rectangle

		paint                  Paint
		clip                   image.Rectangle
		alphaMask              *image.Alpha
		covScale               uint32
		covSkip                bool
		evenOdd                bool
		size                   image.Point
		z                      rasterizer
		pts                    []pathPoint
		minX, minY, maxX, maxY fixed.Int26_6 // keep track of bounds
	}

	// pathPoint is a point a path moves or draws a line to.
	pathPoint struct {
		x, y float32
		move bool
	}
)

// ColorPaint is c at every pixel. It is generic so as not to box c.
func ColorPaint[C color.Color](c C) Paint {
	r, g, b, a := c.RGBA()
	return Paint{Color: color.RGBA64{uint16(r), uint16(g), uint16(b), uint16(a)}}
}

// FuncPaint is f's color at each pixel.
func FuncPaint(f ColorFunc) Paint {
	return Paint{Func: f}
}

// GetPathExtent returns the extent of the path
func (s *ScannerGV) GetPathExtent() fixed.Rectangle26_6 {
	return fixed.Rectangle26_6{Min: fixed.Point26_6{X: s.minX, Y: s.minY}, Max: fixed.Point26_6{X: s.maxX, Y: s.maxY}}
}

// SetWinding set the winding rule for the scanner
func (s *ScannerGV) SetWinding(useNonZeroWinding bool) {
	s.evenOdd = !useNonZeroWinding
}

// SetPaint sets what the scanner draws with.
func (s *ScannerGV) SetPaint(p Paint) {
	s.paint = p
}

// SetClip sets an optional clipping rectangle to restrict rendering only to
// that region -- if size is 0 then ignored (set to image.Rectangle{} to clear).
// It is in the coordinates Paint.Func is given.
func (s *ScannerGV) SetClip(rect image.Rectangle) {
	s.clip = rect
}

// SetAlphaMask sets an optional per-pixel alpha mask (in paint coordinates)
// that modulates every pixel drawn until cleared with SetAlphaMask(nil).
func (s *ScannerGV) SetAlphaMask(mask *image.Alpha) {
	s.alphaMask = mask
}

// SetCoverageScale scales subsequent Draw coverage by scale in [0, 1].
// Values >= 1 disable scaling; values <= 0 skip drawing.
func (s *ScannerGV) SetCoverageScale(scale float64) {
	if scale <= 0 || math.IsNaN(scale) {
		s.covSkip = true
		s.covScale = 0
		return
	}
	s.covSkip = false
	if scale >= 1 {
		s.covScale = 0
		return
	}
	v := uint32(math.Round(scale * 0xffff))
	if v == 0 {
		s.covSkip = true
		s.covScale = 0
		return
	}
	if v >= 0xffff {
		s.covScale = 0
		return
	}
	s.covScale = v
}

func (s *ScannerGV) set(a fixed.Point26_6) {
	if s.maxX < a.X {
		s.maxX = a.X
	}
	if s.maxY < a.Y {
		s.maxY = a.Y
	}
	if s.minX > a.X {
		s.minX = a.X
	}
	if s.minY > a.Y {
		s.minY = a.Y
	}
}

// Start starts a new path at the given point.
func (s *ScannerGV) Start(a fixed.Point26_6) {
	s.set(a)
	s.pts = append(s.pts, pathPoint{float32(a.X) / 64, float32(a.Y) / 64, true})
}

// Line adds a linear segment to the current curve.
func (s *ScannerGV) Line(b fixed.Point26_6) {
	s.set(b)
	s.pts = append(s.pts, pathPoint{float32(b.X) / 64, float32(b.Y) / 64, false})
}

// Draw renders the accumulate scan to the desination
func (s *ScannerGV) Draw() {
	if len(s.pts) == 0 || s.covSkip {
		return
	}
	r := s.window().Intersect(image.Rectangle{Max: s.Targ.Size()})
	if s.clip != (image.Rectangle{}) {
		r = r.Intersect(s.clip.Sub(s.Offset))
	}
	if s.alphaMask != nil {
		r = r.Intersect(s.alphaMask.Rect.Sub(s.Offset))
	}
	if s.Dest != nil {
		r = r.Intersect(s.Dest.Rect.Sub(s.Targ.Min))
	} else if s.DestAlpha != nil {
		r = r.Intersect(s.DestAlpha.Rect.Sub(s.Targ.Min))
	}
	if r.Empty() {
		return
	}
	s.Touched = s.Touched.Union(r.Add(s.Targ.Min))
	if s.Dest == nil {
		if s.DestAlpha != nil {
			s.rasterize(r)
			s.drawAlpha(s.DestAlpha)
		}
		return
	}
	s.rasterize(r)
	s.drawRGBA(s.Dest)
}

// window is the part of the mask the path can cover: its extent, a pixel
// further each side for the rounding of where its lines cross a row.
func (s *ScannerGV) window() image.Rectangle {
	// The points' own extent: rounding to float32 keeps the order, so
	// the least of the points is the least of the fixed ones, rounded.
	minX, minY := float32(s.minX)/64, float32(s.minY)/64
	maxX, maxY := float32(s.maxX)/64, float32(s.maxY)/64
	if !s.pts[0].move {
		// A line from the pen's start, at the origin.
		minX, minY, maxX, maxY = min(minX, 0), min(minY, 0), max(maxX, 0), max(maxY, 0)
	}
	clampTo := func(v float64, n int) int {
		return int(max(0, min(v, float64(n))))
	}
	return image.Rectangle{
		Min: image.Point{clampTo(math.Floor(float64(minX))-1, s.size.X), clampTo(math.Floor(float64(minY)), s.size.Y)},
		Max: image.Point{clampTo(math.Floor(float64(maxX))+2, s.size.X), clampTo(math.Ceil(float64(maxY)), s.size.Y)},
	}
}

// rasterize accumulates the path's cells in win.
func (s *ScannerGV) rasterize(win image.Rectangle) {
	z := &s.z
	z.reset(win, s.evenOdd)
	var penX, penY float32
	for _, p := range s.pts {
		if !p.move {
			z.lineTo(penX, penY, p.x, p.y)
		}
		penX, penY = p.x, p.y
	}
}

// drawRGBA draws the paint through the window's mask over dst, with the
// arithmetic of vector's Draw: its generic path, through color.RGBA64 and
// image.RGBA.Set, which for a premultiplied color is its fast path for a
// uniform one too. A pixel the mask does not cover is left alone, as both
// leave it.
func (s *ScannerGV) drawRGBA(dst *image.RGBA) {
	z := &s.z
	r := z.win
	span := s.paint.Span
	f := s.paint.Func
	am := s.alphaMask
	x0 := s.Offset.X + r.Min.X
	for y := r.Min.Y; y < r.Max.Y; y++ {
		mask := z.rowAlphas(y)
		if s.covScale != 0 {
			sc := s.covScale
			for i, ma := range mask {
				if ma != 0 {
					mask[i] = (ma*sc + 0x7fff) / 0xffff
				}
			}
		}
		pix := dst.Pix[dst.PixOffset(s.Targ.Min.X+r.Min.X, s.Targ.Min.Y+y):]
		var amRow []uint8
		if am != nil {
			off := am.PixOffset(x0, s.Offset.Y+y)
			amRow = am.Pix[off : off+len(mask) : off+len(mask)]
		}
		if span != nil {
			span.BlendSpan(pix, mask, amRow, x0, s.Offset.Y+y)
			continue
		}
		if f == nil {
			if amRow == nil {
				blend(pix, mask, s.paint.Color)
			} else {
				blendMasked(pix, mask, amRow, s.paint.Color)
			}
			continue
		}
		py := s.Offset.Y + y
		if amRow == nil {
			for i, ma := range mask {
				if ma != 0 {
					blendOne(pix[4*i:4*i+4:4*i+4], ma, f(x0+i, py))
				}
			}
		} else {
			for i, ma := range mask {
				if ma == 0 {
					continue
				}
				a := uint32(amRow[i])
				if a == 0 {
					continue
				}
				c := f(x0+i, py)
				if a < 255 {
					c = color.RGBA64{
						R: uint16(uint32(c.R) * a / 255),
						G: uint16(uint32(c.G) * a / 255),
						B: uint16(uint32(c.B) * a / 255),
						A: uint16(uint32(c.A) * a / 255),
					}
				}
				blendOne(pix[4*i:4*i+4:4*i+4], ma, c)
			}
		}
	}
}

// drawAlpha composites opaque coverage directly into a single-channel alpha
// image, matching opaque white drawn into an RGBA buffer bit for bit.
func (s *ScannerGV) drawAlpha(dst *image.Alpha) {
	z := &s.z
	r := z.win
	for y := r.Min.Y; y < r.Max.Y; y++ {
		mask := z.rowAlphas(y)
		if s.covScale != 0 {
			sc := s.covScale
			for i, ma := range mask {
				if ma != 0 {
					mask[i] = (ma*sc + 0x7fff) / 0xffff
				}
			}
		}
		pix := dst.Pix[dst.PixOffset(s.Targ.Min.X+r.Min.X, s.Targ.Min.Y+y):]
		for i, ma := range mask {
			if ma == 0 {
				continue
			}
			if ma == 0xffff {
				pix[i] = 0xff
				continue
			}
			d := uint32(pix[i])
			if d == 0 {
				pix[i] = uint8(ma >> 8)
				continue
			}
			a := 0xffff - ma
			pix[i] = uint8(uint16((d*0x101*a+0xffff*ma)/0xffff) >> 8)
		}
	}
}

func blendOne(q []uint8, ma uint32, c color.RGBA64) {
	sr, sg, sb, sa := uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A)
	if ma == 0xffff && sa == 0xffff {
		q[0], q[1], q[2], q[3] = uint8(sr>>8), uint8(sg>>8), uint8(sb>>8), 0xff
		return
	}
	a := 0xffff - (sa * ma / 0xffff)
	q[0] = uint8(uint16((uint32(q[0])*0x101*a+sr*ma)/0xffff) >> 8)
	q[1] = uint8(uint16((uint32(q[1])*0x101*a+sg*ma)/0xffff) >> 8)
	q[2] = uint8(uint16((uint32(q[2])*0x101*a+sb*ma)/0xffff) >> 8)
	q[3] = uint8(uint16((uint32(q[3])*0x101*a+sa*ma)/0xffff) >> 8)
}

// blend draws c through the mask's alphas over the pixels p, four bytes
// to an alpha.
func blend(p []uint8, mask []uint32, c color.RGBA64) {
	sr, sg, sb, sa := uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A)
	for i, ma := range mask {
		if ma == 0 {
			continue
		}
		q := p[4*i : 4*i+4 : 4*i+4]
		if ma == 0xffff && sa == 0xffff {
			q[0], q[1], q[2], q[3] = uint8(sr>>8), uint8(sg>>8), uint8(sb>>8), 0xff
			continue
		}
		a := 0xffff - (sa * ma / 0xffff)
		q[0] = uint8(uint16((uint32(q[0])*0x101*a+sr*ma)/0xffff) >> 8)
		q[1] = uint8(uint16((uint32(q[1])*0x101*a+sg*ma)/0xffff) >> 8)
		q[2] = uint8(uint16((uint32(q[2])*0x101*a+sb*ma)/0xffff) >> 8)
		q[3] = uint8(uint16((uint32(q[3])*0x101*a+sa*ma)/0xffff) >> 8)
	}
}

// blendMasked draws c through mask modulated by alphaMask over pixels p.
func blendMasked(p []uint8, mask []uint32, alphaMask []uint8, c color.RGBA64) {
	cr, cg, cb, ca := uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A)
	cr8, cg8, cb8 := uint8(cr>>8), uint8(cg>>8), uint8(cb>>8)
	for i, ma := range mask {
		if ma == 0 {
			continue
		}
		am := uint32(alphaMask[i])
		if am == 0 {
			continue
		}
		q := p[4*i : 4*i+4 : 4*i+4]
		if am == 255 {
			if ma == 0xffff && ca == 0xffff {
				q[0], q[1], q[2], q[3] = cr8, cg8, cb8, 0xff
				continue
			}
			a := 0xffff - (ca * ma / 0xffff)
			q[0] = uint8(uint16((uint32(q[0])*0x101*a+cr*ma)/0xffff) >> 8)
			q[1] = uint8(uint16((uint32(q[1])*0x101*a+cg*ma)/0xffff) >> 8)
			q[2] = uint8(uint16((uint32(q[2])*0x101*a+cb*ma)/0xffff) >> 8)
			q[3] = uint8(uint16((uint32(q[3])*0x101*a+ca*ma)/0xffff) >> 8)
			continue
		}
		sr := cr * am / 255
		sg := cg * am / 255
		sb := cb * am / 255
		sa := ca * am / 255
		a := 0xffff - (sa * ma / 0xffff)
		q[0] = uint8(uint16((uint32(q[0])*0x101*a+sr*ma)/0xffff) >> 8)
		q[1] = uint8(uint16((uint32(q[1])*0x101*a+sg*ma)/0xffff) >> 8)
		q[2] = uint8(uint16((uint32(q[2])*0x101*a+sb*ma)/0xffff) >> 8)
		q[3] = uint8(uint16((uint32(q[3])*0x101*a+sa*ma)/0xffff) >> 8)
	}
}

// Clear cancels any previous accumulated scans
func (s *ScannerGV) Clear() {
	s.pts = s.pts[:0]
	const mxfi = fixed.Int26_6(math.MaxInt32)
	s.minX, s.minY, s.maxX, s.maxY = mxfi, mxfi, -mxfi, -mxfi
}

// SetBounds sets the maximum width and height of the rasterized image. The
// width and height are in pixels, not fixed.Int26_6 units.
func (s *ScannerGV) SetBounds(width, height int) {
	s.size = image.Point{width, height}
}

// NewScannerGV creates a new Scanner with the given bounds.
func NewScannerGV(width, height int, dest *image.RGBA,
	targ image.Rectangle) *ScannerGV {
	s := new(ScannerGV)
	s.Clear()
	s.SetBounds(width, height)
	s.Dest = dest
	s.Targ = targ
	s.paint = ColorPaint(color.RGBA{255, 0, 0, 255})
	return s
}
