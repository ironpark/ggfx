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
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/vector"
)

// cacheScenes are scenes whose paths the stencil cache sees again: in later flushes of a frame and
// in later frames, moved by whole pixels and by fractions, changed in place, and more of them than
// the cache holds.
var cacheScenes = []batchScene{
	{"frames", 260, 220, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		dst.Fill(color.RGBA{0x20, 0x20, 0x28, 0xff})
		icon := ggfx.NewImage(8, 8)
		defer icon.Deallocate()
		icon.Fill(color.RGBA{0x80, 0x80, 0x80, 0x80})
		sub := dst.SubImage(image.Rect(30, 20, 200, 170)).(*ggfx.Image)
		type shape struct {
			pts  []float32
			clr  color.Color
			quad bool
		}
		var shapes []shape
		for range 24 {
			cx, cy, rad := 40+r.Float32()*140, 40+r.Float32()*120, 4+r.Float32()*30
			var pts []float32
			for range 3 + r.IntN(6) {
				pts = append(pts, cx+(r.Float32()*2-1)*rad, cy+(r.Float32()*2-1)*rad)
			}
			shapes = append(shapes, shape{pts, randomColor(r), r.IntN(2) == 0})
		}
		build := func(p *vector.Path, s shape, dx, dy float32) {
			p.Reset()
			p.MoveTo(s.pts[0]+dx, s.pts[1]+dy)
			for k := 2; k+3 < len(s.pts); k += 4 {
				if s.quad {
					p.QuadTo(s.pts[k]+dx, s.pts[k+1]+dy, s.pts[k+2]+dx, s.pts[k+3]+dy)
				} else {
					p.LineTo(s.pts[k]+dx, s.pts[k+1]+dy)
					p.LineTo(s.pts[k+2]+dx, s.pts[k+3]+dy)
				}
			}
			p.Close()
		}
		var mutated vector.Path
		mutated.MoveTo(100, 100)
		mutated.LineTo(150, 110)
		mutated.LineTo(120, 160)
		mutated.Close()
		for frame := range 5 {
			var p vector.Path
			for i, s := range shapes {
				var dx, dy float32
				switch i % 4 {
				case 1:
					dx, dy = float32(frame*7), float32(frame*3)
				case 2:
					dx = float32(frame) * 0.25
				case 3:
					dx, dy = float32(frame%2)*13, float32(frame%3)*-9
				}
				build(&p, s, dx, dy)
				d := dst
				if i%3 == 0 {
					d = sub
				}
				fillPath(d, &p, s.clr, aa, rule, ggfx.BlendSourceOver)
				if i%5 == 4 {
					// Drawing to the destination flushes the fills so far.
					op := &ggfx.DrawImageOptions{}
					op.GeoM.Translate(float64(i*9), float64(frame*11))
					dst.DrawImage(icon, op)
				}
			}
			fillPath(dst, &mutated, color.RGBA{0xff, 0x40, 0x40, 0xc0}, aa, rule, ggfx.BlendSourceOver)
			// The same Path, changed after it was filled.
			mutated.LineTo(90+float32(frame*20), 180-float32(frame*5))
			mutated.Close()
			if err := vector.NextFrame(); err != nil {
				panic(err)
			}
		}
	}},
	{"strokes", 220, 200, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		const w, h = 220, 200
		type stroke struct {
			x, y, x1, y1, x2, y2, rad, width float32
		}
		var strokes []stroke
		for range 20 {
			strokes = append(strokes, stroke{r.Float32() * w, r.Float32() * h, r.Float32() * w, r.Float32() * h, r.Float32() * w, r.Float32() * h, 3 + r.Float32()*20, 0.5 + r.Float32()*5})
		}
		for frame := range 3 {
			for i, s := range strokes {
				var p vector.Path
				d := float32(frame * 5 * (i % 2))
				p.MoveTo(s.x+d, s.y)
				p.ArcTo(s.x1+d, s.y1, s.x2+d, s.y2, s.rad)
				p.Arc(s.x2+d, s.y2, s.rad, 0, math.Pi*1.5, vector.Clockwise)
				op := &vector.DrawPathOptions{AntiAlias: aa}
				op.ColorScale.ScaleWithColor(randomColor(r))
				vector.StrokePath(dst, &p, &vector.StrokeOptions{Width: s.width, LineJoin: vector.LineJoinRound, LineCap: vector.LineCapRound}, op)
			}
			if err := vector.NextFrame(); err != nil {
				panic(err)
			}
		}
	}},
	// Large distinct paths in several flushes per frame, more than the cache holds, over two
	// frames whose first flushes repeat.
	{"evict", 300, 300, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		icon := ggfx.NewImage(8, 8)
		defer icon.Deallocate()
		icon.Fill(color.White)
		seed := r.Uint64()
		for frame := range 2 {
			for flush := range 4 {
				rr := rand.New(rand.NewPCG(seed, uint64(flush%2+frame*flush)))
				for range 70 {
					fillPath(dst, gridPolygon(rr, 150+rr.Float32()*10, 150+rr.Float32()*10, 80+rr.Float32()*70, 3+rr.IntN(20)), randomColor(rr), aa, rule, ggfx.BlendSourceOver)
				}
				op := &ggfx.DrawImageOptions{}
				op.GeoM.Translate(float64(flush*20), float64(frame*20))
				dst.DrawImage(icon, op)
			}
			if err := vector.NextFrame(); err != nil {
				panic(err)
			}
		}
	}},
}

