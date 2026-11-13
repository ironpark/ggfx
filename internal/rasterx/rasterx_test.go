// Copyright 2018 by the rasterx Authors. All rights reserved.
// Created 2018 by S.R.Wiley
package rasterx_test

import (
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	"github.com/ironpark/ggfx/internal/goldentest"
	. "github.com/ironpark/ggfx/internal/rasterx"
	"golang.org/x/image/colornames"
	"golang.org/x/image/math/fixed"
)

func getOpenCubicPath() (p Path) {
	p.Start(ToFixedP(50, 50))
	p.Line(ToFixedP(100, 50)) // Yes I meant to do this
	p.CubeBezier(ToFixedP(120, 70), ToFixedP(80, 90), ToFixedP(100, 100))
	p.CubeBezier(ToFixedP(101, 95), ToFixedP(80, 90), ToFixedP(75, 100))
	p.Line(ToFixedP(95, 120))
	p.Line(ToFixedP(78, 100))
	return
}

func getOpenCubicPath2() (p Path) {
	//M87, 212 C 138, 90,  269, 75, 259, 147 C 254, 71, 104,176, 128, 282z
	p.Start(ToFixedP(87, 212))
	p.CubeBezier(ToFixedP(138, 90), ToFixedP(269, 75), ToFixedP(259, 147))
	p.CubeBezier(ToFixedP(254, 71), ToFixedP(104, 176), ToFixedP(128, 282))
	p.Stop(true)

	p.Start(ToFixedP(600-87, 212))
	p.CubeBezier(ToFixedP(600-138, 90), ToFixedP(600-269, 75), ToFixedP(600-259, 147))
	p.CubeBezier(ToFixedP(600-254, 71), ToFixedP(600-104, 176), ToFixedP(600-128, 282))
	p.Stop(true)
	return
}

func getPartPath() (testPath Path) {
	//M210.08,222.97
	testPath.Start(ToFixedP(210.08, 222.97))
	//L192.55,244.95
	testPath.Line(ToFixedP(192.55, 244.95))
	//Q146.53,229.95,115.55,209.55
	testPath.QuadBezier(ToFixedP(146.53, 229.95), ToFixedP(115.55, 209.55))
	//Q102.50,211.00,95.38,211.00
	testPath.QuadBezier(ToFixedP(102.50, 211.00), ToFixedP(95.38, 211.00))
	//Q56.09,211.00,31.17,182.33
	testPath.QuadBezier(ToFixedP(56.09, 211.00), ToFixedP(31.17, 182.33))
	//Q6.27,153.66,6.27,108.44
	testPath.QuadBezier(ToFixedP(6.27, 153.66), ToFixedP(6.27, 108.44))
	//Q6.27,61.89,31.44,33.94
	testPath.QuadBezier(ToFixedP(6.27, 61.89), ToFixedP(31.44, 33.94))
	//Q56.62,6.00,98.55,6.00
	testPath.QuadBezier(ToFixedP(56.62, 6.00), ToFixedP(98.55, 6.00))
	//Q141.27,6.00,166.64,33.88
	testPath.QuadBezier(ToFixedP(141.27, 6.00), ToFixedP(166.64, 33.88))
	//Q192.02,61.77,192.02,108.70
	testPath.QuadBezier(ToFixedP(192.02, 61.77), ToFixedP(192.02, 108.70))
	//Q192.02,175.67,140.86,202.05
	testPath.QuadBezier(ToFixedP(192.02, 175.67), ToFixedP(140.86, 202.05))
	//Q173.42,216.66,210.08,222.97
	testPath.QuadBezier(ToFixedP(173.42, 216.66), ToFixedP(210.08, 222.97))
	//z
	testPath.Stop(true)
	return
}

