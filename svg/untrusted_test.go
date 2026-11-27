// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/svg"
)

// D-Q1: tiny dasharray over a huge segment must stay bounded in time and memory,
// and normal dashed paths must render identically.
func TestUntrustedDashBounded(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		check func(t *testing.T, dst *image.RGBA)
	}{
		{
			name: "huge-line-subpixel-dash",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><path d="M0 50H1000000" stroke="#000" stroke-dasharray="0.02"/></svg>`,
			check: func(t *testing.T, dst *image.RGBA) {
				// Odd single-entry dasharray has 50% duty cycle -> alpha ~127 along stroke, 0 outside.
				c := dst.RGBAAt(50, 50)
				if c.A < 100 || c.A > 155 {
					t.Errorf("painted pixel (50,50) = %v; want ~50%% duty alpha", c)
				}
				if off := dst.RGBAAt(50, 40); off.A != 0 {
					t.Errorf("outside pixel (50,40) = %v; want transparent", off)
				}
			},
		},
		{
			name: "huge-line-halfpx-dash",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><path d="M-500000 50H1000000" stroke="#000" stroke-dasharray="4 6"/></svg>`,
			check: func(t *testing.T, dst *image.RGBA) {
				// Period is 10 px; -500000 is an exact multiple of 10, so [0,4) is painted and [4,10) is a gap.
				if c := dst.RGBAAt(12, 50); c.A != 255 {
					t.Errorf("painted dash pixel (12,50) = %v; want opaque black", c)
				}
				if gap := dst.RGBAAt(17, 50); gap.A != 0 {
					t.Errorf("gap pixel (17,50) = %v; want transparent", gap)
				}
			},
		},
		{
			name: "huge-curve-tiny-dash",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><path d="M0 50Q500000 500000 1000000 50" fill="none" stroke="#000" stroke-width="2" stroke-dasharray="6 6"/></svg>`,
			check: func(t *testing.T, dst *image.RGBA) {
				// Curve enters at (0,50) at ~45 deg down-right: near (2,52) is in first 6px dash,
				// near (6,56) (arc length ~8.5px) is in the 6px gap.
				if c := dst.RGBAAt(2, 52); c.A == 0 {
					t.Errorf("painted curve dash pixel (2,52) = %v; want non-zero alpha", c)
				}
				if gap := dst.RGBAAt(6, 56); gap.A != 0 {
					t.Errorf("curve gap pixel (6,56) = %v; want transparent", gap)
				}
			},
		},
		{
			name: "nonuniform-scale-huge-line",
			src:  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><g transform="scale(2,0.5)"><path d="M0 50H1000000" stroke="#000" stroke-width="4" stroke-dasharray="4 6"/></g></svg>`,
			check: func(t *testing.T, dst *image.RGBA) {
				// Scale X=2 makes dashes 8px on, 12px off at y=25: x=4 is painted, x=14 is a gap.
				if c := dst.RGBAAt(4, 25); c.A != 255 {
					t.Errorf("scaled painted pixel (4,25) = %v; want opaque black", c)
				}
				if gap := dst.RGBAAt(14, 25); gap.A != 0 {
					t.Errorf("scaled gap pixel (14,25) = %v; want transparent", gap)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := parse(t, tc.src)
			dst := image.NewRGBA(image.Rect(0, 0, 100, 100))

			var m0, m1 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m0)
			start := time.Now()
			d.Draw(dst, ggfx.GeoM{})
			elapsed := time.Since(start)
			runtime.ReadMemStats(&m1)

			allocBytes := m1.TotalAlloc - m0.TotalAlloc
			if elapsed > 2*time.Second {
				t.Errorf("Draw took %v; want < 2s", elapsed)
			}
			if allocBytes > 32*1024*1024 {
				t.Errorf("Draw allocated %d bytes; want < 32 MiB", allocBytes)
			}
			tc.check(t, dst)
		})
	}

	// Clipped dashed line starting far offscreen must match an unclipped dashed line
	// with the equivalent dashoffset at x=0, with verified painted and gap pixels.
	t.Run("clipped-dash-phase-exact", func(t *testing.T) {
		// Dash period is 10+6=16. Starting at x=-1600000 is an exact multiple of 16
		// and exceeds the left-offscreen budget threshold so O(1) phase skipping runs.
		far := parse(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><path d="M-1600000 50 H1600100" stroke="#000" stroke-width="4" stroke-dasharray="10 6"/></svg>`)
		near := parse(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><path d="M-16 50 H116" stroke="#000" stroke-width="4" stroke-dasharray="10 6"/></svg>`)
		imgFar := drawInto(far, image.Rect(0, 0, 100, 100), ggfx.GeoM{})
		imgNear := drawInto(near, image.Rect(0, 0, 100, 100), ggfx.GeoM{})
		if !bytes.Equal(imgFar.Pix, imgNear.Pix) {
			t.Errorf("clipped far dashed segment differs from equivalent near segment")
		}
		if c := imgFar.RGBAAt(5, 50); c.A != 255 {
			t.Errorf("painted dash pixel (5,50) = %v; want opaque black", c)
		}
		if gap := imgFar.RGBAAt(13, 50); gap.A != 0 {
			t.Errorf("gap pixel (13,50) = %v; want transparent", gap)
		}
	})
}