// TestStencilCacheIsExact renders scenes three times in a row, reusing the cached stencil buffers, and
// again with every cached buffer cleared and drawn anew where it is: the pixels must be the same.
// (A path's pixels may depend on where its buffers are by a rounding error, as they do on the
// transient atlas, so the reference draws them at the same places.)
func TestStencilCacheIsExact(t *testing.T) {
	scenes := append(append([]batchScene{}, gridScenes...), cacheScenes...)
	for _, s := range scenes {
		for _, aa := range []bool{false, true} {
			for _, rule := range []vector.FillRule{vector.FillRuleNonZero, vector.FillRuleEvenOdd} {
				t.Run(fmt.Sprintf("%s/aa=%t/rule=%d", s.name, aa, rule), func(t *testing.T) {
					render := func(redrawn bool) []byte {
						vector.ResetStencilCache()
						vector.SetStencilCacheRedrawn(redrawn)
						defer vector.SetStencilCacheRedrawn(false)
						dst := ggfx.NewImage(s.w, s.h)
						defer dst.Deallocate()
						// The paths are cached the second time, and reused the third.
						for range 3 {
							s.draw(dst, rand.New(rand.NewPCG(1, 2)), aa, rule)
							if err := vector.NextFrame(); err != nil {
								t.Fatal(err)
							}
						}
						pix := make([]byte, 4*s.w*s.h)
						dst.ReadPixels(pix)
						return pix
					}
					if diffs := diffPixels(render(false), render(true), s.w, 5); len(diffs) > 0 {
						t.Errorf("reused and redrawn stencil buffers render different pixels: %v", diffs)
					}
				})
			}
		}
	}
}

// TestStencilCacheSteadyState checks that paths filled again, or moved by whole pixels, take only
// their colors: one draw call per flush, and no other render target than the destination.
func TestStencilCacheSteadyState(t *testing.T) {
	vector.ResetStencilCache()
	dst := ggfx.NewImage(400, 300)
	defer dst.Deallocate()
	r := rand.New(rand.NewPCG(7, 8))
	type item struct {
		sub  *ggfx.Image
		x, y float32
		seed uint64
	}
	var items []item
	for range 30 {
		x, y := r.IntN(300), r.IntN(200)
		items = append(items, item{dst.SubImage(image.Rect(x, y, x+100, y+100)).(*ggfx.Image), float32(x) + 50, float32(y) + 50, r.Uint64()})
	}
	fill := func(dx, dy float32) (draws, switches int) {
		d0, s0 := vector.DrawCallCount(), vector.RenderTargetSwitchCount()
		for _, it := range items {
			rr := rand.New(rand.NewPCG(it.seed, 0))
			fillPath(it.sub, gridCurves(rr, it.x+dx, it.y+dy, 20, true), color.White, true, vector.FillRuleNonZero, ggfx.BlendSourceOver)
		}
		dst.At(0, 0)
		return vector.DrawCallCount() - d0, vector.RenderTargetSwitchCount() - s0
	}
	// The paths are cached the second time they are filled.
	if d, s := fill(0, 0); d < 3 || s != 2 {
		t.Errorf("first fill: got %d draw calls and %d render target switches, want at least 3 and 2", d, s)
	}
	if d, s := fill(0, 0); d < 3 || s < 2 {
		t.Errorf("second fill: got %d draw calls and %d render target switches, want at least 3 and 2", d, s)
	}
	for _, tc := range []struct {
		name   string
		dx, dy float32
	}{
		{"again", 0, 0},
		{"next frame", 0, 0},
		{"moved by whole pixels", 3, -2},
	} {
		if tc.name == "next frame" {
			if err := vector.NextFrame(); err != nil {
				t.Fatal(err)
			}
		}
		if d, s := fill(tc.dx, tc.dy); d != 1 || s != 0 {
			t.Errorf("%s: got %d draw calls and %d render target switches, want 1 and 0", tc.name, d, s)
		}
	}
	// Moved by a fraction, every path is new, and goes to the transient atlas.
	if d, s := fill(0.5, 0); d < 3 || s != 2 {
		t.Errorf("moved by a fraction: got %d draw calls and %d render target switches, want at least 3 and 2", d, s)
	}
}

// TestStencilCacheBounds fills more large paths than the cache holds, and checks its size, and that
// it gives its pages back once no fill uses them.
func TestStencilCacheBounds(t *testing.T) {
	vector.ResetStencilCache()
	dst := ggfx.NewImage(600, 600)
	defer dst.Deallocate()
	r := rand.New(rand.NewPCG(9, 10))
	bytes := func() int64 {
		var n int64
		for _, s := range vector.StencilCachePageSizes() {
			n += 4 * int64(s.X) * int64(s.Y)
		}
		return n
	}
	for range 6 {
		// Each path is filled twice, which caches it.
		seed := r.Uint64()
		for range 2 {
			rr := rand.New(rand.NewPCG(seed, 0))
			for range 100 {
				fillPath(dst, gridPolygon(rr, 300, 300, 150+rr.Float32()*140, 5), color.White, true, vector.FillRuleNonZero, ggfx.BlendSourceOver)
			}
			dst.At(0, 0)
		}
		if got := bytes(); got > vector.StencilCacheMaxBytes {
			t.Fatalf("got %d bytes of cache pages, want at most %d", got, vector.StencilCacheMaxBytes)
		}
	}
	for range 3 * vector.AtlasRecentWindow {
		if err := vector.NextFrame(); err != nil {
			t.Fatal(err)
		}
	}
	// A fill gives back the pages unused in the frames since.
	vector.FillRect(dst, 0, 0, 10, 10, color.White, true)
	dst.At(0, 0)
	if got := bytes(); got > 256*256*4 {
		t.Errorf("after idle frames: got %d bytes of cache pages, want at most one small page", got)
	}
}
