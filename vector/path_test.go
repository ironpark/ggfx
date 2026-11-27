// Copyright 2024 The Ebitengine Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package vector_test

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/vector"
)

func TestMoveToAndClose(t *testing.T) {
	var path vector.Path
	if _, ok := vector.CurrentPosition(&path); ok != false {
		t.Errorf("expected no last position, got one")
	}
	if got, want := vector.SubPathCount(&path), 0; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}

	path.MoveTo(10, 20)
	if p, ok := vector.CurrentPosition(&path); p != (vector.Point{10, 20}) || !ok {
		t.Errorf("expected last position to be (10, 20), got %v", p)
	}
	if got, want := vector.SubPathCount(&path), 1; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}

	path.MoveTo(30, 40)
	if p, ok := vector.CurrentPosition(&path); p != (vector.Point{30, 40}) || !ok {
		t.Errorf("expected last position to be (30, 40), got %v", p)
	}
	if got, want := vector.SubPathCount(&path), 1; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}

	path.LineTo(50, 60)
	if p, ok := vector.CurrentPosition(&path); p != (vector.Point{50, 60}) || !ok {
		t.Errorf("expected last position to be (50, 60), got %v", p)
	}
	if got, want := vector.SubPathCount(&path), 1; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}

	path.Close()
	if p, ok := vector.CurrentPosition(&path); p != (vector.Point{30, 40}) || !ok {
		t.Errorf("expected last position to be (30, 40) after close, got %v", p)
	}
	if got, want := vector.SubPathCount(&path), 1; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}

	path.MoveTo(70, 80)
	if p, ok := vector.CurrentPosition(&path); p != (vector.Point{70, 80}) || !ok {
		t.Errorf("expected last position to be (70, 80), got %v", p)
	}
	if got, want := vector.SubPathCount(&path), 2; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}

	path.LineTo(90, 100)
	if p, ok := vector.CurrentPosition(&path); p != (vector.Point{90, 100}) || !ok {
		t.Errorf("expected last position to be (50, 60), got %v", p)
	}
	if got, want := vector.SubPathCount(&path), 2; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}

	// MoveTo without closing forces to create a new sub-path.
	// The previous sub-path is left unclosed.
	path.MoveTo(110, 120)
	if p, ok := vector.CurrentPosition(&path); p != (vector.Point{110, 120}) || !ok {
		t.Errorf("expected last position to be (70, 80), got %v", p)
	}
	if got, want := vector.SubPathCount(&path), 3; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}
}

func TestAddPath(t *testing.T) {
	var path vector.Path
	path.MoveTo(10, 20)
	path.LineTo(30, 40)
	path.Close()

	op := &vector.AddPathOptions{}
	op.GeoM.Translate(100, 100)
	var path2 vector.Path
	path2.AddPath(&path, op)

	if p, ok := vector.CurrentPosition(&path); p != (vector.Point{10, 20}) || !ok {
		t.Errorf("expected last position to be (10, 20), got %v", p)
	}
	if got, want := vector.SubPathCount(&path), 1; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}
	if p, ok := vector.CurrentPosition(&path2); p != (vector.Point{110, 120}) || !ok {
		t.Errorf("expected last position to be (110, 120), got %v", p)
	}
	if got, want := vector.SubPathCount(&path2), 1; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}
}

func TestAddPathSelf(t *testing.T) {
	var path vector.Path
	path.MoveTo(10, 20)
	path.LineTo(30, 40)
	path.Close()

	op := &vector.AddPathOptions{}
	op.GeoM.Translate(100, 100)
	path.AddPath(&path, op)

	if p, ok := vector.CurrentPosition(&path); p != (vector.Point{110, 120}) || !ok {
		t.Errorf("expected last position to be (110, 120), got %v", p)
	}
	if got, want := vector.SubPathCount(&path), 2; got != want {
		t.Errorf("expected close count to be %d, got %d", want, got)
	}
}

func TestArcAndGeoM(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("the result might be flaky in this environment")
	}

	testCases := []struct {
		name     string
		geoM     ggfx.GeoM
		origPath vector.Path
		refPath  vector.Path
	}{
		{
			name: "identity",
			geoM: ggfx.GeoM{},
			origPath: func() (p vector.Path) {
				p.MoveTo(0, 0)
				p.ArcTo(16, 0, 16, 16, 16)
				return p
			}(),
			refPath: func() (p vector.Path) {
				p.MoveTo(0, 0)
				p.ArcTo(16, 0, 16, 16, 16)
				return p
			}(),
		},
		{
			name: "scale 2x",
			geoM: func() (geoM ggfx.GeoM) {
				geoM.Scale(2, 2)
				return geoM
			}(),
			origPath: func() (p vector.Path) {
				p.MoveTo(0, 0)
				p.ArcTo(8, 0, 8, 8, 8)
				return p
			}(),
			refPath: func() (p vector.Path) {
				p.MoveTo(0, 0)
				p.ArcTo(16, 0, 16, 16, 16)
				return p
			}(),
		},
		{
			name: "scale 256x",
			geoM: func() (geoM ggfx.GeoM) {
				geoM.Scale(256, 256)
				return geoM
			}(),
			origPath: func() (p vector.Path) {
				p.MoveTo(0, 0)
				p.ArcTo(1.0/16.0, 0, 1.0/16.0, 1.0/16.0, 1.0/16.0)
				return p
			}(),
			refPath: func() (p vector.Path) {
				p.MoveTo(0, 0)
				p.ArcTo(16, 0, 16, 16, 16)
				return p
			}(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			origDst := ggfx.NewImage(16, 16)
			defer origDst.Deallocate()
			refDst := ggfx.NewImage(16, 16)
			defer refDst.Deallocate()

			var path vector.Path
			op := &vector.AddPathOptions{}
			op.GeoM = tc.geoM
			path.AddPath(&tc.origPath, op)

			strokeOp := &vector.StrokeOptions{
				Width: 1,
			}
			// Do not use alpha blending, which can cause non-deterministic results.
			vector.StrokePath(origDst, &path, strokeOp, nil)
			vector.StrokePath(refDst, &tc.refPath, strokeOp, nil)

			for j := range 16 {
				for i := range 16 {
					got := origDst.At(i, j)
					want := refDst.At(i, j)
					if got != want {
						t.Errorf("At(%d, %d): got: %v, want: %v", i, j, got, want)
					}
				}
			}
		})
	}
}