func GetTestPath() (testPath Path) {
	//Path for Q
	testPath = getPartPath()

	//M162.22,109.69 M162.22,109.69
	testPath.Start(ToFixedP(162.22, 109.69))
	//Q162.22,70.11,145.61,48.55
	testPath.QuadBezier(ToFixedP(162.22, 70.11), ToFixedP(145.61, 48.55))
	//Q129.00,27.00,98.42,27.00
	testPath.QuadBezier(ToFixedP(129.00, 27.00), ToFixedP(98.42, 27.00))
	//Q69.14,27.00,52.53,48.62
	testPath.QuadBezier(ToFixedP(69.14, 27.00), ToFixedP(52.53, 48.62))
	//Q35.92,70.25,35.92,108.50
	testPath.QuadBezier(ToFixedP(35.92, 70.25), ToFixedP(35.92, 108.50))
	//Q35.92,146.75,52.53,168.38
	testPath.QuadBezier(ToFixedP(35.92, 146.75), ToFixedP(52.53, 168.38))
	//Q69.14,190.00,98.42,190.00
	testPath.QuadBezier(ToFixedP(69.14, 190.00), ToFixedP(98.42, 190.00))
	//Q128.34,190.00,145.28,168.70
	testPath.QuadBezier(ToFixedP(128.34, 190.00), ToFixedP(145.28, 168.70))
	//Q162.22,147.41,162.22,109.69
	testPath.QuadBezier(ToFixedP(162.22, 147.41), ToFixedP(162.22, 109.69))
	//z
	testPath.Stop(true)

	return
}

func BenchmarkScanGV(b *testing.B) {
	var (
		p         = GetTestPath()
		wx, wy    = 512, 512
		img       = image.NewRGBA(image.Rect(0, 0, wx, wy))
		scannerGV = NewScannerGV(wx, wy, img, img.Bounds())
	)
	f := NewFiller(wx, wy, scannerGV)
	p.AddTo(f)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Draw()
	}
}

func BenchmarkFillGV(b *testing.B) {
	var (
		p         = GetTestPath()
		wx, wy    = 512, 512
		img       = image.NewRGBA(image.Rect(0, 0, wx, wy))
		scannerGV = NewScannerGV(wx, wy, img, img.Bounds())
	)
	f := NewFiller(wx, wy, scannerGV)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.AddTo(f)
		f.Draw()
		f.Clear()
	}
}

func BenchmarkDashGV(b *testing.B) {
	var (
		p         = GetTestPath()
		wx, wy    = 512, 512
		img       = image.NewRGBA(image.Rect(0, 0, wx, wy))
		scannerGV = NewScannerGV(wx, wy, img, img.Bounds())
	)
	b.ResetTimer()
	d := NewDasher(wx, wy, scannerGV)
	d.SetStroke(10*64, 4*64, RoundCap, nil, RoundGap, ArcClip, []float64{33, 12}, 0)
	for i := 0; i < b.N; i++ {
		p.AddTo(d)
		d.Draw()
		d.Clear()
	}
}

// The original tests wrote these drawings to testdata to be looked at;
// here each is pinned by a hash of its pixels, per GOARCH as for the svg
// package's goldens.
var goldens = goldentest.File{
	Path:   "testdata/golden.txt",
	Header: "# drawing GOARCH sha256[:12] of the pixels; go test ./internal/rasterx -golden.update",
}

func TestMain(m *testing.M) { goldens.Main(m) }

func TestRoundRect(t *testing.T) {
	var (
		wx, wy    = 512, 512
		img       = image.NewRGBA(image.Rect(0, 0, wx, wy))
		scannerGV = NewScannerGV(wx, wy, img, img.Bounds())
		f         = NewFiller(wx, wy, scannerGV)
	)

	for _, r := range []struct {
		c                      color.Color
		x0, y0, x1, y1, rx, ry float64
		rot                    float64
		gf                     GapFunc
	}{
		{colornames.Cadetblue, 30, 30, 130, 130, 40, 40, 0, RoundGap},
		{colornames.Burlywood, 140, 30, 240, 130, 10, 40, 0, RoundGap},
		{colornames.Yellowgreen, 250, 30, 350, 130, 40, 10, 0, RoundGap},
		{colornames.Lightgreen, 370, 30, 470, 130, 20, 20, 45, RoundGap},
		{colornames.Cadetblue, 30, 140, 130, 240, 40, 40, 0, QuadraticGap},
		{colornames.Burlywood, 140, 140, 240, 240, 10, 40, 0, QuadraticGap},
		{colornames.Yellowgreen, 250, 140, 350, 240, 40, 10, 0, QuadraticGap},
		{colornames.Blueviolet, 370, 140, 470, 240, 20, 20, 45, QuadraticGap},
		{colornames.Cadetblue, 30, 250, 130, 350, 40, 40, 0, CubicGap},
		{colornames.Burlywood, 140, 250, 240, 350, 10, 40, 0, CubicGap},
		{colornames.Yellowgreen, 250, 250, 350, 350, 40, 10, 0, CubicGap},
		{colornames.Lightgreen, 370, 250, 470, 350, 20, 20, 45, CubicGap},
		{colornames.Cadetblue, 30, 360, 130, 460, 40, 40, 0, FlatGap},
		{colornames.Burlywood, 140, 360, 240, 460, 10, 40, 0, FlatGap},
		{colornames.Yellowgreen, 250, 360, 350, 460, 40, 10, 0, FlatGap},
		{colornames.Blueviolet, 370, 360, 470, 460, 20, 20, 45, FlatGap},
	} {
		scannerGV.SetPaint(ColorPaint(r.c))
		AddRoundRect(r.x0, r.y0, r.x1, r.y1, r.rx, r.ry, r.rot, r.gf, f)
		f.Draw()
		f.Clear()
	}
	goldens.Check(t, "roundRect", img)
}

