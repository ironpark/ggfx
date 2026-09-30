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

// The tests in this file check that rendering the paths contained in their bounds together, to the
// whole stencil atlas and to the destination in one call each, renders the same pixels as rendering
// every path one by one to its own stencil buffer and destination, as a clipped path is rendered.

// snap rounds v to a multiple of 1/16. A vertex on this grid, moved by the antialiasing sample
// offsets, which are multiples of 1/16 too, falls on the rasterizer's sub-pixel grid exactly. Then
// coverage cannot depend on how the vertex is rounded on its way to the GPU, and the two ways of
// rendering must agree on every pixel.
func snap(v float32) float32 {
	return float32(math.Round(float64(v)*16) / 16)
}

func randomColor(r *rand.Rand) color.Color {
	return color.NRGBA{R: uint8(r.IntN(256)), G: uint8(r.IntN(256)), B: uint8(r.IntN(256)), A: uint8(64 + r.IntN(192))}
}

func gridPolygon(r *rand.Rand, cx, cy, radius float32, n int) *vector.Path {
	var p vector.Path
	for i := range n {
		a := float64(i) / float64(n) * 2 * math.Pi
		rr := radius * (0.3 + 0.7*r.Float32())
		x, y := snap(cx+rr*float32(math.Cos(a))), snap(cy+rr*float32(math.Sin(a)))
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	p.Close()
	return &p
}

// gridCurves returns a path of lines and quadratic curves whose points are on the grid of snap.
func gridCurves(r *rand.Rand, cx, cy, radius float32, closed bool) *vector.Path {
	pt := func() (float32, float32) {
		return snap(cx + (r.Float32()*2-1)*radius), snap(cy + (r.Float32()*2-1)*radius)
	}
	var p vector.Path
	x, y := pt()
	p.MoveTo(x, y)
	for range 2 + r.IntN(6) {
		if r.IntN(3) == 0 {
			x, y := pt()
			p.LineTo(x, y)
			continue
		}
		x1, y1 := pt()
		x2, y2 := pt()
		p.QuadTo(x1, y1, x2, y2)
	}
	if closed {
		p.Close()
	}
	return &p
}

func fillPath(dst *ggfx.Image, p *vector.Path, clr color.Color, aa bool, rule vector.FillRule, blend ggfx.Blend) {
	op := &vector.DrawPathOptions{AntiAlias: aa, Blend: blend}
	op.ColorScale.ScaleWithColor(clr)
	vector.FillPath(dst, p, &vector.FillOptions{FillRule: rule}, op)
}

type batchScene struct {
	name string
	w, h int
	draw func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule)
}