// Issue #3666
func TestArcHugeAngle(t *testing.T) {
	// For an angle this big, the float32 spacing is bigger than the angle by which an arc is split.
	// Splitting such an arc must still terminate.
	testCases := []struct {
		name       string
		startAngle float32
		sweep      float32
		dir        vector.Direction
	}{
		{
			name:       "clockwise",
			startAngle: 2e7,
			sweep:      2,
			dir:        vector.Clockwise,
		},
		{
			name:       "clockwise, small sweep",
			startAngle: 1 << 24,
			sweep:      1.6,
			dir:        vector.Clockwise,
		},
		{
			name:       "clockwise, full circle",
			startAngle: 1 << 24,
			sweep:      2 * math.Pi,
			dir:        vector.Clockwise,
		},
		{
			name:       "clockwise, negative angle",
			startAngle: -(1 << 25),
			sweep:      4,
			dir:        vector.Clockwise,
		},
		{
			name:       "clockwise, wider spacing",
			startAngle: 1 << 26,
			sweep:      8,
			dir:        vector.Clockwise,
		},
		{
			name:       "counterclockwise",
			startAngle: 2e7,
			sweep:      -2,
			dir:        vector.CounterClockwise,
		},
		{
			name:       "counterclockwise, negative angle",
			startAngle: -(1 << 25),
			sweep:      -4,
			dir:        vector.CounterClockwise,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			const (
				cx     = 100
				cy     = 100
				radius = 5
			)
			var p vector.Path
			p.Arc(cx, cy, radius, tc.startAngle, tc.startAngle+tc.sweep, tc.dir)

			// The angles are degenerate, so the shape is not specified, but it must be finite.
			// The control points of the approximated Bézier curves can reach out this far.
			const allow = 32 * radius
			bounds := p.Bounds()
			if want := image.Rect(cx-allow, cy-allow, cx+allow, cy+allow); !bounds.In(want) {
				t.Errorf("bounds: got: %v, want: a rectangle in %v", bounds, want)
			}
		})
	}
}

// Issue #3330
func TestFillPathSubImage(t *testing.T) {
	dst := ggfx.NewImage(16, 16)

	dst2 := dst.SubImage(image.Rect(0, 0, 8, 8)).(*ggfx.Image)
	var p vector.Path
	p.MoveTo(0, 0)
	p.LineTo(8, 0)
	p.LineTo(8, 8)
	p.LineTo(0, 8)
	p.Close()
	op := &vector.DrawPathOptions{}
	op.ColorScale.ScaleWithColor(color.White)
	op.AntiAlias = true
	vector.FillPath(dst2, &p, nil, op)
	if got, want := dst.At(5, 5), (color.RGBA{0xff, 0xff, 0xff, 0xff}); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
	if got, want := dst2.At(5, 5), (color.RGBA{0xff, 0xff, 0xff, 0xff}); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}

	dst3 := dst2.SubImage(image.Rect(4, 4, 8, 8)).(*ggfx.Image)
	var p2 vector.Path
	p2.MoveTo(4, 4)
	p2.LineTo(8, 4)
	p2.LineTo(8, 8)
	p2.LineTo(4, 8)
	p2.Close()
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(color.Black)
	vector.FillPath(dst3, &p2, nil, op)
	if got, want := dst.At(5, 5), (color.RGBA{0x00, 0x00, 0x00, 0xff}); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
	if got, want := dst2.At(5, 5), (color.RGBA{0x00, 0x00, 0x00, 0xff}); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
	if got, want := dst3.At(5, 5), (color.RGBA{0x00, 0x00, 0x00, 0xff}); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestRaceConditionWithSubImage(t *testing.T) {
	const w, h = 16, 16
	src := ggfx.NewImage(w, h)

	var wg sync.WaitGroup
	for i := range h {
		for j := range w {
			wg.Go(func() {
				subImg := src.SubImage(image.Rect(i, j, i+1, j+1)).(*ggfx.Image)
				var p vector.Path
				p.MoveTo(0, 0)
				p.LineTo(w, 0)
				p.LineTo(w, h)
				p.LineTo(0, h)
				p.Close()
				op := &vector.DrawPathOptions{}
				op.ColorScale.ScaleWithColor(color.White)
				op.AntiAlias = true
				vector.FillPath(subImg, &p, nil, op)
				dst := ggfx.NewImage(w, h)
				dst.DrawImage(subImg, nil)
			})
		}
	}
	wg.Wait()
}

// Issue #3355
func TestFillPathSubImageAndImage(t *testing.T) {
	dst := ggfx.NewImage(200, 200)
	defer dst.Deallocate()
	for i := range 100 {
		var path vector.Path
		path.LineTo(0, 0)
		path.LineTo(0, 100)
		path.LineTo(100, 100)
		path.LineTo(100, 0)
		path.LineTo(0, 0)
		path.Close()
		drawOp := &vector.DrawPathOptions{}
		drawOp.ColorScale.ScaleWithColor(color.RGBA{255, 0, 0, 255})
		subDst := dst.SubImage(image.Rect(0, 0, 100, 100)).(*ggfx.Image)
		vector.FillPath(subDst, &path, nil, drawOp)
		drawOp.ColorScale.Reset()
		drawOp.ColorScale.ScaleWithColor(color.RGBA{0, 255, 0, 255})
		vector.FillPath(dst, &path, nil, drawOp)

		if got, want := dst.At(50, 50), (color.RGBA{0, 255, 0, 255}); got != want {
			t.Errorf("%d: got: %v, want: %v", i, got, want)
		}
	}
}