func isClose(a, b Matrix2D, epsilon float64) bool {
	if math.Abs(a.A-b.A) > epsilon ||
		math.Abs(a.B-b.B) > epsilon ||
		math.Abs(a.C-b.C) > epsilon ||
		math.Abs(a.D-b.D) > epsilon ||
		math.Abs(a.E-b.E) > epsilon ||
		math.Abs(a.F-b.F) > epsilon {
		return false
	}
	return true
}

func TestCircleLineIntersect(t *testing.T) {
	a := fixed.Point26_6{X: 30 * 64, Y: 55 * 64}
	b := fixed.Point26_6{X: 40 * 64, Y: 40 * 64}
	c := fixed.Point26_6{X: 40 * 64, Y: 40 * 64}
	r := fixed.Int26_6(10 * 64)
	_, touching := RayCircleIntersection(a, b, c, r)
	if touching == false {
		t.Error("Ray not intersecting circle ", touching)
	}
}

func TestGeom(t *testing.T) {
	epsilon := 1e-12 // allowed range for round off error
	a := Identity
	b := a.Rotate(-math.Pi / 2)

	x, y := 3.0, 4.0
	m, n := b.Transform(x, y)
	if math.Abs(m-y) > epsilon || math.Abs(x+n) > epsilon {
		t.Error("rotate failed", m-y, x-n, m, n)
	}

	m, n = b.TransformVector(x, y)
	if math.Abs(m-y) > epsilon || math.Abs(x+n) > epsilon {
		t.Error("rotate failed", m-y, x-n, m, n)
	}

	c := b.Invert()
	d := b.Mult(c)
	if isClose(d, Identity, epsilon) == false {
		t.Error("Matrix inversion failed", b, c, d)
	}

	s1 := a.SkewY(2)
	if s1 == Identity {
		t.Error("skew failed")
	}
	s2 := s1.SkewY(-2)
	if !isClose(s2, a, epsilon) {
		t.Error("reverse skewy failed", s1, s2)
	}

	t1 := a.SkewX(2)
	if t1 == Identity {
		t.Error("skewx failed")
	}
	t2 := t1.SkewX(-2)
	if !isClose(t2, a, epsilon) {
		t.Error("reverse skewx failed", t1, t2)
	}
}

func TestToLength(t *testing.T) {
	p := fixed.Point26_6{X: 2, Y: -2}
	ln := fixed.I(40)

	q := ToLength(p, ln)
	expected := fixed.Point26_6{X: 1810, Y: -1810}
	if q != expected {
		t.Error("wrong point", q)
	}
}

func TestMatrixAdderReset(t *testing.T) {
	offsetPath := &MatrixAdder{M: Identity.Translate(180, 340)}
	offsetPath.Reset()
	if !isClose(offsetPath.M, Identity, 1e-12) {
		t.Error("path reset failed", offsetPath)
	}
}

// recorder is an Adder that writes what it is given back into a Path.
type recorder struct{ p Path }