// D-Q2: deep XML nesting and deep <use> chains must return ErrNestingDepth instead of overflowing the stack.
func TestUntrustedDeepNesting(t *testing.T) {
	t.Run("deep-xml-groups", func(t *testing.T) {
		const depth = 2000
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">`)
		for range depth {
			b.WriteString(`<g>`)
		}
		b.WriteString(`<rect width="10" height="10"/>`)
		for range depth {
			b.WriteString(`</g>`)
		}
		b.WriteString(`</svg>`)

		_, err := svg.Parse(strings.NewReader(b.String()))
		if !errors.Is(err, svg.ErrNestingDepth) {
			t.Fatalf("Parse deep nesting: got err=%v, want %v", err, svg.ErrNestingDepth)
		}
	})

	t.Run("deep-use-chain", func(t *testing.T) {
		const chain = 300
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><defs>`)
		b.WriteString(`<rect id="u0" width="10" height="10"/>`)
		for i := 1; i <= chain; i++ {
			fmt.Fprintf(&b, `<g id="u%d"><use href="#u%d"/></g>`, i, i-1)
		}
		fmt.Fprintf(&b, `</defs><use href="#u%d"/></svg>`, chain)

		_, err := svg.Parse(strings.NewReader(b.String()))
		if !errors.Is(err, svg.ErrNestingDepth) {
			t.Fatalf("Parse deep <use> chain: got err=%v, want %v", err, svg.ErrNestingDepth)
		}
	})
}

// D-Q3 & D-Q4: quadratic <use> expansion fails fast with ErrExpansionLimit;
// 10,001 trivial <use> references and 5,001 clip-path references both succeed and render completely.
func TestUntrustedExpansionBudget(t *testing.T) {
	t.Run("quadratic-use-fails-fast", func(t *testing.T) {
		const n = 1000
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><g id="a">`)
		for range n {
			b.WriteString(`<path d="M0 0L1 1"/>`)
		}
		b.WriteString(`</g></defs>`)
		for range n {
			b.WriteString(`<use href="#a"/>`)
		}
		b.WriteString(`</svg>`)

		start := time.Now()
		_, err := svg.Parse(strings.NewReader(b.String()))
		elapsed := time.Since(start)
		if !errors.Is(err, svg.ErrExpansionLimit) {
			t.Fatalf("Parse quadratic <use>: got err=%v, want %v", err, svg.ErrExpansionLimit)
		}
		if elapsed > 2*time.Second {
			t.Errorf("Parse quadratic <use> took %v; want fast failure", elapsed)
		}
	})

	t.Run("forward-quadratic-use-fails-fast", func(t *testing.T) {
		const n = 1000
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">`)
		for range n {
			b.WriteString(`<use href="#a"/>`)
		}
		b.WriteString(`<defs><g id="a">`)
		for range n {
			b.WriteString(`<path d="M0 0L1 1"/>`)
		}
		b.WriteString(`</g></defs></svg>`)

		_, err := svg.Parse(strings.NewReader(b.String()))
		if !errors.Is(err, svg.ErrExpansionLimit) {
			t.Fatalf("Parse forward quadratic <use>: got err=%v, want %v", err, svg.ErrExpansionLimit)
		}
	})

	t.Run("10001-trivial-uses-render", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><defs><rect id="r" width="5" height="5" fill="#f00"/></defs>`)
		for range 10000 {
			b.WriteString(`<use href="#r" x="0" y="0"/>`)
		}
		// The 10,001st use sits at (10, 10); it must not be truncated.
		b.WriteString(`<use href="#r" x="10" y="10"/></svg>`)

		d, err := svg.Parse(strings.NewReader(b.String()))
		if err != nil {
			t.Fatalf("Parse 10,001 trivial uses: %v", err)
		}
		dst := drawInto(d, image.Rect(0, 0, 20, 20), ggfx.GeoM{})
		if c := dst.RGBAAt(12, 12); c != (color.RGBA{0xff, 0, 0, 0xff}) {
			t.Errorf("10,001st use pixel (12, 12) = %v; want opaque red", c)
		}
		// Also verify through scene path (DrawWith with override).
		dstScene := image.NewRGBA(image.Rect(0, 0, 20, 20))
		d.DrawWith(dstScene, ggfx.GeoM{}, &svg.DrawOptions{
			Overrides: svg.Overrides{"nonexistent": svg.NodeOverride{}.Opacity(1)},
		})
		if c := dstScene.RGBAAt(12, 12); c != (color.RGBA{0xff, 0, 0, 0xff}) {
			t.Errorf("scene path 10,001st use pixel (12, 12) = %v; want opaque red", c)
		}
	})

	t.Run("5001-rects-with-two-shape-clip", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><defs><clipPath id="c"><rect width="10" height="20"/><rect x="10" width="10" height="20"/></clipPath></defs>`)
		for range 5000 {
			b.WriteString(`<rect width="5" height="5" fill="#0f0" clip-path="url(#c)"/>`)
		}
		b.WriteString(`<rect x="12" y="12" width="5" height="5" fill="#00f" clip-path="url(#c)"/></svg>`)

		d, err := svg.Parse(strings.NewReader(b.String()))
		if err != nil {
			t.Fatalf("Parse 5,001 clipped rects: %v", err)
		}
		dst := drawInto(d, image.Rect(0, 0, 20, 20), ggfx.GeoM{})
		if c := dst.RGBAAt(14, 14); c != (color.RGBA{0, 0, 0xff, 0xff}) {
			t.Errorf("5,001st clipped rect pixel (14, 14) = %v; want opaque blue", c)
		}
	})

	t.Run("65537-direct-rects-succeed", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">`)
		for range 65536 {
			b.WriteString(`<rect width="1" height="1" fill="#f00"/>`)
		}
		b.WriteString(`<rect x="10" y="10" width="4" height="4" fill="#00f"/></svg>`)

		d, err := svg.Parse(strings.NewReader(b.String()))
		if err != nil {
			t.Fatalf("Parse 65,537 direct rects: %v", err)
		}
		dst := drawInto(d, image.Rect(0, 0, 20, 20), ggfx.GeoM{})
		if c := dst.RGBAAt(12, 12); c != (color.RGBA{0, 0, 0xff, 0xff}) {
			t.Errorf("65,537th direct rect pixel (12, 12) = %v; want opaque blue", c)
		}
		// Also verify through scene path.
		dstScene := image.NewRGBA(image.Rect(0, 0, 20, 20))
		if err := d.DrawWithErr(dstScene, ggfx.GeoM{}, &svg.DrawOptions{
			Overrides: svg.Overrides{"unused": svg.NodeOverride{}.Opacity(1)},
		}); err != nil {
			t.Fatalf("DrawWithErr 65,537 direct rects: %v", err)
		}
		if c := dstScene.RGBAAt(12, 12); c != (color.RGBA{0, 0, 0xff, 0xff}) {
			t.Errorf("scene 65,537th direct rect pixel (12, 12) = %v; want opaque blue", c)
		}
	})
}

