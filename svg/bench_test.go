// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/svg"
)

// rectsSVG is n 4 by 4 rects on a grid in a 100 by 100 viewBox, filled
// with a color or a gradient.
func rectsSVG(n int, gradient bool) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">`)
	fill := "#c33"
	if gradient {
		b.WriteString(`<defs><linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient></defs>`)
		fill = "url(#g)"
	}
	for i := range n {
		x, y := 48, 48
		if n > 1 {
			x, y = i%10*10+3, i/10*10+3
		}
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="4" height="4" fill="%s"/>`, x, y, fill)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func benchmarkDraw(b *testing.B, src string, size int) {
	d, err := svg.Parse(strings.NewReader(src))
	if err != nil {
		b.Fatal(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	var g ggfx.GeoM
	g.Scale(float64(size)/100, float64(size)/100)
	b.ReportAllocs()
	for b.Loop() {
		d.Draw(dst, g)
	}
}

func BenchmarkDraw(b *testing.B) {
	for _, paint := range []string{"solid", "gradient"} {
		for _, n := range []int{1, 100} {
			for _, size := range []int{64, 1024} {
				b.Run(fmt.Sprintf("%s/rects=%d/%d", paint, n, size), func(b *testing.B) {
					benchmarkDraw(b, rectsSVG(n, paint == "gradient"), size)
				})
			}
		}
	}
	for _, size := range []int{24, 256, 1024} {
		b.Run(fmt.Sprintf("icon/%d", size), func(b *testing.B) {
			benchmarkDraw(b, iconSVG, size)
		})
	}
}