func (r *recorder) Start(a fixed.Point26_6)            { r.p.Start(a) }
func (r *recorder) Line(b fixed.Point26_6)             { r.p.Line(b) }
func (r *recorder) QuadBezier(b, c fixed.Point26_6)    { r.p.QuadBezier(b, c) }
func (r *recorder) CubeBezier(b, c, d fixed.Point26_6) { r.p.CubeBezier(b, c, d) }
func (r *recorder) Stop(closeLoop bool)                { r.p.Stop(closeLoop) }

func TestPathAddToReplaysThePath(t *testing.T) {
	for _, p := range []Path{getOpenCubicPath(), getOpenCubicPath2(), GetTestPath()} {
		var r recorder
		p.AddTo(&r)
		if !slices.Equal(r.p, p) {
			t.Errorf("AddTo replayed\n%v\nwant\n%v", r.p, p)
		}
	}
}

func TestPathString(t *testing.T) {
	var p Path
	p.Start(ToFixedP(1, 2))
	p.Line(ToFixedP(3.5, 4))
	p.QuadBezier(ToFixedP(5, 6), ToFixedP(7, 8))
	p.Stop(true)
	if got, want := p.String(), "M1.000,2.000 L3.500,4.000 Q5.000,6.000,7.000,8.000 Z"; got != want {
		t.Errorf("String() = %q; want %q", got, want)
	}
	p.Clear()
	if len(p) != 0 {
		t.Errorf("Clear left %v", p)
	}
}

func TestShapes(t *testing.T) {
	var (
		wx, wy = 512, 512

		imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))

		scannerGV = NewScannerGV(wx, wy, imgs, imgs.Bounds())
		f         = NewFiller(wx, wy, scannerGV)
		s         = NewStroker(wx, wy, scannerGV)
		d         = NewDasher(wx, wy, scannerGV)
	)

	doShapes(t, f, f, "shapeGVF", imgs)

	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	s.SetStroke(10*64, 4*64, RoundCap, nil, RoundGap, ArcClip)
	doShapes(t, s, s, "shapeGVS1", imgs)

	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	s.SetStroke(10*64, 4*64, nil, RoundCap, RoundGap, ArcClip)
	doShapes(t, s, s, "shapeGVS2", imgs)

	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	s.SetStroke(10*64, 4*64, nil, nil, nil, Miter)
	doShapes(t, s, s, "shapeGVS3", imgs)

	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	d.SetStroke(10*64, 4*64, SquareCap, nil, RoundGap, ArcClip, []float64{33, 12}, 30)
	doShapes(t, d, d, "shapeGVD0", imgs)

	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	d.SetStroke(10*64, 4*64, RoundCap, nil, RoundGap, Miter, []float64{33, 12}, 250)
	doShapes(t, d, d, "shapeGVD1", imgs)

	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	d.SetStroke(10*64, 4*64, ButtCap, CubicCap, QuadraticGap, Arc, []float64{33, 12}, -30)
	doShapes(t, d, d, "shapeGVD2", imgs)

	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	d.SetStroke(10*64, 4*64, nil, QuadraticCap, RoundGap, MiterClip, []float64{12, 4}, 14)
	doShapes(t, d, d, "shapeGVD3", imgs)

	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	d.SetStroke(10*64, 4*64, RoundCap, nil, RoundGap, Bevel, []float64{0, 0}, 0)
	doShapes(t, d, d, "shapeGVD4", imgs)

	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	d.SetStroke(10*64, 4*64, SquareCap, nil, nil, Round, []float64{}, 0)
	doShapes(t, d, d, "shapeGVD5", imgs)

	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	d.SetStroke(10*64, 4*64, RoundCap, nil, RoundGap, MiterClip, nil, 0)
	doShapes(t, d, d, "shapeGVD6", imgs)

	getOpenCubicPath().AddTo(f)
	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	f.Draw()
	f.Clear()

	s.SetStroke(4*64, 1, SquareCap, nil, RoundGap, ArcClip)
	getOpenCubicPath().AddTo(s)
	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	s.Draw()
	s.Clear()
	goldens.Check(t, "shapeT1", imgs)

	s.SetStroke(4<<6, 2<<6, SquareCap, nil, RoundGap, ArcClip)
	getOpenCubicPath2().AddTo(s)
	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	s.Draw()
	s.Clear()
	goldens.Check(t, "shapeT2", imgs)

	s.SetStroke(25<<6, 200<<6, CubicCap, CubicCap, CubicGap, ArcClip)
	p := getOpenCubicPath2()
	p.AddTo(s)
	imgs = image.NewRGBA(image.Rect(0, 0, wx, wy))
	scannerGV.Dest = imgs
	s.Draw()
	s.Clear()
	goldens.Check(t, "shapeT3", imgs)

	d.SetBounds(-20, -12) // Test min x and y value checking
}

