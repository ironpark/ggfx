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

package ggfx_test

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/ironpark/ggfx"
)

// glyphs makes n small images of different sizes and colors, as a text's glyphs are, with a
// large one among them that cannot share their atlas.
func glyphs(n int) []*ggfx.Image {
	var imgs []*ggfx.Image
	for k := range n {
		w, h := 3+k%5, 4+k%7
		if k == n/2 {
			w, h = 2100, 9 // too large for the atlas the others share
		}
		img := ggfx.NewImage(w, h)
		pix := make([]byte, 4*w*h)
		for p := 0; p < len(pix); p += 4 {
			pix[p], pix[p+1], pix[p+2], pix[p+3] = byte(40*k), byte(255-p), byte(p*7), 0xff
		}
		img.WritePixels(pix)
		imgs = append(imgs, img)
	}
	return imgs
}

func TestDrawImagesPaintsWhatDrawImageDoes(t *testing.T) {
	for _, filter := range []ggfx.Filter{ggfx.FilterNearest, ggfx.FilterLinear} {
		testDrawImagesPaintsWhatDrawImageDoes(t, filter, 1, 1)
	}
}

func TestDrawImagesLinearShrinkMatchesDrawImage(t *testing.T) {
	scales := [][2]float64{
		{0.9, 0.9},
		{0.6, 0.6},
		{0.5, 0.5},
		{0.49, 0.49},
		{0.25, 0.25},
		{0.4, 0.9},
	}
	for _, sc := range scales {
		testDrawImagesPaintsWhatDrawImageDoes(t, ggfx.FilterLinear, sc[0], sc[1])
	}
}

