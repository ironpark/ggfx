// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"fmt"
	"image"
	"strings"
	"testing"
)

// rectsSVG is n 4 by 4 rects on a grid in a 100 by 100 viewBox, filled
// with a color or a gradient.
func rectsSVG(n int, gradient bool) string {
	return shapesSVG(n, gradient, "")
}

// shapesSVG is rectsSVG with each rect holding a hole, filled with rule.
func shapesSVG(n int, gradient bool, rule string) string {
	var b strings.Builder
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
		if rule == "" {
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="4" height="4" fill="%s"/>`, x, y, fill)
		} else {
			fmt.Fprintf(&b, `<path d="M%d,%d h4 v4 h-4 z m1,1 h2 v2 h-2 z" fill="%s" fill-rule="%s"/>`, x, y, fill, rule)
		}
	}
	return goldenDoc(b.String())
}

func benchmarkDraw(b *testing.B, src string, size int) {
	d := parse(b, src)
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	g := scale(float64(size) / 100)
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
	for _, rule := range []string{"nonzero", "evenodd"} {
		for _, size := range []int{64, 1024} {
			b.Run(fmt.Sprintf("%s/rects=100/%d", rule, size), func(b *testing.B) {
				benchmarkDraw(b, shapesSVG(100, false, rule), size)
			})
		}
	}
	for _, size := range []int{24, 256, 1024} {
		b.Run(fmt.Sprintf("icon/%d", size), func(b *testing.B) {
			benchmarkDraw(b, iconSVG, size)
		})
	}
}