// Issue #3366
func TestFillPathFillRule(t *testing.T) {
	testCases := []struct {
		name     string
		fillRule vector.FillRule
		expected color.RGBA
	}{
		{
			name:     "evenOdd",
			fillRule: vector.FillRuleEvenOdd,
			expected: color.RGBA{0, 0, 0, 0},
		},
		{
			name:     "nonZero",
			fillRule: vector.FillRuleNonZero,
			expected: color.RGBA{0xff, 0xff, 0xff, 0xff},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dst := ggfx.NewImage(16, 16)
			defer dst.Deallocate()

			var p vector.Path
			p.MoveTo(0, 0)
			p.LineTo(16, 0)
			p.LineTo(16, 16)
			p.LineTo(0, 16)
			p.Close()
			p.MoveTo(4, 4)
			p.LineTo(12, 4)
			p.LineTo(12, 12)
			p.LineTo(4, 12)
			p.Close()
			fillOp := &vector.FillOptions{}
			fillOp.FillRule = tc.fillRule
			vector.FillPath(dst, &p, fillOp, nil)

			if got, want := dst.At(8, 8), tc.expected; got != want {
				t.Errorf("got: %v, want: %v", got, want)
			}
		})
	}
}

func TestBounds(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	testCases := []struct {
		name string
		path func(p *vector.Path)
		want image.Rectangle
	}{
		{
			name: "empty",
			path: func(p *vector.Path) {},
			want: image.Rectangle{},
		},
		{
			name: "moveTo only",
			path: func(p *vector.Path) {
				p.MoveTo(100, 50)
			},
			want: image.Rectangle{},
		},
		{
			name: "horizontal line",
			path: func(p *vector.Path) {
				p.MoveTo(100, 50)
				p.LineTo(200, 50)
			},
			want: image.Rect(100, 50, 200, 50),
		},
		{
			name: "vertical line",
			path: func(p *vector.Path) {
				p.MoveTo(50, 10)
				p.LineTo(50, 100)
			},
			want: image.Rect(50, 10, 50, 100),
		},
		{
			name: "diagonal line",
			path: func(p *vector.Path) {
				p.MoveTo(10.5, 20.5)
				p.LineTo(30.5, 40.5)
			},
			want: image.Rect(10, 20, 31, 41),
		},
		{
			name: "two horizontal lines",
			path: func(p *vector.Path) {
				p.MoveTo(100, 50)
				p.LineTo(200, 50)
				p.MoveTo(120, 80)
				p.LineTo(220, 80)
			},
			want: image.Rect(100, 50, 220, 80),
		},
		{
			name: "rectangle",
			path: func(p *vector.Path) {
				p.MoveTo(10, 20)
				p.LineTo(30, 20)
				p.LineTo(30, 40)
				p.LineTo(10, 40)
				p.Close()
			},
			want: image.Rect(10, 20, 30, 40),
		},
		{
			name: "nearly collinear arc",
			path: func(p *vector.Path) {
				p.MoveTo(676.47327, 1502.2303)
				p.ArcTo(257.7812, 1856.779, 1046.7478, 1188.3281, 100)
			},
			want: image.Rect(676, 1188, 1047, 1503),
		},
		{
			name: "infinite line",
			path: func(p *vector.Path) {
				p.MoveTo(0, 0)
				p.LineTo(inf, inf)
			},
			want: image.Rectangle{},
		},
		{
			name: "non-finite control point",
			path: func(p *vector.Path) {
				p.MoveTo(0, 0)
				p.QuadTo(nan, nan, 10, 10)
			},
			want: image.Rectangle{},
		},
		{
			name: "non-finite sub-path with a finite sub-path",
			path: func(p *vector.Path) {
				p.MoveTo(0, 0)
				p.LineTo(nan, nan)
				p.MoveTo(100, 100)
				p.LineTo(110, 110)
			},
			want: image.Rect(100, 100, 110, 110),
		},
		{
			name: "moveTo replacing a non-finite position",
			path: func(p *vector.Path) {
				p.MoveTo(nan, nan)
				p.MoveTo(0, 0)
				p.LineTo(10, 10)
			},
			want: image.Rect(0, 0, 10, 10),
		},
		{
			name: "addPath with a non-finite path",
			path: func(p *vector.Path) {
				var src vector.Path
				src.MoveTo(0, 0)
				src.LineTo(nan, nan)
				p.AddPath(&src, nil)
			},
			want: image.Rectangle{},
		},
		{
			name: "addPath with a non-finite geoM",
			path: func(p *vector.Path) {
				var src vector.Path
				src.MoveTo(0, 0)
				src.LineTo(10, 10)
				op := &vector.AddPathOptions{}
				op.GeoM.SetElement(0, 0, math.NaN())
				p.AddPath(&src, op)
			},
			want: image.Rectangle{},
		},
		{
			name: "addStroke with a non-finite geoM",
			path: func(p *vector.Path) {
				var src vector.Path
				src.MoveTo(0, 0)
				src.LineTo(10, 0)
				op := &vector.AddStrokeOptions{}
				op.StrokeOptions.Width = 2
				op.GeoM.SetElement(0, 0, math.NaN())
				p.AddStroke(&src, op)
			},
			want: image.Rectangle{},
		},
		{
			name: "out-of-int-range coordinates clamped",
			path: func(p *vector.Path) {
				p.MoveTo(0, 0)
				p.LineTo(2e20, -2e20)
			},
			want: image.Rect(0, math.MinInt, math.MaxInt, 0),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var p vector.Path
			tc.path(&p)
			if got, want := p.Bounds(), tc.want; got != want {
				t.Errorf("got: %v, want: %v", got, want)
			}
		})
	}
}