func doShapes(t *testing.T, f Scanner, fa Adder, name string, img *image.RGBA) {
	f.SetPaint(ColorPaint(colornames.Blueviolet))
	AddEllipse(240, 200, 140, 180, 0, fa)
	f.Draw()
	f.Clear()

	f.SetPaint(ColorPaint(colornames.Darkseagreen))
	AddEllipse(240, 200, 40, 180, 45, fa)
	f.Draw()
	f.Clear()

	f.SetPaint(ColorPaint(colornames.Darkgoldenrod))
	AddCircle(300, 300, 80, fa)
	f.Draw()
	f.Clear()

	f.SetPaint(ColorPaint(colornames.Forestgreen))
	AddRoundRect(30, 30, 130, 130, 10, 20, 45, RoundGap, fa)
	f.Draw()
	f.Clear()

	f.SetPaint(ColorPaint(colornames.Blueviolet))
	AddRoundRect(30, 30, 130, 130, 150, 150, 0, nil, fa)
	f.Draw()
	f.Clear()

	f.SetPaint(ColorPaint(ApplyOpacity(colornames.Lightgoldenrodyellow, 0.6)))
	AddCircle(80, 80, 50, fa)
	f.Draw()
	f.Clear()

	f.SetPaint(ColorPaint(colornames.Lemonchiffon))
	f.SetClip(image.Rect(65, 65, 95, 95))
	AddCircle(80, 80, 50, fa)
	f.Draw()
	f.Clear()

	f.SetClip(image.ZR)

	f.SetPaint(ColorPaint(colornames.Firebrick))
	AddRect(370, 370, 400, 500, 15, fa)
	f.Draw()
	f.Clear()

	goldens.Check(t, name, img)
}

func TestFindElipsecenter(t *testing.T) {
	var ra, rb = 10.0, 5.0
	cx, cy := FindEllipseCenter(&ra, &rb, 0.0, 0.0, 0.0, 20.0, 0.0, true, true)
	if cx != 10 || cy != 0 || ra != 10 || rb != 5 {
		t.Error("Find elipse center failed ", cx, cy, ra, rb)
	}
	cx, cy = FindEllipseCenter(&ra, &rb, 0.0, 0.0, 0.0, 35.0, 5.0, false, true)
	if ra == 10 || rb == 5 {
		t.Error("Find elipse center failed with resize of radiuses ", cx, cy, ra, rb)
	}
	ra, rb = 5.0, 5.0
	cx, cy = FindEllipseCenter(&ra, &rb, 0.0, 0.0, 0.0, 35.0, 5.0, true, true)
	if ra == 10 || rb == 5 {
		t.Error("Find elipse center failed with resize of radiuses ", cx, cy, ra, rb)
	}
}