var gridScenes = []batchScene{
	{"polygons", 257, 199, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		for range 60 {
			fillPath(dst, gridPolygon(r, r.Float32()*257, r.Float32()*199, 5+r.Float32()*50, 3+r.IntN(14)), randomColor(r), aa, rule, ggfx.BlendSourceOver)
		}
	}},
	{"curves", 240, 240, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		for i := range 60 {
			fillPath(dst, gridCurves(r, r.Float32()*240, r.Float32()*240, 10+r.Float32()*60, i%3 != 0), randomColor(r), aa, rule, ggfx.BlendSourceOver)
		}
	}},
	// Paths across the destination's edges, which are rendered one by one, between paths contained
	// in it, which are rendered together. Overlapping opaque paths show their order.
	{"clipped", 160, 120, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		for i := range 80 {
			cx, cy := 40+r.Float32()*80, 30+r.Float32()*60
			if i%3 == 0 {
				cx, cy = -40+r.Float32()*240, -40+r.Float32()*200
			}
			clr := randomColor(r)
			if i%4 == 0 {
				clr = color.RGBA{R: uint8(r.IntN(256)), G: uint8(r.IntN(256)), B: 0x80, A: 0xff}
			}
			fillPath(dst, gridCurves(r, cx, cy, 10+r.Float32()*60, true), clr, aa, rule, ggfx.BlendSourceOver)
		}
	}},
	// Paths drawn to sub-images of the destination between paths drawn to the destination itself.
	{"subimages", 300, 260, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		dst.Fill(color.RGBA{0x10, 0x20, 0x30, 0xff})
		subs := []*ggfx.Image{
			dst,
			dst.SubImage(image.Rect(37, 53, 241, 219)).(*ggfx.Image),
			dst.SubImage(image.Rect(150, 10, 290, 150)).(*ggfx.Image),
		}
		for i := range 90 {
			sub := subs[i%len(subs)]
			if i%7 == 0 {
				sub = subs[0]
			}
			b := sub.Bounds()
			cx := float32(b.Min.X) + r.Float32()*float32(b.Dx()+40) - 20
			cy := float32(b.Min.Y) + r.Float32()*float32(b.Dy()+40) - 20
			fillPath(sub, gridPolygon(r, cx, cy, 5+r.Float32()*45, 3+r.IntN(8)), randomColor(r), aa, rule, ggfx.BlendSourceOver)
		}
	}},
	// Changing the blend flushes the paths so far, so every flush renders a few paths.
	{"blends", 180, 180, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		dst.Fill(color.RGBA{0x40, 0x40, 0x40, 0xff})
		blends := []ggfx.Blend{ggfx.BlendLighter, ggfx.BlendCopy, ggfx.BlendSourceOver, ggfx.BlendXor}
		for i := range 40 {
			fillPath(dst, gridPolygon(r, r.Float32()*180, r.Float32()*180, 10+r.Float32()*40, 6), randomColor(r), aa, rule, blends[(i/3)%len(blends)])
		}
	}},
	// Self-intersecting stars with holes, where the fill rules differ.
	{"stars", 200, 200, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		for k := range 9 {
			var p vector.Path
			cx, cy := float32(35+(k%3)*65), float32(35+(k/3)*65)
			n := 5 + 2*k
			for i := range n {
				a := float64(i*(n/2)) / float64(n) * 2 * math.Pi
				x, y := snap(cx+30*float32(math.Cos(a))), snap(cy+30*float32(math.Sin(a)))
				if i == 0 {
					p.MoveTo(x, y)
				} else {
					p.LineTo(x, y)
				}
			}
			p.Close()
			p.MoveTo(cx-8.25, cy-8.25)
			p.LineTo(cx-8.25, cy+8.25)
			p.LineTo(cx+8.25, cy+8.25)
			p.LineTo(cx+8.25, cy-8.25)
			p.Close()
			fillPath(dst, &p, color.RGBA{uint8(25 * k), 0x80, 0xff - uint8(20*k), 0xff}, aa, rule, ggfx.BlendSourceOver)
		}
	}},
	// Paths smaller than a pixel, and degenerate paths.
	{"tiny", 64, 64, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		for range 120 {
			var p vector.Path
			x, y := snap(r.Float32()*64), snap(r.Float32()*64)
			p.MoveTo(x, y)
			p.LineTo(x+snap(r.Float32()*1.5), y+snap(r.Float32()*0.4))
			p.LineTo(x+snap(r.Float32()*0.4), y+snap(r.Float32()*1.7))
			p.Close()
			fillPath(dst, &p, randomColor(r), aa, rule, ggfx.BlendSourceOver)
		}
		var p vector.Path
		p.MoveTo(10, 10)
		fillPath(dst, &p, color.White, aa, rule, ggfx.BlendSourceOver)
		var q vector.Path
		q.MoveTo(5, 5)
		q.LineTo(30, 30)
		q.Close()
		fillPath(dst, &q, color.White, aa, rule, ggfx.BlendSourceOver)
		fillPath(dst, &vector.Path{}, color.White, aa, rule, ggfx.BlendSourceOver)
	}},
	// Enough large paths to need several atlas images; see TestFillPathBatchingSceneUsesAtlasImages.
	{"many", 300, 300, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		for range manyPathCount {
			fillPath(dst, manyPath(r), randomColor(r), aa, rule, ggfx.BlendSourceOver)
		}
	}},
}

const manyPathCount = 500

func manyPath(r *rand.Rand) *vector.Path {
	return gridPolygon(r, 145+r.Float32()*10, 145+r.Float32()*10, 60+r.Float32()*80, 3+r.IntN(20))
}