// TestMipmapSelectionMatchesIndependentBaseline checks DrawImage and DrawImages against an
// independent level-0 / level-1 baseline, including cases where destination-pixel snapping
// reduces edge scale below 0.5 (e.g. a 3x3 source scaled by 0.5 and translated by 0.25),
// rotation, reflection, and non-uniform scaling.
func TestMipmapSelectionMatchesIndependentBaseline(t *testing.T) {
	// 1. 3x3 patterned source: top-left 2x2 is red, right/bottom border is blue.
	// Level 1 is 1x1 = average of the top-left 2x2 = pure red (no blue).
	// Level 0 (DisableMipmaps) samples the right/bottom blue border at (1, 1).
	src3 := ggfx.NewImage(3, 3)
	defer src3.Deallocate()
	pix3 := make([]byte, 4*3*3)
	for y := range 3 {
		for x := range 3 {
			off := 4 * (y*3 + x)
			if x < 2 && y < 2 {
				pix3[off], pix3[off+1], pix3[off+2], pix3[off+3] = 0xff, 0, 0, 0xff
			} else {
				pix3[off], pix3[off+1], pix3[off+2], pix3[off+3] = 0, 0, 0xff, 0xff
			}
		}
	}
	src3.WritePixels(pix3)

	var geo3 ggfx.GeoM
	geo3.Scale(0.5, 0.5)
	geo3.Translate(0.25, 0.25)

	draw3 := func(mode string) color.RGBA {
		dst := ggfx.NewImage(4, 4)
		defer dst.Deallocate()
		op := &ggfx.DrawImageOptions{GeoM: geo3, Filter: ggfx.FilterLinear}
		switch mode {
		case "level0":
			op.DisableMipmaps = true
			dst.DrawImage(src3, op)
		case "drawImage":
			dst.DrawImage(src3, op)
		case "drawImages":
			dst.DrawImages([]ggfx.ImageDraw{{Image: src3}}, op)
		}
		return dst.At(1, 1).(color.RGBA)
	}
	cLevel0 := draw3("level0")
	cDrawImage := draw3("drawImage")
	cDrawImages := draw3("drawImages")
	if cLevel0.B < 0x80 {
		t.Fatalf("level-0 baseline at (1,1) expected blue from 3x3 border, got %v", cLevel0)
	}
	// Level 1 of a 3x3 image is 1x1 pure red (no blue channel at all).
	if cDrawImage.B != 0 || cDrawImage.R == 0 || cDrawImage.R != cDrawImage.A {
		t.Errorf("DrawImage 3x3 scale(0.5)+translate(0.25) at (1,1) = %v, want pure red from mip level 1 (level 0 was %v)", cDrawImage, cLevel0)
	}
	if cDrawImages != cDrawImage {
		t.Errorf("DrawImages 3x3 scale(0.5)+translate(0.25) at (1,1) = %v, want %v", cDrawImages, cDrawImage)
	}

	// 2. 64x64 alternating red/blue checkerboard: level 1 is uniform purple ~(128,0,128,255),
	// whereas level 0 preserves strong red/blue contrast across samples.
	const sz = 64
	checker := ggfx.NewImage(sz, sz)
	defer checker.Deallocate()
	pixC := make([]byte, 4*sz*sz)
	for y := range sz {
		for x := range sz {
			off := 4 * (y*sz + x)
			if (x+y)%2 == 0 {
				pixC[off], pixC[off+1], pixC[off+2], pixC[off+3] = 0xff, 0, 0, 0xff
			} else {
				pixC[off], pixC[off+1], pixC[off+2], pixC[off+3] = 0, 0, 0xff, 0xff
			}
		}
	}
	checker.WritePixels(pixC)

	// Independent level-1 reference image made by explicit 0.5x linear downsample.
	level1Ref := ggfx.NewImage(sz/2, sz/2)
	defer level1Ref.Deallocate()
	{
		var op ggfx.DrawImageOptions
		op.GeoM.Scale(0.5, 0.5)
		op.Filter = ggfx.FilterLinear
		op.DisableMipmaps = true
		level1Ref.DrawImage(checker, &op)
	}

	makeGeo := func(sx, sy, rot, tx, ty float64) ggfx.GeoM {
		var g ggfx.GeoM
		g.Scale(sx, sy)
		if rot != 0 {
			g.Rotate(rot)
		}
		g.Translate(tx, ty)
		return g
	}
	cases := []struct {
		name       string
		geo        ggfx.GeoM
		wantMipmap bool
	}{
		{"scale 0.9", makeGeo(0.9, 0.9, 0, 4, 4), false},
		{"scale 0.6", makeGeo(0.6, 0.6, 0, 4, 4), false},
		{"scale 0.49", makeGeo(0.49, 0.49, 0, 4, 4), true},
		{"rotation 45 at 0.6", makeGeo(0.6, 0.6, math.Pi/4, 48, 4), false},
		{"rotation 45 at 0.4", makeGeo(0.4, 0.4, math.Pi/4, 48, 4), true},
		{"reflection at 0.6", makeGeo(-0.6, 0.6, 0, 60, 4), false},
		{"reflection at 0.4", makeGeo(-0.4, 0.4, 0, 60, 4), true},
		{"non-uniform 0.4x0.9", makeGeo(0.4, 0.9, 0, 4, 4), false},
		{"non-uniform 0.4x0.45", makeGeo(0.4, 0.45, 0, 4, 4), true},
	}

	const dstW, dstH = 96, 96
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			render := func(src *ggfx.Image, geo ggfx.GeoM, disableMip, batched bool) []byte {
				dst := ggfx.NewImage(dstW, dstH)
				defer dst.Deallocate()
				op := &ggfx.DrawImageOptions{GeoM: geo, Filter: ggfx.FilterLinear, DisableMipmaps: disableMip}
				if batched {
					dst.DrawImages([]ggfx.ImageDraw{{Image: src}}, op)
				} else {
					dst.DrawImage(src, op)
				}
				out := make([]byte, 4*dstW*dstH)
				dst.ReadPixels(out)
				return out
			}

			var baseline []byte
			if tc.wantMipmap {
				// Independent mip-level-1 baseline: draw the explicit half-res image scaled by 2x
				// before tc.geo, with mipmaps disabled.
				var g ggfx.GeoM
				g.Scale(2, 2)
				g.Concat(tc.geo)
				baseline = render(level1Ref, g, true, false)
			} else {
				baseline = render(checker, tc.geo, true, false)
			}

			gotSingle := render(checker, tc.geo, false, false)
			gotBatch := render(checker, tc.geo, false, true)
			for p := range baseline {
				if abs(int(gotSingle[p])-int(baseline[p])) > 2 {
					t.Fatalf("DrawImage differs from independent baseline at (%d,%d): got %v, want %v",
						p/4%dstW, p/4/dstW, gotSingle[p&^3:p&^3+4], baseline[p&^3:p&^3+4])
				}
				if abs(int(gotBatch[p])-int(baseline[p])) > 2 {
					t.Fatalf("DrawImages differs from independent baseline at (%d,%d): got %v, want %v",
						p/4%dstW, p/4/dstW, gotBatch[p&^3:p&^3+4], baseline[p&^3:p&^3+4])
				}
			}
		})
	}
}