// TestGradient tests a Dasher's ability to function
// as a filler, stroker, and dasher by invoking the corresponding anonymous structs
func TestGradient(t *testing.T) {
	var (
		wx, wy    = 512, 512
		img       = image.NewRGBA(image.Rect(0, 0, wx, wy))
		scannerGV = NewScannerGV(wx, wy, img, img.Bounds())
	)

	linearGradient := &Gradient{Points: [5]float64{0, 0, 1, 0, 0},
		IsRadial: false, Bounds: struct{ X, Y, W, H float64 }{
			X: 50, Y: 50, W: 100, H: 100}, Matrix: Identity}

	linearGradient.Stops = []GradStop{
		{StopColor: colornames.Aquamarine, Offset: 0.3, Opacity: 1.0},
		{StopColor: colornames.Skyblue, Offset: 0.6, Opacity: 1},
		{StopColor: colornames.Darksalmon, Offset: 1.0, Opacity: .75},
	}

	radialGradient := &Gradient{Points: [5]float64{0.5, 0.5, 0.5, 0.5, 0.5},
		IsRadial: true, Bounds: struct{ X, Y, W, H float64 }{
			X: 230, Y: 230, W: 100, H: 100},
		Matrix: Identity, Spread: ReflectSpread}

	radialGradient.Stops = []GradStop{
		{StopColor: colornames.Orchid, Offset: 0.3, Opacity: 1},
		{StopColor: colornames.Bisque, Offset: 0.6, Opacity: 1},
		{StopColor: colornames.Chartreuse, Offset: 1.0, Opacity: 0.4},
	}

	d := NewDasher(wx, wy, scannerGV)
	d.SetStroke(10*64, 4*64, RoundCap, nil, RoundGap, ArcClip, []float64{33, 12}, 0)
	// p is in the shape of a capital Q
	p := getPartPath()

	f := &d.Filler // This is the anon Filler in the Dasher. It also satisfies
	// the Rasterizer interface, and can only perform a fill on the path.

	offsetPath := &MatrixAdder{Adder: f, M: Identity.Translate(180, 180)}

	p.AddTo(offsetPath)

	scannerGV.SetPaint(radialGradient.GetColorFunction(1))
	f.Draw()
	f.Clear()

	scannerGV.SetClip(image.Rect(420, 350, 460, 400))
	offsetPath.M = Identity.Translate(340, 180)
	scannerGV.SetPaint(radialGradient.GetColorFunction(1))
	p.AddTo(offsetPath)
	f.Draw()
	f.Clear()
	scannerGV.SetClip(image.ZR)
	offsetPath.M = Identity.Translate(180, 340)
	p.AddTo(offsetPath)
	f.Draw()
	f.Clear()

	scannerGV.SetPaint(linearGradient.GetColorFunction(1.0))
	p.AddTo(f)
	f.Draw()
	f.Clear()

	linearGradient.Spread = RepeatSpread
	scannerGV.SetPaint(linearGradient.GetColorFunction(1.0))
	AddRect(20, 460, 150, 610, 45, f)
	f.Draw()
	f.Clear()

	radialGradient.Units = UserSpaceOnUse
	scannerGV.SetPaint(radialGradient.GetColorFunction(1.0))
	AddRect(300, 20, 450, 170, 0, f)
	f.Draw()
	f.Clear()

	linearGradient.Units = UserSpaceOnUse
	scannerGV.SetPaint(linearGradient.GetColorFunction(1.0))
	AddRect(300, 180, 450, 200, 0, f)
	f.Draw()
	f.Clear()

	radialGradient.Units = ObjectBoundingBox
	radialGradient.Points = [5]float64{0.5, 0.5, 0, 0, 0.2} // move focus away from
	scannerGV.SetPaint(radialGradient.GetColorFunction(1.0))
	AddRect(300, 210, 450, 300, 0, f)
	f.Draw()
	f.Clear()

	radialGradient.Units = UserSpaceOnUse
	linearGradient.Spread = PadSpread
	radialGradient.Points = [5]float64{0.5, 0.5, 0.1, 0.1, 0.5} // move focus away from center
	scannerGV.SetPaint(radialGradient.GetColorFunction(1.0))
	AddRect(20, 160, 150, 310, 0, f)
	f.Draw()
	f.Clear()

	linearGradient.Stops = linearGradient.Stops[0:1]
	scannerGV.SetPaint(linearGradient.GetColorFunction(1.0))
	AddRect(300, 180, 450, 200, 0, f)
	f.Draw()
	f.Clear()

	linearGradient.Stops = linearGradient.Stops[0:0]
	scannerGV.SetPaint(linearGradient.GetColorFunction(1.0))
	AddRect(300, 180, 450, 200, 0, f)
	f.Draw()
	f.Clear()

	// Lets try a sinusoidal grid pattern.
	var colF ColorFunc = func(x, y int) color.RGBA64 {
		sinx, siny, sinxy := math.Sin(float64(x)*math.Pi/20), math.Sin(float64(y)*math.Pi/10),
			math.Sin(float64(y+x)*math.Pi/30)
		r := (1 + sinx) * 120
		g := (1 + siny) * 120
		b := (1 + sinxy) * 120
		return color.RGBA64{uint16(uint8(r)) * 0x101, uint16(uint8(g)) * 0x101, uint16(uint8(b)) * 0x101, 0xffff}
	}

	scannerGV.SetPaint(FuncPaint(colF))
	AddRect(20, 300, 150, 450, 0, f)

	f.Draw()
	f.Clear()

	goldens.Check(t, "gradGV", img)
}

