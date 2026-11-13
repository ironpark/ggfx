// Package rasterx implements a rasterizer in go.
// By default rasterx uses ScannerGV to render images
// which uses the rasterizer in the golang.org/x/image/vector package.
// The freetype rasterizer under the GNU license can also be used, by
// downloading the scanFT package.
//
// Copyright 2018 All rights reserved.
// Created: 5/12/2018 by S.R.Wiley
package rasterx

import (
	"image"
	"image/color"
	"image/draw"
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

	// ScannerGV rasterizes paths as golang.org/x/image/vector's
	// Rasterizer, sized to the target, would, and draws them as its Draw
	// would, byte for byte; but each path costs its extent, not the
	// target's area. See raster.go.
	ScannerGV struct {
		Dest draw.Image
		// Targ is where the mask goes in Dest: its origin at Targ.Min,
		// as far as Targ.Max. Dest may hold only part of it.
		Targ image.Rectangle
		// Offset is added to a pixel's place in the mask to give the
		// place at which Paint.Func is asked its color.
		Offset image.Point
		// Touched is the part of Targ that a Draw changed or, with Dest
		// nil, would have; callers reset it.
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
	if s.size.X <= 0 || s.size.Y <= 0 || len(s.pts) == 0 {
		return
	}
	win := s.window()
	if !s.rasterize(win) {
		// The coverage's rounding errors outside the window show, or a
		// line strayed from it: do as vector does, all over.
		win = image.Rectangle{Max: s.size}
		s.rasterize(win)
	}
	r := win.Intersect(image.Rectangle{Max: s.Targ.Size()})
	if s.clip != image.ZR {
		r = r.Intersect(s.clip.Sub(s.Offset))
	}
	if r.Empty() {
		return
	}
	s.Touched = s.Touched.Union(r.Add(s.Targ.Min))
	if s.Dest == nil {
		return
	}
	r = r.Intersect(s.Dest.Bounds().Sub(s.Targ.Min))
	if dst, ok := s.Dest.(*image.RGBA); ok {
		s.drawRGBA(dst, r)
	} else {
		s.drawImage(r)
	}
}

// window is the part of the mask the path can cover: its extent, and as
// far around it as vector's lines can stray in rounding.
func (s *ScannerGV) window() image.Rectangle {
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	if !s.pts[0].move {
		minX, minY, maxX, maxY = 0, 0, 0, 0 // a line from the pen's start
	}
	for _, p := range s.pts {
		minX, maxX = min(minX, p.x), max(maxX, p.x)
		minY, maxY = min(minY, p.y), max(maxY, p.y)
	}
	clampTo := func(v float64, n int) int {
		return int(max(0, min(v, float64(n))))
	}
	r := image.Rect(0, 0, 0, 0)
	r.Min.X = clampTo(math.Floor(float64(minX))-2, s.size.X)
	r.Max.X = clampTo(math.Ceil(float64(maxX))+2, s.size.X)
	r.Min.Y = clampTo(math.Floor(float64(minY))-1, s.size.Y)
	r.Max.Y = clampTo(math.Ceil(float64(maxY))+1, s.size.Y)
	if r.Max.X == 0 {
		// Left of the mask, the path covers its column 0.
		r.Max.X = 1
	}
	return r
}

// rasterize accumulates the path's mask in win, reporting false if win
// does not hold all of it that shows.
func (s *ScannerGV) rasterize(win image.Rectangle) bool {
	z := &s.z
	z.reset(s.size, win, s.evenOdd)
	var penX, penY float32
	for _, p := range s.pts {
		if !p.move {
			z.lineTo(penX, penY, p.x, p.y)
		}
		penX, penY = p.x, p.y
	}
	if z.stray {
		return false
	}
	return z.accumulate()
}

// drawRGBA draws the paint through the mask in r over dst, with the
// arithmetic vector's Draw uses: its fast path for a uniform color, and
// its generic one, through color.RGBA64 and image.RGBA.Set, otherwise. A
// pixel the mask does not cover is left alone, as both leave it.
func (s *ScannerGV) drawRGBA(dst *image.RGBA, r image.Rectangle) {
	z := &s.z
	w := z.win.Dx()
	p := s.paint
	uniform := p.Func == nil && s.clip == image.ZR
	sr, sg, sb, sa := uint32(p.Color.R), uint32(p.Color.G), uint32(p.Color.B), uint32(p.Color.A)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		mask := z.u32[(y-z.win.Min.Y)*w:]
		pix := dst.Pix[dst.PixOffset(s.Targ.Min.X+r.Min.X, s.Targ.Min.Y+y):]
		for x := r.Min.X; x < r.Max.X; x++ {
			ma := mask[x-z.win.Min.X]
			if ma == 0 {
				continue
			}
			i := 4 * (x - r.Min.X)
			if uniform {
				if ma == 0xffff && sa == 0xffff {
					// Opaque over all: what both paths come to.
					pix[i+0], pix[i+1], pix[i+2], pix[i+3] = uint8(sr>>8), uint8(sg>>8), uint8(sb>>8), 0xff
					continue
				}
				a := 0xffff - (sa * ma / 0xffff)
				pix[i+0] = uint8(((uint32(pix[i+0])*0x101*a + sr*ma) / 0xffff) >> 8)
				pix[i+1] = uint8(((uint32(pix[i+1])*0x101*a + sg*ma) / 0xffff) >> 8)
				pix[i+2] = uint8(((uint32(pix[i+2])*0x101*a + sb*ma) / 0xffff) >> 8)
				pix[i+3] = uint8(((uint32(pix[i+3])*0x101*a + sa*ma) / 0xffff) >> 8)
				continue
			}
			if p.Func != nil {
				c := p.Func(s.Offset.X+x, s.Offset.Y+y)
				sr, sg, sb, sa = uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A)
			}
			if ma == 0xffff && sa == 0xffff {
				pix[i+0], pix[i+1], pix[i+2], pix[i+3] = uint8(sr>>8), uint8(sg>>8), uint8(sb>>8), 0xff
				continue
			}
			a := 0xffff - (sa * ma / 0xffff)
			pix[i+0] = uint8(uint16((uint32(pix[i+0])*0x101*a+sr*ma)/0xffff) >> 8)
			pix[i+1] = uint8(uint16((uint32(pix[i+1])*0x101*a+sg*ma)/0xffff) >> 8)
			pix[i+2] = uint8(uint16((uint32(pix[i+2])*0x101*a+sb*ma)/0xffff) >> 8)
			pix[i+3] = uint8(uint16((uint32(pix[i+3])*0x101*a+sa*ma)/0xffff) >> 8)
		}
	}
}