// renderBoth renders a scene with and without batching and returns the pixels of each.
func renderBoth(s batchScene, seed uint64, aa bool, rule vector.FillRule) (batched, unbatched []byte) {
	render := func(disabled bool) []byte {
		vector.SetBatchingDisabled(disabled)
		defer vector.SetBatchingDisabled(false)
		dst := ggfx.NewImage(s.w, s.h)
		defer dst.Deallocate()
		s.draw(dst, rand.New(rand.NewPCG(seed, 1)), aa, rule)
		pix := make([]byte, 4*s.w*s.h)
		dst.ReadPixels(pix)
		return pix
	}
	return render(false), render(true)
}

// diffPixels returns the positions of the pixels that differ, up to limit.
func diffPixels(a, b []byte, w, limit int) []string {
	var diffs []string
	for i := 0; i < len(a); i += 4 {
		if [4]byte(a[i:i+4]) == [4]byte(b[i:i+4]) {
			continue
		}
		if len(diffs) < limit {
			diffs = append(diffs, fmt.Sprintf("(%d, %d): %v != %v", (i/4)%w, (i/4)/w, a[i:i+4], b[i:i+4]))
		} else {
			diffs = append(diffs, "...")
			break
		}
	}
	return diffs
}

func TestFillPathBatchingIsExact(t *testing.T) {
	for _, s := range gridScenes {
		for _, aa := range []bool{false, true} {
			for _, rule := range []vector.FillRule{vector.FillRuleNonZero, vector.FillRuleEvenOdd} {
				for seed := range uint64(2) {
					t.Run(fmt.Sprintf("%s/aa=%t/rule=%d/seed=%d", s.name, aa, rule, seed), func(t *testing.T) {
						batched, unbatched := renderBoth(s, seed, aa, rule)
						if diffs := diffPixels(batched, unbatched, s.w, 5); len(diffs) > 0 {
							t.Errorf("batched and unbatched pixels differ: %v", diffs)
						}
					})
				}
			}
		}
	}
}

// TestFillPathBatchingSceneUsesAtlasImages checks that the scene "many" covers what it is for: its
// paths are contained, and they are on more than one atlas image.
func TestFillPathBatchingSceneUsesAtlasImages(t *testing.T) {
	r := rand.New(rand.NewPCG(0, 1))
	var paths []*vector.Path
	var bounds []image.Rectangle
	dstBounds := image.Rect(0, 0, 300, 300)
	for range manyPathCount {
		paths = append(paths, manyPath(r))
		bounds = append(bounds, dstBounds)
		randomColor(r)
	}
	for _, aa := range []bool{false, true} {
		images := map[int]bool{}
		var contained int
		for _, region := range vector.AtlasRegions(dstBounds, paths, bounds, aa) {
			images[region.ImageIndex] = true
			if region.Contained {
				contained++
			}
		}
		if len(images) < 2 {
			t.Errorf("aa=%t: the paths are on %d atlas image(s), want 2 or more", aa, len(images))
		}
		if contained < manyPathCount*9/10 {
			t.Errorf("aa=%t: %d paths are contained, want most of %d", aa, contained, manyPathCount)
		}
	}
}