// TestMultiFunction tests a Dasher's ability to function
// as a filler, stroker, and dasher by invoking the corresponding anonymous structs
func TestMultiFunctionGV(t *testing.T) {

	var (
		wx, wy    = 512, 512
		img       = image.NewRGBA(image.Rect(0, 0, wx, wy))
		scannerGV = NewScannerGV(wx, wy, img, img.Bounds())
	)

	scannerGV.SetPaint(ColorPaint(colornames.Cornflowerblue))
	d := NewDasher(wx, wy, scannerGV)
	d.SetStroke(10*64, 4*64, RoundCap, nil, RoundGap, ArcClip, []float64{33, 12}, 0)
	// p is in the shape of a capital Q
	p := GetTestPath()

	f := &d.Filler // This is the anon Filler in the Dasher. It also satisfies
	// the Rasterizer interface, and will only perform a fill on the path.

	p.AddTo(f)

	// The extent is the path's own: its curves are flattened inside their
	// control points.
	extentR := scannerGV.GetPathExtent()
	if want := (fixed.Rectangle26_6{Min: ToFixedP(6.27, 6), Max: ToFixedP(210.08, 244.95)}); extentR != want {
		t.Errorf("extent %v; want %v", extentR, want)
	}
	f.Draw()
	f.Clear()

	scannerGV.SetPaint(ColorPaint(colornames.Cornsilk))

	s := &d.Stroker // This is the anon Stroke in the Dasher. It also satisfies
	// the Rasterizer interface, but will perform a fill on the path.
	p.AddTo(s)
	s.Draw()
	s.Clear()

	scannerGV.SetPaint(ColorPaint(colornames.Darkolivegreen))

	// Now lets use the Dasher itself; it will perform a dashed stroke if dashes are set
	// in the SetStroke method.
	p.AddTo(d)
	d.Draw()
	d.Clear()

	goldens.Check(t, "tmfGV", img)
}

// A drawing the size of the destination's floating-point accumulator, and
// one with its bounds away from the origin.
func TestLargeAndOffsetDestinations(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    image.Rectangle
	}{
		{"large", image.Rect(0, 0, 777, 600)},
		{"offset", image.Rect(-40, 30, 400, 300)},
	} {
		img := image.NewRGBA(tc.r)
		w, h := tc.r.Dx(), tc.r.Dy()
		scannerGV := NewScannerGV(w, h, img, img.Bounds())
		d := NewDasher(w, h, scannerGV)
		p := GetTestPath()
		big := &MatrixAdder{Adder: &d.Filler, M: Identity.Translate(-60, 40).Scale(3, 2.5)}
		scannerGV.SetPaint(ColorPaint(colornames.Teal))
		p.AddTo(big)
		d.Draw()
		d.Clear()
		d.SetStroke(7*64, 4*64, RoundCap, nil, RoundGap, Miter, []float64{20, 9}, 3)
		scannerGV.SetPaint(ColorPaint(ApplyOpacity(colornames.Crimson, 0.7)))
		big.Adder = d
		p.AddTo(big)
		d.Draw()
		d.Clear()
		g := &Gradient{Points: [5]float64{0.5, 0.5, 0.3, 0.2, 0.5}, IsRadial: true,
			Bounds: struct{ X, Y, W, H float64 }{X: 100, Y: 100, W: 500, H: 400},
			Matrix: Identity, Spread: RepeatSpread, Stops: []GradStop{
				{StopColor: colornames.Gold, Offset: 0, Opacity: 1},
				{StopColor: colornames.Navy, Offset: 1, Opacity: 0.5},
			}}
		scannerGV.SetPaint(g.GetColorFunction(1))
		AddCircle(350, 300, 220, &d.Filler)
		d.Draw()
		d.Clear()
		goldens.Check(t, "dest-"+tc.name, img)
	}
}