// drawImage is drawRGBA for any other Dest, as vector's generic path.
func (s *ScannerGV) drawImage(r image.Rectangle) {
	z := &s.z
	w := z.win.Dx()
	p := s.paint
	sr, sg, sb, sa := uint32(p.Color.R), uint32(p.Color.G), uint32(p.Color.B), uint32(p.Color.A)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		mask := z.u32[(y-z.win.Min.Y)*w:]
		for x := r.Min.X; x < r.Max.X; x++ {
			ma := mask[x-z.win.Min.X]
			if ma == 0 {
				continue
			}
			if p.Func != nil {
				c := p.Func(s.Offset.X+x, s.Offset.Y+y)
				sr, sg, sb, sa = uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A)
			}
			dx, dy := s.Targ.Min.X+x, s.Targ.Min.Y+y
			dr, dg, db, da := s.Dest.At(dx, dy).RGBA()
			a := 0xffff - (sa * ma / 0xffff)
			s.Dest.Set(dx, dy, color.RGBA64{
				uint16((dr*a + sr*ma) / 0xffff),
				uint16((dg*a + sg*ma) / 0xffff),
				uint16((db*a + sb*ma) / 0xffff),
				uint16((da*a + sa*ma) / 0xffff),
			})
		}
	}
}

// Clear cancels any previous accumulated scans
func (s *ScannerGV) Clear() {
	s.pts = s.pts[:0]
	const mxfi = fixed.Int26_6(math.MaxInt32)
	s.minX, s.minY, s.maxX, s.maxY = mxfi, mxfi, -mxfi, -mxfi
}

// SetBounds sets the maximum width and height of the rasterized image and
// calls Clear. The width and height are in pixels, not fixed.Int26_6 units.
func (s *ScannerGV) SetBounds(width, height int) {
	s.size = image.Point{width, height}
}

// NewScannerGV creates a new Scanner with the given bounds.
func NewScannerGV(width, height int, dest draw.Image,
	targ image.Rectangle) *ScannerGV {
	s := new(ScannerGV)
	s.SetBounds(width, height)
	s.Dest = dest
	s.Targ = targ
	s.paint = ColorPaint(color.RGBA{255, 0, 0, 255})
	return s
}