func TestBoundsConcurrency(t *testing.T) {
	var p vector.Path
	p.MoveTo(10, 20)
	p.LineTo(30, 40)
	p.QuadTo(50, 60, 70, 80)
	p.Close()

	// Bounds must not modify the path so that one path can be shared by multiple goroutines.
	const goroutineCount = 8
	got := make([]image.Rectangle, goroutineCount)
	var wg sync.WaitGroup
	for i := range goroutineCount {
		wg.Go(func() {
			got[i] = p.Bounds()
		})
	}
	wg.Wait()

	for i, b := range got {
		if want := image.Rect(10, 20, 70, 80); b != want {
			t.Errorf("%d: got: %v, want: %v", i, b, want)
		}
	}
}

func TestStrokeMiterJoinNearlyCollinear(t *testing.T) {
	var p vector.Path
	p.MoveTo(676.47327, 1502.2303)
	p.LineTo(257.7812, 1856.779)
	p.LineTo(1046.7478, 1188.3281)

	op := &vector.AddStrokeOptions{}
	op.StrokeOptions.LineJoin = vector.LineJoinMiter
	op.StrokeOptions.MiterLimit = 4
	op.StrokeOptions.Width = 1

	var sp vector.Path
	sp.AddStroke(&p, op)

	got := sp.Bounds()
	if got, want := got, image.Rect(257, 1187, 1048, 1858); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestQuadCuspIsKept(t *testing.T) {
	var p vector.Path
	p.MoveTo(0, 0)
	p.QuadTo(10, 10, 0, 0)

	// The cusp reaches (5, 5) at t=0.5.
	if got, want := p.Bounds(), image.Rect(0, 0, 5, 5); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}

	var d vector.Path
	d.MoveTo(5, 5)
	d.QuadTo(5, 5, 5, 5)
	if b := d.Bounds(); !b.Empty() {
		t.Errorf("Bounds of a single point: got %v, want empty", b)
	}
}