// TestFillPathBatchingWithGeneratedPaths compares the two ways of rendering for strokes, circles,
// arcs and cubic curves, whose points the package computes. An edge passing within a rounding
// error of a pixel center can change that pixel's coverage, so a few isolated pixels may differ.
func TestFillPathBatchingWithGeneratedPaths(t *testing.T) {
	const w, h = 220, 200
	s := batchScene{"generated", w, h, func(dst *ggfx.Image, r *rand.Rand, aa bool, rule vector.FillRule) {
		for i := range 40 {
			x, y := r.Float32()*w, r.Float32()*h
			switch i % 4 {
			case 0:
				var p, s vector.Path
				p.MoveTo(x, y)
				p.CubicTo(r.Float32()*w, r.Float32()*h, r.Float32()*w, r.Float32()*h, r.Float32()*w, r.Float32()*h)
				p.ArcTo(r.Float32()*w, r.Float32()*h, r.Float32()*w, r.Float32()*h, 5+r.Float32()*20)
				s.AddStroke(&p, &vector.AddStrokeOptions{StrokeOptions: vector.StrokeOptions{
					Width: 0.5 + r.Float32()*8, LineJoin: vector.LineJoin(i % 3), LineCap: vector.LineCap(i % 3), MiterLimit: 4,
				}})
				fillPath(dst, &s, randomColor(r), aa, vector.FillRuleNonZero, ggfx.BlendSourceOver)
			case 1:
				var p vector.Path
				p.Arc(x, y, 3+r.Float32()*30, 0, 2*math.Pi, vector.Clockwise)
				fillPath(dst, &p, randomColor(r), aa, rule, ggfx.BlendSourceOver)
			case 2:
				var p vector.Path
				p.MoveTo(x, y)
				p.CubicTo(r.Float32()*w, r.Float32()*h, r.Float32()*w, r.Float32()*h, x+10, y+10)
				p.Close()
				fillPath(dst, &p, randomColor(r), aa, rule, ggfx.BlendSourceOver)
			case 3:
				var p, s vector.Path
				p.MoveTo(x, y)
				p.LineTo(r.Float32()*w, r.Float32()*h)
				p.LineTo(r.Float32()*w, r.Float32()*h)
				s.AddStroke(&p, &vector.AddStrokeOptions{StrokeOptions: vector.StrokeOptions{Width: 0.3 + r.Float32()*3}})
				fillPath(dst, &s, randomColor(r), aa, vector.FillRuleNonZero, ggfx.BlendSourceOver)
			}
		}
	}}
	for _, aa := range []bool{false, true} {
		for _, rule := range []vector.FillRule{vector.FillRuleNonZero, vector.FillRuleEvenOdd} {
			t.Run(fmt.Sprintf("aa=%t/rule=%d", aa, rule), func(t *testing.T) {
				batched, unbatched := renderBoth(s, 7, aa, rule)
				if diffs := diffPixels(batched, unbatched, w, 3); len(diffs) > 3 {
					t.Errorf("more than a few pixels differ: %v", diffs)
				}
			})
		}
	}
}

// TestFillPathBatchingDrawCalls checks how many draw calls rendering paths takes.
func TestFillPathBatchingDrawCalls(t *testing.T) {
	polygon := func(x, y float32) *vector.Path {
		var p vector.Path
		p.MoveTo(x, y)
		p.LineTo(x+10, y)
		p.LineTo(x+5, y+10)
		p.Close()
		return &p
	}
	curve := func(x, y float32) *vector.Path {
		var p vector.Path
		p.MoveTo(x, y)
		p.QuadTo(x+10, y, x+10, y+10)
		p.Close()
		return &p
	}
	const n = 50
	// With antialiasing, a path takes 8 samples, and a stencil pass draws each sample.
	const samples = 8

	tests := []struct {
		name  string
		draw  func(dst *ggfx.Image)
		want  int
		wantU int
	}{
		{
			name: "contained polygons",
			draw: func(dst *ggfx.Image) {
				for i := range n {
					fillPath(dst, polygon(float32(i*3%180), float32(i*7%180)), color.White, true, vector.FillRuleNonZero, ggfx.BlendSourceOver)
				}
			},
			// The polygons, then the colors. No path has a curve.
			want:  2,
			wantU: n*samples + n,
		},
		{
			name: "contained curves",
			draw: func(dst *ggfx.Image) {
				for i := range n {
					fillPath(dst, curve(float32(i*3%180), float32(i*7%180)), color.White, true, vector.FillRuleNonZero, ggfx.BlendSourceOver)
				}
			},
			want:  3,
			wantU: n*samples*2 + n,
		},
		{
			name: "a clipped polygon",
			draw: func(dst *ggfx.Image) {
				for i := range n {
					x, y := float32(i*3%180), float32(i*7%180)
					if i == n/2 {
						x = -5
					}
					fillPath(dst, polygon(x, y), color.White, true, vector.FillRuleNonZero, ggfx.BlendSourceOver)
				}
			},
			// The clipped path's samples are drawn one by one. Its bounds are the destination's,
			// so its color is drawn with the others.
			want:  1 + samples + 1,
			wantU: n*samples + n,
		},
		{
			name: "a polygon on a sub-image",
			draw: func(dst *ggfx.Image) {
				sub := dst.SubImage(image.Rect(20, 20, 120, 120)).(*ggfx.Image)
				for i := range n {
					d := dst
					if i == n/2 {
						d = sub
					}
					fillPath(d, polygon(float32(30+i*3%80), float32(30+i*7%80)), color.White, true, vector.FillRuleNonZero, ggfx.BlendSourceOver)
				}
			},
			// The path on the sub-image is contained in it, so its stencil is drawn with the
			// others. Its color is drawn to the sub-image alone, between the colors of the
			// paths before and after it.
			want:  1 + 3,
			wantU: n*samples + n,
		},
		{
			name: "without antialiasing",
			draw: func(dst *ggfx.Image) {
				for i := range n {
					fillPath(dst, curve(float32(i*3%180), float32(i*7%180)), color.White, false, vector.FillRuleNonZero, ggfx.BlendSourceOver)
				}
			},
			want:  3,
			wantU: n*2 + n,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, disabled := range []bool{false, true} {
				vector.SetBatchingDisabled(disabled)
				dst := ggfx.NewImage(200, 200)
				tc.draw(dst)
				before := vector.DrawCallCount()
				// Using the destination renders the paths.
				dst.At(0, 0)
				got := vector.DrawCallCount() - before
				dst.Deallocate()
				vector.SetBatchingDisabled(false)

				want := tc.want
				if disabled {
					want = tc.wantU
				}
				if got != want {
					t.Errorf("batching disabled=%t: got %d draw calls, want %d", disabled, got, want)
				}
			}
		})
	}
}

