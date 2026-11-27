// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg_test

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/ironpark/ggfx/internal/oksvg"
	"github.com/ironpark/ggfx/internal/rasterx"
)

// D-P1: Clipped solid rect at 1024x1024 (axis-aligned rect clip and circular clip).
func BenchmarkDP1Clip(b *testing.B) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "solid-rect-1024",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="#c33"/></svg>`,
		},
		{
			name: "rect-clip-int-1024",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><clipPath id="c"><rect x="0" y="0" width="50" height="100"/></clipPath></defs><rect width="100" height="100" fill="#c33" clip-path="url(#c)"/></svg>`,
		},
		{
			name: "rect-clip-1024",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><clipPath id="c"><rect x="10" y="10" width="80" height="80"/></clipPath></defs><rect width="100" height="100" fill="#c33" clip-path="url(#c)"/></svg>`,
		},
		{
			name: "circle-clip-1024",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><clipPath id="c"><circle cx="50" cy="50" r="45"/></clipPath></defs><rect width="100" height="100" fill="#c33" clip-path="url(#c)"/></svg>`,
		},
		{
			name: "shared-circle-clip-50rects-256",
			src: func() string {
				var sb strings.Builder
				sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><clipPath id="c"><circle cx="50" cy="50" r="45"/></clipPath></defs>`)
				for i := range 50 {
					fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="10" height="10" fill="#c33" clip-path="url(#c)"/>`, (i%10)*10, (i/10)*20)
				}
				sb.WriteString(`</svg>`)
				return sb.String()
			}(),
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			icon, err := oksvg.ReadIconStream(strings.NewReader(tc.src))
			if err != nil {
				b.Fatal(err)
			}
			size := 1024
			if strings.HasSuffix(tc.name, "256") {
				size = 256
			}
			dst := image.NewRGBA(image.Rect(0, 0, size, size))
			p := oksvg.NewPainter(size, size, dst, dst.Bounds())
			m := rasterx.Identity.Scale(float64(size)/100, float64(size)/100)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				icon.DrawTransformed(p, 1, m)
			}
		})
	}
}

// D-P2: Full-canvas 1024x1024 solid vs linear gradient vs radial gradient fill.
func BenchmarkDP2Gradient(b *testing.B) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "solid-1024",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="#c33"/></svg>`,
		},
		{
			name: "linear-1024",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="0.5" stop-color="#0f0"/><stop offset="1" stop-color="#00f"/></linearGradient></defs><rect width="100" height="100" fill="url(#g)"/></svg>`,
		},
		{
			name: "radial-centered-1024",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><radialGradient id="g"><stop offset="0" stop-color="#fff"/><stop offset="0.5" stop-color="#f80"/><stop offset="1" stop-color="#00f"/></radialGradient></defs><rect width="100" height="100" fill="url(#g)"/></svg>`,
		},
		{
			name: "radial-focal-1024",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><radialGradient id="g" fx="0.3" fy="0.3"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#00f"/></radialGradient></defs><rect width="100" height="100" fill="url(#g)"/></svg>`,
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			icon, err := oksvg.ReadIconStream(strings.NewReader(tc.src))
			if err != nil {
				b.Fatal(err)
			}
			const size = 1024
			dst := image.NewRGBA(image.Rect(0, 0, size, size))
			p := oksvg.NewPainter(size, size, dst, dst.Bounds())
			m := rasterx.Identity.Scale(float64(size)/100, float64(size)/100)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				icon.DrawTransformed(p, 1, m)
			}
		})
	}
}

// D-P5: 2,000-point polyline with marker-mid evaluated through scene Renderer.
func BenchmarkDP5Markers(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><marker id="m" viewBox="0 0 10 10" refX="5" refY="5" markerWidth="2" markerHeight="2" orient="auto"><circle cx="5" cy="5" r="4" fill="#f00"/></marker></defs><polyline id="pl" fill="none" stroke="#000" stroke-width="1" marker-mid="url(#m)" points="`)
	for i := range 2000 {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%.2f,%.2f", float64(i%100), float64((i/100)*5))
	}
	sb.WriteString(`"/></svg>`)
	icon, err := oksvg.ReadIconStream(strings.NewReader(sb.String()))
	if err != nil {
		b.Fatal(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
	rend := icon.NewRenderer()
	defer rend.Release()
	opts := &oksvg.EvalOptions{
		Overrides: map[string]oksvg.NodeOverride{"pl": {HasOpacity: true, Opacity: 1}},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rend.Draw(dst, rasterx.Identity, opts)
	}
}

// D-P6: 100 rects sharing a currentColor gradient.
func BenchmarkDP6CurrentColorGrad(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><linearGradient id="g"><stop offset="0" stop-color="currentColor"/><stop offset="1" stop-color="#00f"/></linearGradient></defs>`)
	for i := range 100 {
		fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="8" height="8" fill="url(#g)"/>`, (i%10)*10, (i/10)*10)
	}
	sb.WriteString(`</svg>`)
	icon, err := oksvg.ReadIconStream(strings.NewReader(sb.String()))
	if err != nil {
		b.Fatal(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
	rend := icon.NewRenderer()
	defer rend.Release()
	opts := &oksvg.EvalOptions{CurrentColor: color.NRGBA{0xff, 0, 0, 0xff}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rend.Draw(dst, rasterx.Identity, opts)
	}
}

// D-P7: Parse memory for 1,000 empty <g> elements.
func BenchmarkDP7ParseEmptyGroups(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">`)
	for range 1000 {
		sb.WriteString(`<g/>`)
	}
	sb.WriteString(`</svg>`)
	src := sb.String()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := oksvg.ReadIconStream(strings.NewReader(src)); err != nil {
			b.Fatal(err)
		}
	}
}

// D-P8: Animated `d` attribute evaluated every frame.
func BenchmarkDP8AnimatedD(b *testing.B) {
	var d1, d2 strings.Builder
	d1.WriteString("M0 0")
	d2.WriteString("M0 10")
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&d1, " L%d %d", i, i%20)
		fmt.Fprintf(&d2, " L%d %d", i, 20-(i%20))
	}
	src := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><path d="%s" fill="none" stroke="#000"><animate attributeName="d" values="%s;%s" dur="2s" repeatCount="indefinite"/></path></svg>`, d1.String(), d1.String(), d2.String())
	icon, err := oksvg.ReadIconStream(strings.NewReader(src))
	if err != nil {
		b.Fatal(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
	rend := icon.NewRenderer()
	defer rend.Release()
	opts := &oksvg.EvalOptions{Animate: true, Time: 1.5}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rend.Draw(dst, rasterx.Identity, opts)
	}
}
