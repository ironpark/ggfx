// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package rasterx

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"testing"

	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// Drawing a premultiplied color through a mask under faint, as vector's
// Draw does, leaves every byte of an RGBA pixel as it is. The result
// depends on the source only through k, sa*ma/0xffff, and through s*ma,
// which it grows with, so the extremes of s*ma for each k cover all
// sources.
func TestFaintMaskChangesNothing(t *testing.T) {
	for v := uint32(0); v < 0x100; v++ {
		d := v * 0x101
		for ma := uint32(1); ma < faint; ma++ {
			for k := uint32(0); k <= ma; k++ {
				// sa < (k+1)*0xffff/ma, and s <= sa.
				maxSM := min(0xffff*ma, (k+1)*0xffff-1)
				for _, sm := range []uint32{0, maxSM} {
					out := uint8(uint16((d*(0xffff-k)+sm)/0xffff) >> 8)
					if uint32(out) != v {
						t.Fatalf("byte %#x, mask %#x, k %d, s*ma %d: drew %#x", v, ma, k, sm, out)
					}
				}
			}
		}
	}
}

// The scanner draws what vector's Rasterizer, sized to the target, draws.
func TestScannerDrawsAsVector(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	sizes := []image.Point{{1, 1}, {3, 5}, {64, 48}, {512, 160}, {120, 512}, {513, 40}, {37, 513}, {700, 90}}
	windowed, fallbacks := 0, 0
	for _, size := range sizes {
		for n := 0; n < 24; n++ {
			pts := randomPath(rnd, size, n%8)
			paints := []Paint{
				ColorPaint(color.NRGBA{uint8(rnd.Intn(256)), uint8(rnd.Intn(256)), uint8(rnd.Intn(256)), uint8(rnd.Intn(256))}),
				FuncPaint(func(x, y int) color.RGBA64 {
					a := uint16(x*7+y*13) | 0x00ff
					return color.RGBA64{a / 3, a / 2, a, a}
				}),
			}
			for pi, paint := range paints {
				name := fmt.Sprintf("%v/%d/%d", size, n, pi)
				bg := randomImage(rnd, size)
				want, got := cloneRGBA(bg), cloneRGBA(bg)

				z := vector.NewRasterizer(size.X, size.Y)
				for _, p := range pts {
					if p.move {
						z.MoveTo(p.x, p.y)
					} else {
						z.LineTo(p.x, p.y)
					}
				}
				var src image.Image = image.NewUniform(paint.Color)
				if paint.Func != nil {
					src = funcImage(paint.Func)
				}
				z.Draw(want, want.Bounds(), src, image.Point{})

				s := NewScannerGV(size.X, size.Y, got, got.Bounds())
				s.Clear()
				s.SetPaint(paint)
				for _, p := range pts {
					fp := fixed.Point26_6{X: fixed.Int26_6(p.x * 64), Y: fixed.Int26_6(p.y * 64)}
					if p.move {
						s.Start(fp)
					} else {
						s.Line(fp)
					}
				}
				if len(pts) > 0 {
					if s.rasterize(s.window()) {
						windowed++
					} else {
						fallbacks++
					}
				}
				s.Draw()
				if !bytes.Equal(got.Pix, want.Pix) {
					t.Errorf("%s: drew differently from vector", name)
				}

				// vector has no even-odd rule: the scanner's, in a window,
				// draws what it draws over all the mask.
				if len(pts) == 0 {
					continue
				}
				s.SetWinding(false)
				eo, all := cloneRGBA(bg), cloneRGBA(bg)
				s.Dest = eo
				s.Draw()
				s.Dest = all
				s.rasterize(image.Rectangle{Max: size})
				s.drawRGBA(all, image.Rectangle{Max: size})
				if !bytes.Equal(eo.Pix, all.Pix) {
					t.Errorf("%s: drew by the even-odd rule differently in a window", name)
				}
			}
		}
	}
	t.Logf("%d drawn in their windows, %d all over", windowed, fallbacks)
}

type funcImage ColorFunc

func (f funcImage) ColorModel() color.Model { return color.RGBA64Model }
func (f funcImage) Bounds() image.Rectangle { return image.Rect(-1e9, -1e9, 1e9, 1e9) }
func (f funcImage) At(x, y int) color.Color { return f(x, y) }

func cloneRGBA(m *image.RGBA) *image.RGBA {
	return &image.RGBA{Pix: bytes.Clone(m.Pix), Stride: m.Stride, Rect: m.Rect}
}

func randomImage(rnd *rand.Rand, size image.Point) *image.RGBA {
	img := image.NewRGBA(image.Rectangle{Max: size})
	rnd.Read(img.Pix)
	for i := 0; i < len(img.Pix); i += 4 {
		a := img.Pix[i+3]
		if a < 0x40 {
			a = 0
		}
		img.Pix[i+0] = uint8(uint32(img.Pix[i+0]) * uint32(a) / 0xff)
		img.Pix[i+1] = uint8(uint32(img.Pix[i+1]) * uint32(a) / 0xff)
		img.Pix[i+2] = uint8(uint32(img.Pix[i+2]) * uint32(a) / 0xff)
		img.Pix[i+3] = a
	}
	return img
}

