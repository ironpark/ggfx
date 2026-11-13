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
	// Paint is what a path is drawn with: Func's color at each pixel, or
	// where Func is nil, Color at every one.
	Paint struct {
		Color color.RGBA64 // alpha-premultiplied, as color.Color.RGBA gives it
		Func  ColorFunc
	}

	// ScannerGV rasterizes paths with golang.org/x/image/vector's line
	// rasterization, each in a window around it, so that a path costs
	// its extent, not the target's area, and draws them as vector's Draw
	// does. See raster.go.
	ScannerGV struct {
		Dest *image.RGBA
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
// that region -- if size is 0 then ignored (set to image.ZR to clear). It
// is in the coordinates Paint.Func is given.
func (s *ScannerGV) SetClip(rect image.Rectangle) {
	s.clip = rect
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
	if len(s.pts) == 0 {
		return
	}
	r := s.window().Intersect(image.Rectangle{Max: s.Targ.Size()})
	if s.clip != image.ZR {
		r = r.Intersect(s.clip.Sub(s.Offset))
	}
	if s.Dest != nil {
		r = r.Intersect(s.Dest.Rect.Sub(s.Targ.Min))
	}
	if r.Empty() {
		return
	}
	s.Touched = s.Touched.Union(r.Add(s.Targ.Min))
	if s.Dest == nil {
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
	z.reset(s.size, win, s.evenOdd)
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
	f := s.paint.Func
	for y := r.Min.Y; y < r.Max.Y; y++ {
		mask := z.rowAlphas(y)
		pix := dst.Pix[dst.PixOffset(s.Targ.Min.X+r.Min.X, s.Targ.Min.Y+y):]
		if f == nil {
			blend(pix, mask, s.paint.Color)
			continue
		}
		for i, ma := range mask {
			if ma != 0 {
				blend(pix[4*i:], mask[i:i+1], f(s.Offset.X+r.Min.X+i, s.Offset.Y+y))
			}
		}
	}
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