// Stops of translucent and 16-bit colors, and a translucent fill.
func TestTranslucentColors(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	scannerGV := NewScannerGV(200, 100, img, img.Bounds())
	f := NewFiller(200, 100, scannerGV)
	g := &Gradient{Points: [5]float64{0, 0, 1, 0, 0}, Matrix: Identity,
		Bounds: struct{ X, Y, W, H float64 }{X: 10, Y: 10, W: 180, H: 40},
		Stops: []GradStop{
			{StopColor: color.NRGBA{0xff, 0x80, 0x00, 0x80}, Offset: 0, Opacity: 1},
			{StopColor: color.RGBA64{0x1234, 0x5678, 0x9abc, 0xffff}, Offset: 0.5, Opacity: 1},
			{StopColor: color.NRGBA{0x00, 0x40, 0xff, 0x40}, Offset: 1, Opacity: 0.8},
		}}
	scannerGV.SetPaint(g.GetColorFunction(1))
	AddRect(10, 10, 190, 50, 0, f)
	f.Draw()
	f.Clear()
	scannerGV.SetPaint(ColorPaint(ApplyOpacity(color.NRGBA{0x20, 0xa0, 0x60, 0x99}, 0.7)))
	AddRect(10, 60, 100, 90, 0, f)
	f.Draw()
	f.Clear()
	scannerGV.SetPaint(ColorPaint(ApplyOpacity(color.RGBA64{0xfedc, 0x0123, 0x8000, 0xffff}, 1)))
	AddRect(100, 60, 190, 90, 0, f)
	f.Draw()
	f.Clear()
	goldens.Check(t, "translucent", img)
}

// ApplyOpacity gives back an 8-bit color as it was, whatever its alpha,
// takes a 16-bit one's high byte, and scales the alpha.
func TestApplyOpacity(t *testing.T) {
	for c := 0; c < 0x100; c++ {
		for a := 1; a < 0x100; a++ {
			in := color.NRGBA{uint8(c), uint8(255 - c), uint8(c / 3), uint8(a)}
			if got := ApplyOpacity(in, 1); got != in {
				t.Fatalf("ApplyOpacity(%v, 1) = %v", in, got)
			}
		}
	}
	for _, tc := range []struct {
		in      color.Color
		opacity float64
		want    color.NRGBA
	}{
		{color.RGBA64{0x1234, 0x5678, 0x9abc, 0xffff}, 1, color.NRGBA{0x12, 0x56, 0x9a, 0xff}},
		{color.RGBA{0x40, 0x20, 0, 0x80}, 1, color.NRGBA{0x80, 0x40, 0, 0x80}},
		{color.NRGBA64{0x1234, 0x5678, 0x9abc, 0x8000}, 1, color.NRGBA{0x12, 0x56, 0x9a, 0x7f}},
		{color.NRGBA{10, 20, 30, 0x80}, 0.5, color.NRGBA{10, 20, 30, 0x40}},
		{color.NRGBA{10, 20, 30, 0xff}, 0.6, color.NRGBA{10, 20, 30, 153}},
		{color.NRGBA{10, 20, 30, 0xff}, 2, color.NRGBA{10, 20, 30, 0xff}},
		{color.NRGBA{10, 20, 30, 0}, 1, color.NRGBA{}},
	} {
		if got := ApplyOpacity(tc.in, tc.opacity); got != tc.want {
			t.Errorf("ApplyOpacity(%v, %v) = %v; want %v", tc.in, tc.opacity, got, tc.want)
		}
	}
}

// A gradient blends its stops' colors and opacities apart: halfway from
// half-transparent red to opaque blue is purple, three quarters opaque.
func TestGradientBlendsTranslucentStops(t *testing.T) {
	g := &Gradient{Points: [5]float64{0, 0, 1, 0, 0}, Matrix: Identity, Units: UserSpaceOnUse,
		Bounds: struct{ X, Y, W, H float64 }{W: 1, H: 1},
		Stops: []GradStop{
			{StopColor: color.NRGBA{0xff, 0, 0, 0x80}, Offset: 0, Opacity: 1},
			{StopColor: color.NRGBA{0, 0, 0xff, 0xff}, Offset: 1, Opacity: 1},
		}}
	g.Points = [5]float64{0, 0, 100, 0, 0}
	got := g.GetColorFunction(1).Func(49, 0) // t = 0.495
	if want := ColorPaint(color.NRGBA{0x81, 0, 0x7e, 0xbe}).Color; got != want {
		t.Errorf("halfway: %v; want %v", got, want)
	}
}