// Item 12 & M2: crafted binaries cannot amplify memory beyond binary + target size.
func TestReadBinaryMemoryBounded(t *testing.T) {
	// 1. Build a valid binary header via MarshalBinary, walk past version + 4
	// encoded viewBox floats, and corrupt ONLY the path count uvarint so
	// ReadBinary reaches binaryReader.countFor and rejects the bogus count.
	validDoc := parse(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="5" height="5" fill="#f00"/></svg>`)
	validBin, err := validDoc.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	off := 1 // version byte
	for range 4 {
		if off >= len(validBin) {
			t.Fatalf("unexpected short binary header (%d bytes)", len(validBin))
		}
		w := int(validBin[off])
		if w != 4 && w != 8 {
			t.Fatalf("unexpected float width tag %d at offset %d", w, off)
		}
		off += 1 + w
	}
	crafted := append([]byte(nil), validBin[:off]...)
	// Replace only the path count with a huge uvarint (268,435,455) followed by valid trailing bytes.
	crafted = append(crafted, 0xff, 0xff, 0xff, 0x7f)
	crafted = append(crafted, validBin[off+1:]...)

	var badDoc svg.Document
	if err := badDoc.UnmarshalBinary(crafted); err == nil {
		t.Errorf("UnmarshalBinary accepted crafted bogus path-count header")
	}

	// 2. A small valid binary with a huge dashed line and tiny dasharray stays bounded during Draw.
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><path d="M0 50H1000000" fill="none" stroke="#000" stroke-width="2" stroke-dasharray="0.02"/></svg>`
	d := parse(t, src)
	bin, err := d.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	var round svg.Document
	if err := round.UnmarshalBinary(bin); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	round.Draw(dst, ggfx.GeoM{})
	runtime.ReadMemStats(&m1)
	if alloc := m1.TotalAlloc - m0.TotalAlloc; alloc > 4*1024*1024 {
		t.Errorf("Draw on %d-byte binary allocated %d bytes; want < 4 MiB", len(bin), alloc)
	}
	if c := dst.RGBAAt(50, 50); c.A < 100 || c.A > 155 {
		t.Errorf("binary dash pixel (50,50) = %v; want ~50%% duty alpha", c)
	}
}

// D-Q5 benchmark: CSS cascade with many rules and many elements.
func BenchmarkParseCSSCascade(b *testing.B) {
	const n = 2000
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><style>`)
	for i := range n {
		fmt.Fprintf(&sb, "g g g .c%d{fill:#%06x}\n", i, i&0xffffff)
	}
	sb.WriteString(`</style><g><g><g>`)
	for i := range n {
		fmt.Fprintf(&sb, `<rect class="c%d" width="1" height="1"/>`, i)
	}
	sb.WriteString(`</g></g></g></svg>`)
	src := sb.String()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := svg.Parse(strings.NewReader(src)); err != nil {
			b.Fatal(err)
		}
	}
}
