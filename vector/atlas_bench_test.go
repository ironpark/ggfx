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
	"image/color"
	"math/rand/v2"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/vector"
)

// benchmarkFill fills paths small paths, and a large one every largeEvery iterations if it is
// positive, per iteration; a frame ends every 16 iterations.
func benchmarkFill(b *testing.B, paths int, largeEvery int, antialias bool) {
	const w, h = 1440, 900
	dst := ggfx.NewImage(w, h)
	defer dst.Deallocate()
	sink := ggfx.NewImage(16, 16)
	defer sink.Deallocate()

	r := rand.New(rand.NewPCG(1, 2))
	ps := make([]*vector.Path, paths)
	for i := range ps {
		ps[i] = gridPolygon(r, 20+r.Float32()*(w-40), 20+r.Float32()*(h-40), 8+r.Float32()*12, 3+r.IntN(8))
	}
	var large vector.Path
	large.MoveTo(0, h)
	for x := float32(0); x <= w; x += 16 {
		large.LineTo(x, h/2+float32(r.IntN(h/2)))
	}
	large.LineTo(w, h)
	large.Close()

	fo := &vector.FillOptions{}
	do := &vector.DrawPathOptions{AntiAlias: antialias}
	do.ColorScale.ScaleWithColor(color.NRGBA{0x40, 0x80, 0xc0, 0xff})

	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		for _, p := range ps {
			vector.FillPath(dst, p, fo, do)
		}
		if largeEvery > 0 && i%largeEvery == 0 {
			vector.FillPath(dst, &large, fo, do)
		}
		// Using dst runs the fills.
		sink.DrawImage(dst, nil)
		if i%16 == 15 {
			if err := vector.NextFrame(); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkFillSubImages fills paths as a UI does: widgets, each drawn to its own clipped sub-image
// of the destination one after another, with a rounded background, a few small shapes and a
// stroked border, some overhanging the widget.
func BenchmarkFillSubImages(b *testing.B) {
	const w, h = 1440, 900
	dst := ggfx.NewImage(w, h)
	defer dst.Deallocate()
	sink := ggfx.NewImage(16, 16)
	defer sink.Deallocate()

	r := rand.New(rand.NewPCG(1, 2))
	type widget struct {
		sub    *ggfx.Image
		bg     vector.Path
		shapes []*vector.Path
	}
	widgets := make([]widget, 24)
	for i := range widgets {
		rect := randomWidgetRect(r, w, h)
		wg := &widgets[i]
		wg.sub = dst.SubImage(rect).(*ggfx.Image)
		fx, fy, fw, fh := float32(rect.Min.X), float32(rect.Min.Y), float32(rect.Dx()), float32(rect.Dy())
		addRoundedRect(&wg.bg, fx, fy, fw, fh, 6)
		for range 4 {
			// Some shapes overhang the widget and are clipped.
			cx, cy := fx-8+r.Float32()*(fw+16), fy-8+r.Float32()*(fh+16)
			wg.shapes = append(wg.shapes, gridPolygon(r, cx, cy, 4+r.Float32()*8, 3+r.IntN(6)))
		}
	}

	fo := &vector.FillOptions{}
	do := &vector.DrawPathOptions{AntiAlias: true}
	do.ColorScale.ScaleWithColor(color.NRGBA{0x40, 0x80, 0xc0, 0xff})
	so := &vector.StrokeOptions{Width: 1, LineJoin: vector.LineJoinRound}

	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		for k := range widgets {
			wg := &widgets[k]
			vector.FillPath(wg.sub, &wg.bg, fo, do)
			for _, p := range wg.shapes {
				vector.FillPath(wg.sub, p, fo, do)
			}
			vector.StrokePath(wg.sub, &wg.bg, so, do)
		}
		// Using dst runs the fills.
		sink.DrawImage(dst, nil)
		if i%16 == 15 {
			if err := vector.NextFrame(); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkFillSmall(b *testing.B)      { benchmarkFill(b, 100, 0, true) }
func BenchmarkFillSmallNoAA(b *testing.B)  { benchmarkFill(b, 100, 0, false) }
func BenchmarkFillMixed(b *testing.B)      { benchmarkFill(b, 100, 2, true) }
func BenchmarkFillLargeEvery(b *testing.B) { benchmarkFill(b, 0, 1, true) }
