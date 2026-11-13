// Copyright 2026 The ggfx Authors
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
	"math/rand/v2"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/vector"
)

// uiFrame is a frame of a UI as ggui draws it: widgets, each on its own clipped sub-image, with a
// rounded background, a stroked border and a few shapes, built anew every frame, and an image
// drawn to the destination after every few widgets, which flushes the fills so far.
type uiFrame struct {
	dst     *ggfx.Image
	icon    *ggfx.Image
	widgets []uiWidget
	p, s    vector.Path
}

type uiWidget struct {
	sub        *ggfx.Image
	x, y, w, h float32
	shapes     [][3]float32
}

const uiWidgetsPerFlush = 6

// randomWidgetRect returns the bounds of a widget at a random place in a destination of size w*h.
func randomWidgetRect(r *rand.Rand, w, h int) image.Rectangle {
	x, y := r.IntN(w-200), r.IntN(h-120)
	ww, wh := 60+r.IntN(140), 24+r.IntN(96)
	return image.Rect(x, y, x+ww, y+wh)
}

// addRoundedRect adds a rectangle with corners rounded by rad to p, as a widget's background.
func addRoundedRect(p *vector.Path, x, y, w, h, rad float32) {
	p.MoveTo(x+rad, y)
	p.LineTo(x+w-rad, y)
	p.ArcTo(x+w, y, x+w, y+rad, rad)
	p.LineTo(x+w, y+h-rad)
	p.ArcTo(x+w, y+h, x+w-rad, y+h, rad)
	p.LineTo(x+rad, y+h)
	p.ArcTo(x, y+h, x, y+h-rad, rad)
	p.LineTo(x, y+rad)
	p.ArcTo(x, y, x+rad, y, rad)
	p.Close()
}

func newUIFrame(dst *ggfx.Image, n int) *uiFrame {
	f := &uiFrame{dst: dst, icon: ggfx.NewImage(16, 16)}
	f.icon.Fill(color.White)
	r := rand.New(rand.NewPCG(5, 6))
	b := dst.Bounds()
	for range n {
		rect := randomWidgetRect(r, b.Dx(), b.Dy())
		x, y, w, h := rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy()
		wg := uiWidget{
			sub: dst.SubImage(rect).(*ggfx.Image),
			x:   float32(x), y: float32(y), w: float32(w), h: float32(h),
		}
		for range 2 {
			wg.shapes = append(wg.shapes, [3]float32{float32(x) + 4 + r.Float32()*float32(w-8), float32(y) + 4 + r.Float32()*float32(h-8), 2 + r.Float32()*6})
		}
		f.widgets = append(f.widgets, wg)
	}
	return f
}

func (f *uiFrame) dispose() {
	f.icon.Deallocate()
}

// draw draws the frame with every coordinate moved by d.
func (f *uiFrame) draw(d float32) {
	do := &vector.DrawPathOptions{AntiAlias: true}
	do.ColorScale.ScaleWithColor(color.NRGBA{0x40, 0x80, 0xc0, 0xff})
	so := &vector.StrokeOptions{Width: 1, LineJoin: vector.LineJoinRound}
	for i := range f.widgets {
		wg := &f.widgets[i]
		// The background is inset, so that it and its border stay in the sub-image while d < 0.5.
		x, y, w, h := wg.x+1+d, wg.y+1+d, wg.w-2, wg.h-2
		f.p.Reset()
		addRoundedRect(&f.p, x, y, w, h, 6)
		vector.FillPath(wg.sub, &f.p, nil, do)
		vector.StrokePath(wg.sub, &f.p, so, do)
		for _, s := range wg.shapes {
			f.s.Reset()
			f.s.MoveTo(s[0]+d, s[1]+d-s[2])
			f.s.LineTo(s[0]+d+s[2], s[1]+d+s[2])
			f.s.LineTo(s[0]+d-s[2], s[1]+d+s[2])
			f.s.Close()
			vector.FillPath(wg.sub, &f.s, nil, do)
		}
		if i%uiWidgetsPerFlush == uiWidgetsPerFlush-1 {
			op := &ggfx.DrawImageOptions{}
			op.GeoM.Translate(float64(x), float64(y))
			f.dst.DrawImage(f.icon, op)
		}
	}
}

// benchmarkUIFrame draws a UI frame per iteration. If changing is true, every path moves by a
// fraction of a pixel, less than a half, every frame, so no path is ever the same as one drawn before.
func benchmarkUIFrame(b *testing.B, changing bool) {
	const w, h = 1440, 900
	dst := ggfx.NewImage(w, h)
	defer dst.Deallocate()
	sink := ggfx.NewImage(16, 16)
	defer sink.Deallocate()
	f := newUIFrame(dst, 36)
	defer f.dispose()

	b.ReportAllocs()
	before, beforeSwitches := vector.DrawCallCount(), vector.RenderTargetSwitchCount()
	var frames int
	for i := 0; b.Loop(); i++ {
		var d float32
		if changing {
			d = float32(i%997) / 1994
		}
		f.draw(d)
		sink.DrawImage(dst, nil)
		if err := vector.NextFrame(); err != nil {
			b.Fatal(err)
		}
		frames++
	}
	b.ReportMetric(float64(vector.DrawCallCount()-before)/float64(frames), "draws/frame")
	b.ReportMetric(float64(vector.RenderTargetSwitchCount()-beforeSwitches)/float64(frames), "switches/frame")
}

func BenchmarkFillUIFrameSteady(b *testing.B)   { benchmarkUIFrame(b, false) }
func BenchmarkFillUIFrameChanging(b *testing.B) { benchmarkUIFrame(b, true) }

// BenchmarkFillUIFrameSteadyNoCache is BenchmarkFillUIFrameSteady without the stencil cache.
func BenchmarkFillUIFrameSteadyNoCache(b *testing.B) {
	vector.SetStencilCacheDisabled(true)
	defer vector.SetStencilCacheDisabled(false)
	benchmarkUIFrame(b, false)
}
