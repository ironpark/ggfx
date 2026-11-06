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

package rendertest_test

import (
	"image"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/host"
)

// BenchmarkGlyphChurn makes 200 new alpha images a frame, as a text renderer makes glyphs, and
// deallocates those of 30 frames before. AsCreated draws each image as soon as it is made, as a
// text renderer does; AfterAll makes them all first. The two should cost about the same: the
// uploads of the images join one command either way.
func BenchmarkGlyphChurnAsCreated(b *testing.B) { benchmarkGlyphChurn(b, false) }
func BenchmarkGlyphChurnAfterAll(b *testing.B)  { benchmarkGlyphChurn(b, true) }

func benchmarkGlyphChurn(b *testing.B, afterAll bool) {
	const perFrame, liveFrames = 200, 30

	dst := ggfx.NewImage(1440, 900)
	defer dst.Deallocate()
	srcs := make([]*image.Alpha, perFrame)
	for k := range srcs {
		a := image.NewAlpha(image.Rect(0, 0, 10+k%6, 14))
		for i := range a.Pix {
			a.Pix[i] = byte(i + k)
		}
		srcs[k] = a
	}
	opts := &ggfx.NewImageFromImageOptions{Format: ggfx.PixelFormatAlpha}

	var live [][]*ggfx.Image
	defer func() {
		for _, gs := range live {
			for _, g := range gs {
				g.Deallocate()
			}
		}
	}()
	op := &ggfx.DrawImageOptions{}
	draw := func(k int, g *ggfx.Image) {
		op.GeoM.Reset()
		op.GeoM.Translate(float64(k%100)*12, float64(k/100)*16)
		dst.DrawImage(g, op)
	}
	frame := func() {
		fr, err := host.BeginFrame()
		if err != nil {
			b.Fatal(err)
		}
		gs := make([]*ggfx.Image, 0, perFrame)
		for k, a := range srcs {
			g := ggfx.NewImageFromImageWithOptions(a, opts)
			gs = append(gs, g)
			if !afterAll {
				draw(k, g)
			}
		}
		if afterAll {
			for k, g := range gs {
				draw(k, g)
			}
		}
		live = append(live, gs)
		if len(live) > liveFrames {
			for _, g := range live[0] {
				g.Deallocate()
			}
			live = live[1:]
		}
		if err := fr.End(); err != nil {
			b.Fatal(err)
		}
		if _, err := host.Flush(false); err != nil {
			b.Fatal(err)
		}
	}

	// Fill the atlas so that the frames measured reuse freed slots.
	for range 2 * liveFrames {
		frame()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		frame()
	}
}