// TestFillPathAtlasRegions checks the layout that batching relies on: regions do not overlap, the
// stencil buffers are the region or its halves, and a contained path lies in its rendering bounds.
// A contained path's triangles then cover no pixel center outside its stencil buffer, as its
// samples are offset by less than half a pixel.
func TestFillPathAtlasRegions(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	dstBounds := image.Rect(10, 20, 410, 320)
	var paths []*vector.Path
	var bounds []image.Rectangle
	for i := range 400 {
		cx, cy := 10+r.Float32()*400, 20+r.Float32()*300
		var p *vector.Path
		switch i % 5 {
		case 0:
			p = &vector.Path{}
		case 1:
			// Outside the destination.
			p = gridPolygon(r, -100, -100, 20, 5)
		default:
			p = gridCurves(r, cx, cy, 1+r.Float32()*120, true)
		}
		paths = append(paths, p)
		b := dstBounds
		if i%3 == 0 {
			b = image.Rect(50, 60, 250, 200)
		}
		bounds = append(bounds, b)
	}

	for _, aa := range []bool{false, true} {
		regions := vector.AtlasRegions(dstBounds, paths, bounds, aa)
		for i, ri := range regions {
			if ri.Region.Empty() {
				continue
			}
			rb := vector.PathRenderingBounds(dstBounds, paths[i], bounds[i])
			want := []image.Rectangle{ri.Region}
			if aa {
				left, right := ri.Region, ri.Region
				left.Max.X = left.Min.X + left.Dx()/2
				right.Min.X = left.Max.X
				want = []image.Rectangle{left, right}
			}
			if len(ri.StencilBuffers) != len(want) {
				t.Fatalf("aa=%t: path %d: got %d stencil buffers, want %d", aa, i, len(ri.StencilBuffers), len(want))
			}
			for k, sb := range ri.StencilBuffers {
				if sb != want[k] {
					t.Errorf("aa=%t: path %d: stencil buffer %d is %v, want %v", aa, i, k, sb, want[k])
				}
				if sb.Size() != rb.Size() {
					t.Errorf("aa=%t: path %d: stencil buffer %d has size %v, want the rendering bounds' %v", aa, i, k, sb.Size(), rb.Size())
				}
			}
			pb := paths[i].Bounds()
			if want := pb.In(bounds[i].Intersect(dstBounds)); ri.Contained != want {
				t.Errorf("aa=%t: path %d: contained is %t, want %t", aa, i, ri.Contained, want)
			}
			if ri.Contained && !pb.In(rb) {
				t.Errorf("aa=%t: path %d is contained, but its bounds %v are not in its rendering bounds %v", aa, i, pb, rb)
			}
			for j := i + 1; j < len(regions); j++ {
				rj := regions[j]
				if rj.ImageIndex == ri.ImageIndex && rj.Region.Overlaps(ri.Region) {
					t.Errorf("aa=%t: the regions of paths %d %v and %d %v overlap", aa, i, ri.Region, j, rj.Region)
				}
			}
		}
	}
}
