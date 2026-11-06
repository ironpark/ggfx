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
// positive, per iteration; the queue is flushed every 16 iterations, as frames would.
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
			_ = sink.At(0, 0)
		}
	}
}

func BenchmarkFillSmall(b *testing.B)      { benchmarkFill(b, 100, 0, true) }
func BenchmarkFillSmallNoAA(b *testing.B)  { benchmarkFill(b, 100, 0, false) }
func BenchmarkFillMixed(b *testing.B)      { benchmarkFill(b, 100, 2, true) }
func BenchmarkFillLargeEvery(b *testing.B) { benchmarkFill(b, 0, 1, true) }