// randomPath is a path of one of a few kinds, in points a 64th of a pixel
// apart, as the scanner gets them.
func randomPath(rnd *rand.Rand, size image.Point, kind int) []pathPoint {
	w, h := float32(size.X), float32(size.Y)
	q := func(v float32) float32 { return float32(int32(v*64)) / 64 }
	var pts []pathPoint
	add := func(x, y float32, move bool) { pts = append(pts, pathPoint{q(x), q(y), move}) }
	switch kind {
	case 0: // a small polygon inside
		cx, cy := rnd.Float32()*w, rnd.Float32()*h
		add(cx, cy, true)
		for i := 0; i < 2+rnd.Intn(6); i++ {
			add(cx+rnd.Float32()*20-10, cy+rnd.Float32()*20-10, false)
		}
		add(cx, cy, false)
	case 1: // a polygon over the edges
		add(rnd.Float32()*w*1.6-w*0.3, rnd.Float32()*h*1.6-h*0.3, true)
		for i := 0; i < 3+rnd.Intn(8); i++ {
			add(rnd.Float32()*w*1.6-w*0.3, rnd.Float32()*h*1.6-h*0.3, false)
		}
		add(pts[0].x, pts[0].y, false)
	case 2: // a sliver across the whole width, shallow
		y0 := rnd.Float32() * h
		dy := rnd.Float32()*6 - 3
		th := rnd.Float32() * 0.7
		add(-5, y0, true)
		add(w+5, y0+dy, false)
		add(w+5, y0+dy+th, false)
		add(-5, y0+th, false)
		add(-5, y0, false)
	case 3: // wholly outside, to one side
		ox := []float32{-50, w + 10, 0, 0}[rnd.Intn(4)]
		oy := float32(0)
		if ox == 0 {
			oy = []float32{-50, h + 10}[rnd.Intn(2)]
		}
		add(ox, oy+rnd.Float32()*h, true)
		add(ox+20, oy+rnd.Float32()*h, false)
		add(ox+30, oy+rnd.Float32()*h, false)
		add(ox, oy+rnd.Float32()*h, false)
		add(pts[0].x, pts[0].y, false)
	case 4: // open: lines never closed, and a line before any move
		add(rnd.Float32()*w, rnd.Float32()*h, false)
		for i := 0; i < 5; i++ {
			add(rnd.Float32()*w, rnd.Float32()*h, rnd.Intn(3) == 0)
		}
	case 5: // many thin shallow slivers stacked, from left of the target into it
		x1 := rnd.Float32() * w
		for i := 0; i < 30; i++ {
			y := rnd.Float32() * h
			add(-3, y, true)
			add(x1, y+rnd.Float32()*2, false)
			add(x1, y+2.1, false)
			add(-3, y+0.05, false)
			add(-3, y, false)
		}
	case 6: // a big shape past the right edge
		x0 := rnd.Float32() * w
		add(x0, -2, true)
		add(w*3, h*0.2, false)
		add(w*2, h+5, false)
		add(x0+3, h*0.7, false)
		add(x0, -2, false)
	case 7: // one long open diagonal and its way back, a hair apart
		add(-10, -10, true)
		add(w+10, h*0.9, false)
		add(w+10, h*0.9+0.02, false)
		add(-10, -10+0.03, false)
	}
	return pts
}

// The even-odd rule leaves a hole where a path covers twice; the non-zero
// one fills it. Each side of a hole, or of a shared edge, is as the
// non-zero rule draws it.
func TestEvenOdd(t *testing.T) {
	for _, size := range []image.Point{{40, 40}, {600, 40}} { // fixed and floating point
		draw := func(evenOdd bool, rects ...[4]float64) *image.RGBA {
			img := image.NewRGBA(image.Rectangle{Max: size})
			f := NewFiller(size.X, size.Y, NewScannerGV(size.X, size.Y, img, img.Bounds()))
			f.SetWinding(!evenOdd)
			f.SetPaint(ColorPaint(color.White))
			for _, r := range rects {
				AddRect(r[0], r[1], r[2], r[3], 0, f)
			}
			f.Draw()
			return img
		}
		ring := [][4]float64{{5, 5, 35, 35}, {15, 15, 25, 25}}
		eo, nz := draw(true, ring...), draw(false, ring...)
		for _, tc := range []struct {
			x, y   int
			eo, nz uint8
		}{
			{20, 20, 0, 0xff}, {10, 20, 0xff, 0xff}, {2, 20, 0, 0},
		} {
			if a := eo.RGBAAt(tc.x, tc.y).A; a != tc.eo {
				t.Errorf("%v: even-odd at (%d, %d): alpha %#x; want %#x", size, tc.x, tc.y, a, tc.eo)
			}
			if a := nz.RGBAAt(tc.x, tc.y).A; a != tc.nz {
				t.Errorf("%v: non-zero at (%d, %d): alpha %#x; want %#x", size, tc.x, tc.y, a, tc.nz)
			}
		}
		// Abutting at a half pixel, the pixel is covered once.
		if a := draw(true, [4]float64{5, 5, 20.5, 35}, [4]float64{20.5, 5, 35, 35}).RGBAAt(20, 20).A; a != 0xff {
			t.Errorf("%v: where two squares meet: alpha %#x; want opaque", size, a)
		}
		// Overlapping by half a pixel, the pixel is covered one and a
		// half times: half, by the even-odd rule.
		if a := draw(true, [4]float64{5, 5, 20.5, 35}, [4]float64{20, 5, 35, 35}).RGBAAt(20, 20).A; a < 0x7f || a > 0x80 {
			t.Errorf("%v: where two squares overlap by half a pixel: alpha %#x; want half", size, a)
		}
	}
}
