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
		testDrawImagesPaintsWhatDrawImageDoes(t, filter)
	}
}

func testDrawImagesPaintsWhatDrawImageDoes(t *testing.T, filter ggfx.Filter) {
	imgs := glyphs(24)
	var tint ggfx.ColorScale
	tint.Scale(0.5, 0.8, 1, 0.75)
	var geoM ggfx.GeoM
	geoM.Scale(1, 1)
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