func TestStrokeQuadCusp(t *testing.T) {
	var p vector.Path
	p.MoveTo(0, 0)
	p.LineTo(100, 0)
	p.QuadTo(100, 50, 100, 0)
	p.LineTo(200, 0)

	// The cusp reaches (100, 25) at its midpoint, so it is equivalent to this out-and-back path.
	var l vector.Path
	l.MoveTo(0, 0)
	l.LineTo(100, 0)
	l.LineTo(100, 25)
	l.LineTo(100, 0)
	l.LineTo(200, 0)

	for _, lineJoin := range []vector.LineJoin{vector.LineJoinMiter, vector.LineJoinBevel, vector.LineJoinRound} {
		op := &vector.AddStrokeOptions{}
		op.StrokeOptions.Width = 10
		op.StrokeOptions.LineJoin = lineJoin

		var sp vector.Path
		sp.AddStroke(&p, op)
		var lp vector.Path
		lp.AddStroke(&l, op)

		// The stroked cusp must agree with the stroked out-and-back path exactly, including the joint at the tip.
		if got, want := vector.PathOperationsString(&sp), vector.PathOperationsString(&lp); got != want {
			t.Errorf("LineJoin %d: got:\n%v\nwant:\n%v", lineJoin, got, want)
		}
	}

	// The bounds of the cusp must be kept.
	if got, want := p.Bounds(), image.Rect(0, 0, 200, 25); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestStrokeTinyQuadCusp(t *testing.T) {
	// The midpoint of this cusp is rounded back to the start point in float32, so there is nothing to stroke.
	var p vector.Path
	p.MoveTo(1e7, 0)
	p.QuadTo(1e7+1, 0, 1e7, 0)

	op := &vector.AddStrokeOptions{}
	op.StrokeOptions.Width = 4

	var sp vector.Path
	sp.AddStroke(&p, op)
	if got, want := vector.SubPathCount(&sp), 0; got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestStrokeHugeQuadCusp(t *testing.T) {
	// The midpoint of this cusp must not overflow to infinity in float32.
	var p vector.Path
	p.MoveTo(3.0e38, 1)
	p.QuadTo(3.4e38, 1, 3.0e38, 1)

	op := &vector.AddStrokeOptions{}
	op.StrokeOptions.Width = 4

	var sp vector.Path
	sp.AddStroke(&p, op)

	// AddStroke must not modify the source path.
	if got, want := vector.PathOperationsString(&p), "MoveTo(3e+38, 1)\nQuadTo(3.4e+38, 1, 3e+38, 1)\n"; got != want {
		t.Errorf("got:\n%v\nwant:\n%v", got, want)
	}

	// The cusp is equivalent to this out-and-back path, whose midpoint (3.2e38, 1) doesn't overflow to infinity.
	var l vector.Path
	l.MoveTo(3.0e38, 1)
	l.LineTo(3.2e38, 1)
	l.LineTo(3.0e38, 1)

	var lp vector.Path
	lp.AddStroke(&l, op)

	// The stroked cusp must agree with the stroked out-and-back path exactly.
	if got, want := vector.PathOperationsString(&sp), vector.PathOperationsString(&lp); got != want {
		t.Errorf("got:\n%v\nwant:\n%v", got, want)
	}
}

func TestAddStrokeAllocs(t *testing.T) {
	testCases := []struct {
		name  string
		build func(p *vector.Path)
	}{
		{
			name: "no cusp",
			build: func(p *vector.Path) {
				p.MoveTo(0, 0)
				p.LineTo(100, 0)
				p.QuadTo(100, 50, 50, 50)
				p.LineTo(0, 0)
				p.Close()
			},
		},
		{
			name: "one cusp",
			build: func(p *vector.Path) {
				p.MoveTo(0, 0)
				p.LineTo(100, 0)
				p.QuadTo(100, 50, 100, 0)
				p.LineTo(200, 0)
			},
		},
		{
			name: "cusps in multiple sub-paths",
			build: func(p *vector.Path) {
				for i := range 3 {
					x := float32(i) * 100
					p.MoveTo(x, 0)
					p.QuadTo(x+50, 50, x, 0)
					p.LineTo(x+30, 30)
					p.QuadTo(x+60, 60, x+30, 30)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			op := &vector.AddStrokeOptions{}
			op.StrokeOptions.Width = 4

			var src vector.Path
			var dst vector.Path
			// Stroking must not allocate for a path that is reset and rebuilt repeatedly.
			for _, stroke := range []vector.StrokeOptions{
				{Width: 4, LineCap: vector.LineCapButt, LineJoin: vector.LineJoinMiter, MiterLimit: 4},
				{Width: 4, LineCap: vector.LineCapRound, LineJoin: vector.LineJoinRound},
			} {
				op.StrokeOptions = stroke
				if got := testing.AllocsPerRun(10, func() {
					src.Reset()
					tc.build(&src)
					dst.Reset()
					dst.AddStroke(&src, op)
				}); got != 0 {
					t.Errorf("cap=%d join=%d allocations: got: %v, want: 0", stroke.LineCap, stroke.LineJoin, got)
				}
			}
		})
	}
}

func TestArcHugeFiniteAnglesTerminate(t *testing.T) {
	for _, tc := range []struct {
		start, end float32
		dir        vector.Direction
	}{
		{1e18, 1e18 + 1e12, vector.Clockwise},
		{1e30, 1e30 + 1e24, vector.Clockwise},
		{-1e30, -1e30 - 1e24, vector.CounterClockwise},
		{float32(math.NaN()), 1, vector.Clockwise},
		{0, float32(math.Inf(1)), vector.Clockwise},
	} {
		var p vector.Path
		p.Arc(10, 10, 5, tc.start, tc.end, tc.dir)
	}
}

func TestLineJoinRound90Degrees(t *testing.T) {
	dstRound := ggfx.NewImage(64, 64)
	defer dstRound.Deallocate()
	dstBevel := ggfx.NewImage(64, 64)
	defer dstBevel.Deallocate()

	var p vector.Path
	p.MoveTo(16, 32)
	p.LineTo(32, 32)
	p.LineTo(32, 48)

	vector.StrokePath(dstRound, &p, &vector.StrokeOptions{Width: 20, LineJoin: vector.LineJoinRound}, nil)
	vector.StrokePath(dstBevel, &p, &vector.StrokeOptions{Width: 20, LineJoin: vector.LineJoinBevel}, nil)

	// At the outer 45-degree diagonal of the 90-degree corner (around (32+8, 32-8) = (40, 24)),
	// a bevel cuts along x - y = 10*sqrt(2) ≈ 14.14 (distance 7.07 from corner), whereas a round
	// join extends to radius 10 (distance 10 from corner, covering (39, 25) at distance ~9.9? Check (38, 26) at dist 8.48).
	if got, want := dstRound.At(38, 26).(color.RGBA), (color.RGBA{0xff, 0xff, 0xff, 0xff}); got != want {
		t.Errorf("round join at (38, 26) = %v, want %v", got, want)
	}
	if got, want := dstBevel.At(38, 26).(color.RGBA), (color.RGBA{}); got != want {
		t.Errorf("bevel join at (38, 26) = %v, want %v", got, want)
	}
}

func TestAddStrokeTrailingEmptySubPath(t *testing.T) {
	var src vector.Path
	src.MoveTo(10, 10)
	src.LineTo(20, 10)

	var dst vector.Path
	dst.MoveTo(0, 0) // trailing empty sub-path
	op := &vector.AddStrokeOptions{
		StrokeOptions: vector.StrokeOptions{Width: 4},
	}
	op.GeoM.Translate(100, 200)
	dst.AddStroke(&src, op)

	if got, want := dst.Bounds(), image.Rect(110, 208, 120, 212); got != want {
		t.Errorf("AddStroke with trailing empty sub-path + GeoM: got %v, want %v", got, want)
	}

	// Self-stroke with trailing empty sub-path must not double-stroke.
	var self vector.Path
	self.MoveTo(10, 10)
	self.LineTo(20, 10)
	self.MoveTo(50, 50)
	self.AddStroke(&self, &vector.AddStrokeOptions{StrokeOptions: vector.StrokeOptions{Width: 4}})
	if got, want := vector.SubPathCount(&self), 3; got != want {
		t.Errorf("self AddStroke SubPathCount = %d, want %d", got, want)
	}
}

func TestStrokeCollinearFoldBackQuad(t *testing.T) {
	var p vector.Path
	p.MoveTo(0, 10)
	p.QuadTo(100, 10, 20, 10) // folds back at x = 500/9 ≈ 55.56

	var sp vector.Path
	sp.AddStroke(&p, &vector.AddStrokeOptions{StrokeOptions: vector.StrokeOptions{Width: 4}})
	b := sp.Bounds()
	if b.Max.X < 55 {
		t.Errorf("stroked collinear fold-back quad bounds = %v, want Max.X >= 55", b)
	}
}

func TestCubicToEmptyPath(t *testing.T) {
	var p vector.Path
	p.CubicTo(10, 20, 50, 100, 100, 20)
	// Start of the cubic must remain (10, 20), so Bounds.Min.X == 10 and Bounds.Min.Y == 20.
	b := p.Bounds()
	if b.Min.X != 10 || b.Min.Y != 20 {
		t.Errorf("CubicTo on empty path bounds = %v, want Min=(10, 20)", b)
	}
}

func TestStrokeCubicArcNoSpikes(t *testing.T) {
	buildCubicArc := func(cx, cy, r, start, end float64) vector.Path {
		var p vector.Path
		sweep := end - start
		at := func(a float64) (float32, float32) {
			return float32(cx + r*math.Cos(a)), float32(cy + r*math.Sin(a))
		}
		x0, y0 := at(start)
		p.MoveTo(x0, y0)
		n := max(1, int(math.Ceil(math.Abs(sweep)/(math.Pi/2)-1e-9)))
		step := sweep / float64(n)
		k := 4.0 / 3 * math.Tan(step/4) * r
		for i := range n {
			a0, a1 := start+step*float64(i), start+step*float64(i+1)
			sx, sy := cx+r*math.Cos(a0), cy+r*math.Sin(a0)
			ex, ey := cx+r*math.Cos(a1), cy+r*math.Sin(a1)
			p.CubicTo(
				float32(sx-k*math.Sin(a0)), float32(sy+k*math.Cos(a0)),
				float32(ex+k*math.Sin(a1)), float32(ey-k*math.Cos(a1)),
				float32(ex), float32(ey),
			)
		}
		return p
	}

	centers := [][2]float64{{20, 20}, {600, 400}, {1200, 800}, {4000, 3000}}
	fractions := []float64{0.25, 0.5, 0.68, 0.75, 0.99, 1.0}
	joins := []vector.LineJoin{vector.LineJoinRound, vector.LineJoinMiter, vector.LineJoinBevel}
	caps := []vector.LineCap{vector.LineCapButt, vector.LineCapRound, vector.LineCapSquare}

	const (
		r     = 10.0
		width = 4.0
	)
	for _, c := range centers {
		cx, cy := c[0], c[1]
		allow := image.Rect(
			int(math.Floor(cx-r-width-2)),
			int(math.Floor(cy-r-width-2)),
			int(math.Ceil(cx+r+width+2)),
			int(math.Ceil(cy+r+width+2)),
		)
		for _, frac := range fractions {
			src := buildCubicArc(cx, cy, r, -math.Pi/2, -math.Pi/2+2*math.Pi*frac)
			for _, lj := range joins {
				for _, lc := range caps {
					var sp vector.Path
					sp.AddStroke(&src, &vector.AddStrokeOptions{
						StrokeOptions: vector.StrokeOptions{
							Width:      width,
							LineJoin:   lj,
							LineCap:    lc,
							MiterLimit: 4,
						},
					})
					if got := sp.Bounds(); !got.In(allow) {
						t.Errorf("center=(%.0f,%.0f) frac=%.2f join=%d cap=%d: bounds %v exceeded %v", cx, cy, frac, lj, lc, got, allow)
					}
				}
			}
		}
	}
}

func TestStrokeNearlyTangentJoinLargeCoords(t *testing.T) {
	angles := []float64{1e-4, 5e-4, 1e-3, 5e-3, 1e-2, 5e-2, 0.1}
	joins := []vector.LineJoin{vector.LineJoinRound, vector.LineJoinMiter}
	const (
		cx, cy = 4000.0, 3000.0
		segLen = 40.0
		width  = 8.0
	)
	allow := image.Rect(
		int(cx-segLen-width-2),
		int(cy-segLen-width-2),
		int(cx+segLen+width+2),
		int(cy+segLen+width+2),
	)
	for _, da := range angles {
		for _, sign := range []float64{-1, 1} {
			var src vector.Path
			src.MoveTo(float32(cx-segLen), float32(cy))
			src.LineTo(float32(cx), float32(cy))
			src.LineTo(float32(cx+segLen*math.Cos(sign*da)), float32(cy+segLen*math.Sin(sign*da)))
			for _, lj := range joins {
				var sp vector.Path
				sp.AddStroke(&src, &vector.AddStrokeOptions{
					StrokeOptions: vector.StrokeOptions{
						Width:      width,
						LineJoin:   lj,
						MiterLimit: 4,
					},
				})
				if got := sp.Bounds(); !got.In(allow) {
					t.Errorf("da=%g sign=%.0f join=%d: bounds %v exceeded %v", da, sign, lj, got, allow)
				}
			}
		}
	}
}

func TestStrokeRoundCapRendering(t *testing.T) {
	dstRound := ggfx.NewImage(64, 64)
	defer dstRound.Deallocate()
	dstButt := ggfx.NewImage(64, 64)
	defer dstButt.Deallocate()
	dstSquare := ggfx.NewImage(64, 64)
	defer dstSquare.Deallocate()

	var p vector.Path
	p.MoveTo(20, 32)
	p.LineTo(44, 32)

	vector.StrokePath(dstRound, &p, &vector.StrokeOptions{Width: 20, LineCap: vector.LineCapRound}, nil)
	vector.StrokePath(dstButt, &p, &vector.StrokeOptions{Width: 20, LineCap: vector.LineCapButt}, nil)
	vector.StrokePath(dstSquare, &p, &vector.StrokeOptions{Width: 20, LineCap: vector.LineCapSquare}, nil)

	// Center of end cap (51, 32) and start cap (13, 32) are inside round & square caps, outside butt cap.
	for _, pt := range []image.Point{{51, 32}, {13, 32}} {
		if got, want := dstRound.At(pt.X, pt.Y).(color.RGBA), (color.RGBA{0xff, 0xff, 0xff, 0xff}); got != want {
			t.Errorf("round cap at %v = %v, want %v", pt, got, want)
		}
		if got, want := dstButt.At(pt.X, pt.Y).(color.RGBA), (color.RGBA{}); got != want {
			t.Errorf("butt cap at %v = %v, want %v", pt, got, want)
		}
	}
	// Outer corner of square cap (52, 23) and (12, 23) (dist ~12.0 from endpoint > r=10) must be outside round cap, inside square cap.
	for _, pt := range []image.Point{{52, 23}, {12, 23}} {
		if got, want := dstRound.At(pt.X, pt.Y).(color.RGBA), (color.RGBA{}); got != want {
			t.Errorf("round cap corner at %v = %v, want %v", pt, got, want)
		}
		if got, want := dstSquare.At(pt.X, pt.Y).(color.RGBA), (color.RGBA{0xff, 0xff, 0xff, 0xff}); got != want {
			t.Errorf("square cap corner at %v = %v, want %v", pt, got, want)
		}
	}
}

func TestStrokeCubicArcRenderedNoSpikes(t *testing.T) {
	// Render the 68% 3-cubic arc with LineJoinRound on an image and verify no stray pixels appear outside the ring.
	const (
		w, h  = 128, 128
		cx    = 64.0
		cy    = 64.0
		r     = 10.0
		width = 4.0
	)
	var p vector.Path
	start := -math.Pi / 2
	sweep := 2 * math.Pi * 0.68
	n := 3
	step := sweep / float64(n)
	k := 4.0 / 3 * math.Tan(step/4) * r
	p.MoveTo(float32(cx+r*math.Cos(start)), float32(cy+r*math.Sin(start)))
	for i := range n {
		a0, a1 := start+step*float64(i), start+step*float64(i+1)
		sx, sy := cx+r*math.Cos(a0), cy+r*math.Sin(a0)
		ex, ey := cx+r*math.Cos(a1), cy+r*math.Sin(a1)
		p.CubicTo(
			float32(sx-k*math.Sin(a0)), float32(sy+k*math.Cos(a0)),
			float32(ex+k*math.Sin(a1)), float32(ey-k*math.Cos(a1)),
			float32(ex), float32(ey),
		)
	}

	dst := ggfx.NewImage(w, h)
	defer dst.Deallocate()
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(color.White)
	vector.StrokePath(dst, &p, &vector.StrokeOptions{Width: width, LineJoin: vector.LineJoinRound}, op)

	maxR := r + width/2 + 2
	var litCount int
	for y := range h {
		for x := range w {
			c := dst.At(x, y).(color.RGBA)
			if c.A == 0 {
				continue
			}
			litCount++
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			if dist := math.Hypot(dx, dy); dist > maxR {
				t.Fatalf("stray pixel at (%d, %d) with color %v (dist=%.2f > maxR=%.2f)", x, y, c, dist, maxR)
			}
		}
	}
	if litCount == 0 {
		t.Fatalf("expected non-empty rendered ring")
	}
}

func TestAddStrokeNumericEdgeCases(t *testing.T) {
	nan := float32(math.NaN())
	posInf := float32(math.Inf(1))
	negInf := float32(math.Inf(-1))

	t.Run("non-finite or non-positive width", func(t *testing.T) {
		var src vector.Path
		src.MoveTo(10, 10)
		src.LineTo(30, 10)

		for _, w := range []float32{0, -2, nan, posInf, negInf} {
			var sp vector.Path
			sp.AddStroke(&src, &vector.AddStrokeOptions{
				StrokeOptions: vector.StrokeOptions{Width: w},
			})
			if got := vector.SubPathCount(&sp); got != 0 {
				t.Errorf("Width=%v: SubPathCount = %d, want 0", w, got)
			}
		}

		dst := ggfx.NewImage(32, 32)
		defer dst.Deallocate()
		for _, w := range []float32{nan, posInf} {
			vector.StrokePath(dst, &src, &vector.StrokeOptions{Width: w}, nil)
		}
		if got, want := dst.At(20, 10).(color.RGBA), (color.RGBA{}); got != want {
			t.Errorf("StrokePath non-finite width drew %v, want transparent", got)
		}
	})

	t.Run("non-finite source sub-path skipped", func(t *testing.T) {
		var src vector.Path
		src.MoveTo(0, 0)
		src.LineTo(nan, 10)
		src.MoveTo(10, 10)
		src.LineTo(20, 10)
		src.MoveTo(0, 0)
		src.QuadTo(posInf, 0, 10, 10)

		var sp vector.Path
		sp.AddStroke(&src, &vector.AddStrokeOptions{
			StrokeOptions: vector.StrokeOptions{Width: 4},
		})
		if got, want := vector.SubPathCount(&sp), 1; got != want {
			t.Errorf("SubPathCount = %d, want %d (only valid sub-path stroked)", got, want)
		}
		if got, want := sp.Bounds(), image.Rect(10, 8, 20, 12); got != want {
			t.Errorf("Bounds = %v, want %v", got, want)
		}
	})

	t.Run("miter limit NaN and Inf", func(t *testing.T) {
		// Sharp V-bend where a miter would extend far past the corner.
		var p vector.Path
		p.MoveTo(10, 10)
		p.LineTo(50, 10)
		p.LineTo(10, 12)

		var bevel, nanMiter vector.Path
		bevel.AddStroke(&p, &vector.AddStrokeOptions{
			StrokeOptions: vector.StrokeOptions{Width: 4, LineJoin: vector.LineJoinBevel},
		})
		nanMiter.AddStroke(&p, &vector.AddStrokeOptions{
			StrokeOptions: vector.StrokeOptions{Width: 4, LineJoin: vector.LineJoinMiter, MiterLimit: nan},
		})
		if got, want := vector.PathOperationsString(&nanMiter), vector.PathOperationsString(&bevel); got != want {
			t.Errorf("NaN MiterLimit should bevel:\ngot:\n%s\nwant:\n%s", got, want)
		}

		// 180-degree reversal with +Inf MiterLimit must bevel rather than divide by zero.
		var rev vector.Path
		rev.MoveTo(10, 10)
		rev.LineTo(50, 10)
		rev.LineTo(10, 10)
		var infMiter vector.Path
		infMiter.AddStroke(&rev, &vector.AddStrokeOptions{
			StrokeOptions: vector.StrokeOptions{Width: 4, LineJoin: vector.LineJoinMiter, MiterLimit: posInf},
		})
		if got, want := infMiter.Bounds(), image.Rect(10, 8, 50, 12); got != want {
			t.Errorf("180-deg +Inf MiterLimit bounds = %v, want %v", got, want)
		}
	})

	t.Run("collinear large-coord fold-back quad", func(t *testing.T) {
		var p vector.Path
		p.MoveTo(0, 0)
		p.QuadTo(2e20, 2e20, 1e20, 1e20)

		op := &vector.AddStrokeOptions{
			StrokeOptions: vector.StrokeOptions{Width: 4},
		}
		var sp vector.Path
		sp.AddStroke(&p, op)

		// The fold-back is equivalent to this out-and-back two-line path peaking at t = 2/3.
		tip := float32((4.0 / 3.0) * float64(float32(1e20)))
		var l vector.Path
		l.MoveTo(0, 0)
		l.LineTo(tip, tip)
		l.LineTo(1e20, 1e20)

		var lp vector.Path
		lp.AddStroke(&l, op)

		if got, want := vector.PathOperationsString(&sp), vector.PathOperationsString(&lp); got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("subnormal near-collinear fold-back quad", func(t *testing.T) {
		// Along y = 0 the subnormal offset survives normalization and halves to 0 inside
		// doAppendParalleledQuad, hitting the collinear fold-back bevel connector.
		// Tip is at x = 62.5 (t = 0.625) while the first De Casteljau split is at x = 60.
		var p vector.Path
		p.MoveTo(0, 0)
		p.QuadTo(100, math.Float32frombits(1), 40, 0)

		op := &vector.AddStrokeOptions{
			StrokeOptions: vector.StrokeOptions{Width: 20},
		}
		op.GeoM.Translate(10, 32)
		var sp vector.Path
		sp.AddStroke(&p, op)

		dst := ggfx.NewImage(80, 64)
		defer dst.Deallocate()
		vector.FillPath(dst, &sp, nil, nil)
		// Without the bevel connector at the tip (x = 72.5 after translate), both forward and
		// reverse passes cut diagonally from (72.5, 42) back toward (50, 22) and (70, 22),
		// leaving the upper-right tip corner (71, 24) unfilled.
		for _, pt := range []image.Point{{71, 24}, {71, 32}, {71, 40}, {50, 32}} {
			if got, want := dst.At(pt.X, pt.Y).(color.RGBA), (color.RGBA{0xff, 0xff, 0xff, 0xff}); got != want {
				t.Errorf("pixel %v = %v, want %v", pt, got, want)
			}
		}
	})

	t.Run("float32-collinear hairpin keeps round join", func(t *testing.T) {
		// In float32 both cross-product terms round to 20000 so the hairpin splits at its tip
		// (~(133.33, 133.33)) and receives a round join.
		var p vector.Path
		p.MoveTo(0, 0)
		p.QuadTo(200, 200.0244140625, 99.98779296875, 100)

		var sp vector.Path
		sp.AddStroke(&p, &vector.AddStrokeOptions{
			StrokeOptions: vector.StrokeOptions{Width: 20, LineJoin: vector.LineJoinRound},
		})
		if got, want := sp.Bounds(), image.Rect(-8, -8, 144, 144); got != want {
			t.Errorf("Bounds = %v, want %v", got, want)
		}
	})

	t.Run("float32-collinear monotonic quad normalized to line", func(t *testing.T) {
		// Both cross-product terms round to 20000 in float32, so the monotonic quad normalizes to a LineTo.
		var p vector.Path
		p.MoveTo(0, 0)
		p.QuadTo(100.01220703125, 100, 200, 199.9755859375)

		op := &vector.AddStrokeOptions{
			StrokeOptions: vector.StrokeOptions{Width: 10},
		}
		var sp vector.Path
		sp.AddStroke(&p, op)

		var l vector.Path
		l.MoveTo(0, 0)
		l.LineTo(200, 199.9755859375)
		var lp vector.Path
		lp.AddStroke(&l, op)

		got := vector.PathOperationsString(&sp)
		if strings.Contains(got, "QuadTo") {
			t.Errorf("expected quad to normalize to lines, got:\n%s", got)
		}
		if want := vector.PathOperationsString(&lp); got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("huge quad control point does not overflow to NaN", func(t *testing.T) {
		var p vector.Path
		p.MoveTo(2e38, 0)
		p.QuadTo(2e38, 2e38, 0, 2e38)

		var sp vector.Path
		sp.AddStroke(&p, &vector.AddStrokeOptions{
			StrokeOptions: vector.StrokeOptions{Width: 4},
		})
		if got := vector.SubPathCount(&sp); got != 1 {
			t.Errorf("SubPathCount = %d, want 1", got)
		}
		ops := vector.PathOperationsString(&sp)
		if ops == "" || strings.Contains(ops, "NaN") || strings.Contains(ops, "Inf") {
			t.Errorf("expected finite stroke operations, got:\n%s", ops)
		}
	})

	t.Run("huge diagonal vector norm does not overflow to zero", func(t *testing.T) {
		var p vector.Path
		p.MoveTo(3e38, 0)
		p.LineTo(0, 3e38)

		var sp vector.Path
		sp.AddStroke(&p, &vector.AddStrokeOptions{
			StrokeOptions: vector.StrokeOptions{Width: 1e37},
		})
		// With float32 Hypot overflow, norm() returned (0, 0) and collapsed the stroke onto the line.
		if got := vector.PathOperationsString(&sp); got == "" {
			t.Errorf("expected non-empty stroked path")
		}
		// Start offset should move away from (3e38, 0) in X/Y by ~3.5e36.
		if pos, ok := vector.CurrentPosition(&sp); !ok || pos.X == 3e38 || pos.Y == 0 {
			t.Errorf("expected non-zero normal offset from (3e38, 0), got %v (ok=%t)", pos, ok)
		}
	})
}