func testDrawImagesPaintsWhatDrawImageDoes(t *testing.T, filter ggfx.Filter, sx, sy float64) {
	imgs := glyphs(24)
	var tint ggfx.ColorScale
	tint.Scale(0.5, 0.8, 1, 0.75)
	var geoM ggfx.GeoM
	geoM.Scale(sx, sy)
	geoM.Translate(3.25, 5.5)

	paint := func(batched bool) []byte {
		dst := ggfx.NewImage(160, 120)
		dst.Fill(color.RGBA{0x10, 0x20, 0x30, 0xff})
		// A sub-image, as a clipped canvas draws into.
		sub := dst.SubImage(image.Rect(4, 6, 150, 110)).(*ggfx.Image)
		var draws []ggfx.ImageDraw
		for k, img := range imgs {
			d := ggfx.ImageDraw{Image: img, X: float64(k%8) * 17.5, Y: float64(k/8) * 31}
			if k%3 == 0 {
				d.ColorScale = tint
			}
			draws = append(draws, d)
		}
		op := &ggfx.DrawImageOptions{GeoM: geoM, Filter: filter}
		if batched {
			sub.DrawImages(draws, op)
		} else {
			for _, d := range draws {
				o := *op
				o.GeoM.Reset()
				o.GeoM.Translate(d.X, d.Y)
				o.GeoM.Concat(geoM)
				o.ColorScale = d.ColorScale
				sub.DrawImage(d.Image, &o)
			}
		}
		pix := make([]byte, 4*160*120)
		dst.ReadPixels(pix)
		return pix
	}
	// Linear filtering may differ by one between two paints of DrawImage itself, as GPUs round
	// (see sameColors).
	want := paint(false)
	got := paint(true)
	if !bytes.Equal(got, want) {
		for p := range want {
			if abs(int(got[p])-int(want[p])) > 1 {
				t.Fatalf("filter %d: pixel %d,%d differs: got %v, want %v", filter, p/4%160, p/4/160, got[p&^3:p&^3+4], want[p&^3:p&^3+4])
			}
		}
	}
}

// A later WritePixels to the second quad's image must not be hoisted ahead of a merged DrawImages.
func TestDrawImagesWritePixelsOrdering(t *testing.T) {
	red := color.RGBA{0xff, 0, 0, 0xff}
	green := color.RGBA{0, 0xff, 0, 0xff}
	blue := color.RGBA{0, 0, 0xff, 0xff}
	solid := func(c color.RGBA) []byte {
		pix := make([]byte, 4*8*8)
		for i := 0; i < len(pix); i += 4 {
			pix[i], pix[i+1], pix[i+2], pix[i+3] = c.R, c.G, c.B, c.A
		}
		return pix
	}
	img0 := ggfx.NewImage(8, 8)
	img1 := ggfx.NewImage(8, 8)
	img0.WritePixels(solid(red))
	img1.WritePixels(solid(green))

	dst := ggfx.NewImage(32, 16)
	dst.DrawImages([]ggfx.ImageDraw{
		{Image: img0, X: 0, Y: 0},
		{Image: img1, X: 16, Y: 0},
	}, nil)
	// Overwrite img1 after DrawImages in the same frame; dst must still keep green.
	img1.WritePixels(solid(blue))

	if got := dst.At(4, 4).(color.RGBA); got != red {
		t.Errorf("quad 0: got %v, want %v", got, red)
	}
	if got := dst.At(20, 4).(color.RGBA); got != green {
		t.Errorf("quad 1: got %v, want %v (later WritePixels was hoisted ahead of DrawImages)", got, green)
	}
}

// Drawing a shrunk sub-image with FilterLinear + AddressClampToZero scales srcRegions with mipmaps.
func TestDrawTrianglesMipmapClampToZeroSubImage(t *testing.T) {
	sheet := ggfx.NewImage(64, 64)
	red := color.RGBA{0xff, 0, 0, 0xff}
	sub := sheet.SubImage(image.Rect(32, 32, 64, 64)).(*ggfx.Image)
	sub.Fill(red)

	dst := ggfx.NewImage(16, 16)
	vs := []ggfx.Vertex{
		{DstX: 0, DstY: 0, SrcX: 32, SrcY: 32, ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1},
		{DstX: 8, DstY: 0, SrcX: 64, SrcY: 32, ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1},
		{DstX: 0, DstY: 8, SrcX: 32, SrcY: 64, ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1},
		{DstX: 8, DstY: 8, SrcX: 64, SrcY: 64, ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1},
	}
	dst.DrawTriangles(vs, []uint16{0, 1, 2, 1, 2, 3}, sub, &ggfx.DrawTrianglesOptions{
		Filter:  ggfx.FilterLinear,
		Address: ggfx.AddressClampToZero,
	})
	if got := dst.At(4, 4).(color.RGBA); got != red {
		t.Errorf("dst.At(4, 4) = %v, want %v", got, red)
	}
}

func BenchmarkDrawImagesGlyphs(b *testing.B) {
	imgs := glyphs(80)
	dst := ggfx.NewImage(800, 200)
	draws := make([]ggfx.ImageDraw, len(imgs))
	for k, img := range imgs {
		draws[k] = ggfx.ImageDraw{Image: img, X: float64(k * 9)}
	}
	op := &ggfx.DrawImageOptions{}
	b.Run("DrawImage", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			for _, d := range draws {
				o := *op
				o.GeoM.Translate(d.X, d.Y)
				dst.DrawImage(d.Image, &o)
			}
			if i%16 == 15 {
				flushDraws(b, dst)
			}
		}
	})
	b.Run("DrawImages", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			dst.DrawImages(draws, op)
			if i%16 == 15 {
				flushDraws(b, dst)
			}
		}
	})
}
