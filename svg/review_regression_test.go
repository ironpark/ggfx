// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/svg"
)

func pixHash16(pix []byte) string {
	s := sha256.Sum256(pix)
	return hex.EncodeToString(s[:])[:16]
}

func fillRGBA(img *image.RGBA, c color.RGBA) {
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
}

// Item 1: nested clipPath/mask combinations under DrawWith and Renderer.Draw
// must never panic and must resolve nested scopes independently of the outer
// mask's own shapes.
func TestNestedClipMaskMatrixDrawWith(t *testing.T) {
	cases := []struct {
		name         string
		src          string
		wantHash     string // expected HEAD hash when HEAD did not panic
		insidePt     image.Point
		insideColor  color.RGBA
		outsidePt    image.Point
		outsideColor color.RGBA
	}{
		{
			name: "clip-inside-mask",
			src: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><defs>
				<clipPath id="innerClip"><rect x="10" y="10" width="15" height="15"/></clipPath>
				<mask id="outerMask"><rect x="0" y="0" width="40" height="40" fill="#fff" clip-path="url(#innerClip)"/></mask>
			</defs><rect width="40" height="40" fill="#f00" mask="url(#outerMask)"/></svg>`,
			insidePt:     image.Pt(15, 15),
			insideColor:  color.RGBA{255, 0, 0, 255},
			outsidePt:    image.Pt(28, 28),
			outsideColor: color.RGBA{0, 0, 0, 0},
		},
		{
			name: "curve-clip-inside-mask",
			src: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><defs>
				<clipPath id="innerClip"><circle cx="15" cy="15" r="8"/></clipPath>
				<mask id="outerMask"><rect x="0" y="0" width="40" height="40" fill="#fff" clip-path="url(#innerClip)"/></mask>
			</defs><rect width="40" height="40" fill="#f00" mask="url(#outerMask)"/></svg>`,
			insidePt:     image.Pt(15, 15),
			insideColor:  color.RGBA{255, 0, 0, 255},
			outsidePt:    image.Pt(28, 28),
			outsideColor: color.RGBA{0, 0, 0, 0},
		},
		{
			name: "mask-inside-mask",
			src: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><defs>
				<mask id="innerMask"><circle cx="15" cy="15" r="10" fill="#808080"/></mask>
				<mask id="outerMask"><rect x="0" y="0" width="40" height="40" fill="#fff" mask="url(#innerMask)"/></mask>
			</defs><rect width="40" height="40" fill="#f00" mask="url(#outerMask)"/></svg>`,
			insidePt:     image.Pt(15, 15),
			insideColor:  color.RGBA{128, 0, 0, 128},
			outsidePt:    image.Pt(30, 30),
			outsideColor: color.RGBA{0, 0, 0, 0},
		},
		{
			name: "mask-inside-clip",
			src: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><defs>
				<mask id="innerMask"><rect x="10" y="10" width="20" height="20" fill="#808080"/></mask>
				<clipPath id="outerClip"><rect x="5" y="5" width="30" height="30" mask="url(#innerMask)"/></clipPath>
			</defs><rect width="40" height="40" fill="#0f0" clip-path="url(#outerClip)"/></svg>`,
			wantHash:     "3f33af7357b438c0",
			insidePt:     image.Pt(15, 15),
			insideColor:  color.RGBA{0, 255, 0, 255},
			outsidePt:    image.Pt(2, 2),
			outsideColor: color.RGBA{0, 0, 0, 0},
		},
		{
			name: "mask-under-clip",
			src: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><defs>
				<clipPath id="outerClip"><rect x="5" y="5" width="20" height="20"/></clipPath>
				<mask id="innerMask"><rect x="10" y="10" width="20" height="20" fill="#808080"/></mask>
			</defs><g clip-path="url(#outerClip)"><rect width="40" height="40" fill="#00f" mask="url(#innerMask)"/></g></svg>`,
			wantHash:     "3039c8bcbc941fdd",
			insidePt:     image.Pt(15, 15),
			insideColor:  color.RGBA{0, 0, 128, 128},
			outsidePt:    image.Pt(26, 26),
			outsideColor: color.RGBA{0, 0, 0, 0},
		},
		{
			name: "clip-under-mask",
			src: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><defs>
				<mask id="outerMask"><rect x="5" y="5" width="25" height="25" fill="#808080"/></mask>
				<clipPath id="innerClip"><rect x="10" y="10" width="20" height="20"/></clipPath>
			</defs><g mask="url(#outerMask)"><rect width="40" height="40" fill="#f0f" clip-path="url(#innerClip)"/></g></svg>`,
			wantHash:     "e47f262e8c0e836a",
			insidePt:     image.Pt(15, 15),
			insideColor:  color.RGBA{128, 0, 128, 128},
			outsidePt:    image.Pt(5, 5),
			outsideColor: color.RGBA{0, 0, 0, 0},
		},
	}

	nonNilOpts := &svg.DrawOptions{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := parse(t, tc.src)
			checkImg := func(entry string, dst *image.RGBA) {
				t.Helper()
				if got := dst.RGBAAt(tc.insidePt.X, tc.insidePt.Y); got != tc.insideColor {
					t.Errorf("%s inside %v = %v, want %v", entry, tc.insidePt, got, tc.insideColor)
				}
				if got := dst.RGBAAt(tc.outsidePt.X, tc.outsidePt.Y); got != tc.outsideColor {
					t.Errorf("%s outside %v = %v, want %v", entry, tc.outsidePt, got, tc.outsideColor)
				}
				if tc.wantHash != "" {
					if got := pixHash16(dst.Pix); got != tc.wantHash {
						t.Errorf("%s hash = %s, want HEAD %s", entry, got, tc.wantHash)
					}
				}
			}

			// Test every public entry point with no dummy override.
			dstDraw := image.NewRGBA(image.Rect(0, 0, 40, 40))
			d.Draw(dstDraw, ggfx.GeoM{})
			checkImg("Draw", dstDraw)

			dstDrawErr := image.NewRGBA(image.Rect(0, 0, 40, 40))
			if err := d.DrawErr(dstDrawErr, ggfx.GeoM{}); err != nil {
				t.Fatalf("DrawErr: %v", err)
			}
			checkImg("DrawErr", dstDrawErr)

			dstWithNil := image.NewRGBA(image.Rect(0, 0, 40, 40))
			d.DrawWith(dstWithNil, ggfx.GeoM{}, nil)
			checkImg("DrawWith(nil)", dstWithNil)

			dstWithErrNil := image.NewRGBA(image.Rect(0, 0, 40, 40))
			if err := d.DrawWithErr(dstWithErrNil, ggfx.GeoM{}, nil); err != nil {
				t.Fatalf("DrawWithErr(nil): %v", err)
			}
			checkImg("DrawWithErr(nil)", dstWithErrNil)

			dstWith := image.NewRGBA(image.Rect(0, 0, 40, 40))
			d.DrawWith(dstWith, ggfx.GeoM{}, nonNilOpts)
			checkImg("DrawWith(non-nil)", dstWith)

			dstWithErr := image.NewRGBA(image.Rect(0, 0, 40, 40))
			if err := d.DrawWithErr(dstWithErr, ggfx.GeoM{}, nonNilOpts); err != nil {
				t.Fatalf("DrawWithErr(non-nil): %v", err)
			}
			checkImg("DrawWithErr(non-nil)", dstWithErr)

			r := d.NewRenderer()
			defer r.Release()
			dstRNil := image.NewRGBA(image.Rect(0, 0, 40, 40))
			r.Draw(dstRNil, ggfx.GeoM{}, nil)
			checkImg("Renderer.Draw(nil)", dstRNil)

			dstRErrNil := image.NewRGBA(image.Rect(0, 0, 40, 40))
			if err := r.DrawErr(dstRErrNil, ggfx.GeoM{}, nil); err != nil {
				t.Fatalf("Renderer.DrawErr(nil): %v", err)
			}
			checkImg("Renderer.DrawErr(nil)", dstRErrNil)

			dstR := image.NewRGBA(image.Rect(0, 0, 40, 40))
			r.Draw(dstR, ggfx.GeoM{}, nonNilOpts)
			checkImg("Renderer.Draw(non-nil)", dstR)

			dstRErr := image.NewRGBA(image.Rect(0, 0, 40, 40))
			if err := r.DrawErr(dstRErr, ggfx.GeoM{}, nonNilOpts); err != nil {
				t.Fatalf("Renderer.DrawErr(non-nil): %v", err)
			}
			checkImg("Renderer.DrawErr(non-nil)", dstRErr)

			if !bytes.Equal(dstDraw.Pix, dstWithErr.Pix) || !bytes.Equal(dstDraw.Pix, dstRErr.Pix) {
				t.Errorf("pixels differ across public entry points")
			}
		})
	}
}

// Item 2: axisAlignedIntRect must reject backtracking polygons and sub-pixel
// translated rectangles, matching HEAD bit-for-bit on both Draw and DrawWith.
func TestClipRectClassificationExact(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantHash string
	}{
		{
			name:     "backtracking-polygon-empty",
			src:      `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><defs><clipPath id="c"><path d="M3 3H32L3 3V30Z"/></clipPath></defs><rect width="40" height="40" fill="#f00" clip-path="url(#c)"/></svg>`,
			wantHash: "56a43ef88ddfcd0f",
		},
		{
			name:     "subpixel-translate-rect",
			src:      `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><defs><clipPath id="c" transform="translate(-.0000003)"><rect x="4" y="4" width="20" height="20"/></clipPath></defs><rect width="40" height="40" fill="#f00" clip-path="url(#c)"/></svg>`,
			wantHash: "d34d9d2ad11e11d2",
		},
	}

	opts := &svg.DrawOptions{Overrides: svg.Overrides{"unused": svg.NodeOverride{}.Opacity(1)}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := parse(t, tc.src)
			dst := drawInto(d, image.Rect(0, 0, 40, 40), ggfx.GeoM{})
			if got := pixHash16(dst.Pix); got != tc.wantHash {
				t.Errorf("Draw hash = %s, want HEAD %s", got, tc.wantHash)
			}
			dstW := image.NewRGBA(image.Rect(0, 0, 40, 40))
			d.DrawWith(dstW, ggfx.GeoM{}, opts)
			if got := pixHash16(dstW.Pix); got != tc.wantHash {
				t.Errorf("DrawWith hash = %s, want HEAD %s", got, tc.wantHash)
			}
		})
	}
}

// Item 3: gradients must apply PadSpread endpoint rules (t >= 1 -> last stop)
// and exact per-pixel t projection bit-identically to HEAD across linear,
// radial, coincident stops, and wide rows up to 8191 px.
func TestGradientExactVsHEAD(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		w, h      int
		wantHash  string
		checkPt   image.Point
		wantColor color.RGBA
	}{
		{
			name:      "pad-endpoint-linear",
			src:       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 40"><defs><linearGradient id="g" gradientUnits="userSpaceOnUse" x1=".5" x2="32.5"><stop offset="0" stop-color="#f02"/><stop offset="1" stop-color="#0fa"/><stop offset="1" stop-color="#30f"/></linearGradient></defs><rect width="64" height="40" fill="url(#g)"/></svg>`,
			w:         64,
			h:         40,
			wantHash:  "c0ceda256438ef9d",
			checkPt:   image.Pt(32, 20),
			wantColor: color.RGBA{0x33, 0x00, 0xff, 0xff},
		},
		{
			name:      "pad-endpoint-radial-centered",
			src:       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 40"><defs><radialGradient id="g" gradientUnits="userSpaceOnUse" cx=".5" cy=".5" r="32"><stop offset="0" stop-color="#f02"/><stop offset="1" stop-color="#0fa"/><stop offset="1" stop-color="#30f"/></radialGradient></defs><rect width="64" height="40" fill="url(#g)"/></svg>`,
			w:         64,
			h:         40,
			wantHash:  "b3766fe9adbba673",
			checkPt:   image.Pt(32, 0),
			wantColor: color.RGBA{0x33, 0x00, 0xff, 0xff},
		},
		{
			name:      "coincident-stop-0.95-10px",
			src:       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><defs><linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" x2="10"><stop offset=".95" stop-color="#f00"/><stop offset=".95" stop-color="#00f"/></linearGradient></defs><rect width="10" height="10" fill="url(#g)"/></svg>`,
			w:         10,
			h:         10,
			wantHash:  "834f7685ed723160",
			checkPt:   image.Pt(9, 5),
			wantColor: color.RGBA{0xff, 0x00, 0x00, 0xff},
		},
		{
			name:      "wide-row-8191",
			src:       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8191 2"><defs><linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" x2="8191"><stop offset="0" stop-color="#f00"/><stop offset="0.75" stop-color="#0f0"/><stop offset="0.75" stop-color="#00f"/><stop offset="1" stop-color="#ff0"/></linearGradient></defs><rect width="8191" height="2" fill="url(#g)"/></svg>`,
			w:         8191,
			h:         2,
			wantHash:  "c70df94eef737396",
			checkPt:   image.Pt(6143, 0),
			wantColor: color.RGBA{0x00, 0x00, 0xff, 0xff},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := parse(t, tc.src)
			dst := drawInto(d, image.Rect(0, 0, tc.w, tc.h), ggfx.GeoM{})
			if got := dst.RGBAAt(tc.checkPt.X, tc.checkPt.Y); got != tc.wantColor {
				t.Errorf("pixel %v = %v, want %v", tc.checkPt, got, tc.wantColor)
			}
			if got := pixHash16(dst.Pix); got != tc.wantHash {
				t.Errorf("hash = %s, want HEAD %s", got, tc.wantHash)
			}
		})
	}
}

// Item 4: draw-time expansion overflow returns ErrExpansionLimit from the
// error-returning Draw entry points, leaves dst untouched, causes Bounds to
// report false, and allows subsequent under-budget frames to render normally.
func TestDrawTimeLimitErrorGuarantees(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><defs><g id="chunk">`)
	for range 400 {
		b.WriteString(`<rect width="1" height="1"/>`)
	}
	b.WriteString(`</g></defs>`)
	b.WriteString(`<rect id="base" x="0" y="0" width="10" height="10" fill="#0f0"/>`)
	// Hidden at t=0; SMIL switches it visible during [1s, 2s), triggering 400*200 = 80,000 > 65,536 expansions.
	b.WriteString(`<g id="heavy" display="none">`)
	b.WriteString(`<set attributeName="display" to="inline" begin="1s" dur="1s"/>`)
	for range 200 {
		b.WriteString(`<use href="#chunk"/>`)
	}
	b.WriteString(`</g></svg>`)

	d := parse(t, b.String())

	// Under-budget frame at t=0 succeeds and paints green base rect.
	dst0 := image.NewRGBA(image.Rect(0, 0, 20, 20))
	if err := d.DrawWithErr(dst0, ggfx.GeoM{}, &svg.DrawOptions{Time: 0}); err != nil {
		t.Fatalf("t=0 DrawWithErr: %v", err)
	}
	if got := dst0.RGBAAt(5, 5); got != (color.RGBA{0, 255, 0, 255}) {
		t.Fatalf("t=0 base pixel = %v, want green", got)
	}
	if _, ok := d.Bounds("base", &svg.DrawOptions{Time: 0}); !ok {
		t.Fatalf("t=0 Bounds(base) = false, want true")
	}

	// Over-budget frame at t=1.5s returns ErrExpansionLimit, leaves dst untouched, Bounds reports false.
	sentinel := color.RGBA{12, 34, 56, 200}
	dstOver := image.NewRGBA(image.Rect(0, 0, 20, 20))
	fillRGBA(dstOver, sentinel)
	origPix := append([]byte(nil), dstOver.Pix...)

	err := d.DrawWithErr(dstOver, ggfx.GeoM{}, &svg.DrawOptions{Time: 1500 * time.Millisecond})
	if !errors.Is(err, svg.ErrExpansionLimit) {
		t.Fatalf("t=1.5s DrawWithErr = %v, want ErrExpansionLimit", err)
	}
	if !bytes.Equal(dstOver.Pix, origPix) {
		t.Errorf("DrawWithErr modified dst on ErrExpansionLimit")
	}
	if _, ok := d.Bounds("base", &svg.DrawOptions{Time: 1500 * time.Millisecond}); ok {
		t.Errorf("t=1.5s Bounds(base) = true, want false on over-budget frame")
	}

	// Renderer.DrawErr obeys the same contract and recovers on the next under-budget frame (t=2.5s).
	rend := d.NewRenderer()
	defer rend.Release()
	if err := rend.DrawErr(dstOver, ggfx.GeoM{}, &svg.DrawOptions{Time: 1500 * time.Millisecond}); !errors.Is(err, svg.ErrExpansionLimit) {
		t.Fatalf("Renderer.DrawErr t=1.5s = %v, want ErrExpansionLimit", err)
	}
	if !bytes.Equal(dstOver.Pix, origPix) {
		t.Errorf("Renderer.DrawErr modified dst on ErrExpansionLimit")
	}
	dstRecover := image.NewRGBA(image.Rect(0, 0, 20, 20))
	if err := rend.DrawErr(dstRecover, ggfx.GeoM{}, &svg.DrawOptions{Time: 2500 * time.Millisecond}); err != nil {
		t.Fatalf("Renderer.DrawErr t=2.5s = %v, want nil", err)
	}
	if got := dstRecover.RGBAAt(5, 5); got != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("t=2.5s recovered base pixel = %v, want green", got)
	}
}

// Item 5: legacy and scene paths share one logical depth counter; a 90-sibling
// <use> chain (depth ~183 < 256) succeeds on both paths and matches HEAD, while
// a chain exceeding 256 fails on both with ErrNestingDepth.
func TestNestingDepthUnified(t *testing.T) {
	makeChain := func(n int, forward bool) string {
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">`)
		if forward {
			fmt.Fprintf(&b, `<use href="#a%d"/>`, n)
		}
		b.WriteString(`<defs><rect id="a0" x="2" y="2" width="16" height="16" fill="#0a0"/>`)
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&b, `<g id="a%d"><use href="#a%d"/></g>`, i, i-1)
		}
		b.WriteString(`</defs>`)
		if !forward {
			fmt.Fprintf(&b, `<use href="#a%d"/>`, n)
		}
		b.WriteString(`</svg>`)
		return b.String()
	}

	d := parse(t, makeChain(90, false))
	dstDraw := drawInto(d, image.Rect(0, 0, 20, 20), ggfx.GeoM{})
	const wantHash = "dff4acebcd6f7b08"
	if got := pixHash16(dstDraw.Pix); got != wantHash {
		t.Errorf("90-chain Draw hash = %s, want HEAD %s", got, wantHash)
	}
	dstWith := image.NewRGBA(image.Rect(0, 0, 20, 20))
	if err := d.DrawWithErr(dstWith, ggfx.GeoM{}, &svg.DrawOptions{Overrides: svg.Overrides{"none": svg.NodeOverride{}.Opacity(1)}}); err != nil {
		t.Fatalf("90-chain DrawWithErr: %v", err)
	}
	if got := pixHash16(dstWith.Pix); got != wantHash {
		t.Errorf("90-chain DrawWith hash = %s, want HEAD %s", got, wantHash)
	}

	for _, forward := range []bool{false, true} {
		name := "legacy-exceeds"
		if forward {
			name = "scene-forward-exceeds"
		}
		t.Run(name, func(t *testing.T) {
			_, err := svg.Parse(strings.NewReader(makeChain(130, forward)))
			if !errors.Is(err, svg.ErrNestingDepth) {
				t.Fatalf("Parse 130-chain (forward=%v): got err=%v, want %v", forward, err, svg.ErrNestingDepth)
			}
		})
	}
}

// Item 6: @keyframes offset merging must preserve exact abs(delta) < 1e-9
// earliest-insertion matching against HEAD.
func TestKeyframesNearEqualOffsetsExact(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><style>@keyframes k{0%{opacity:0}50.00000004%{opacity:.1}50.00000006%{opacity:.9}100%{opacity:1}}rect{animation:k 2s linear forwards}</style><rect width="16" height="16" fill="#f00"/></svg>`
	d := parse(t, src)
	dst := image.NewRGBA(image.Rect(0, 0, 16, 16))
	d.DrawWith(dst, ggfx.GeoM{}, &svg.DrawOptions{Time: 500 * time.Millisecond})
	if got := dst.RGBAAt(8, 8); got != (color.RGBA{114, 0, 0, 114}) {
		t.Errorf("pixel (8,8) = %v, want {114 0 0 114}", got)
	}
	if got := pixHash16(dst.Pix); got != "d45a9f59ccabda67" {
		t.Errorf("hash = %s, want HEAD d45a9f59ccabda67", got)
	}
}

// Item 7: dashed stroking matches HEAD bit-for-bit on dense legitimate paths,
// prefilled offscreen-entering lines, and closed curves with zero-length dashes,
// while sub-quarter-pixel patterns and budget-exhausted strokes render at their
// duty-cycle average coverage rather than turning solid.
func TestDashExactAndDutyCycle(t *testing.T) {
	t.Run("135-rows-512-wide", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512"><path d="`)
		for i := range 135 {
			fmt.Fprintf(&b, "M0 %dH512 ", 1+2*i)
		}
		b.WriteString(`" fill="none" stroke="#000" stroke-width="1" stroke-dasharray="1 1"/></svg>`)
		d := parse(t, b.String())
		dst := drawInto(d, image.Rect(0, 0, 512, 512), ggfx.GeoM{})
		if got := pixHash16(dst.Pix); got != "6a134edd59ff4936" {
			t.Errorf("135-row hash = %s, want HEAD 6a134edd59ff4936", got)
		}
	})

	t.Run("prefilled-offscreen-line", func(t *testing.T) {
		const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 40"><path d="M-68 17L120 18" fill="none" stroke="red" stroke-width="4" stroke-dasharray="2 4 7" stroke-dashoffset="-11"/></svg>`
		d := parse(t, src)
		dst := image.NewRGBA(image.Rect(0, 0, 64, 40))
		fillRGBA(dst, color.RGBA{15, 30, 11, 60})
		d.Draw(dst, ggfx.GeoM{})
		if got := dst.RGBAAt(48, 15); got != (color.RGBA{103, 19, 6, 132}) {
			t.Errorf("pixel (48,15) = %v, want {103 19 6 132}", got)
		}
		if got := pixHash16(dst.Pix); got != "cd3495f91f27d1bf" {
			t.Errorf("hash = %s, want HEAD cd3495f91f27d1bf", got)
		}
	})

	t.Run("closed-quad-round-zero-nonuniform", func(t *testing.T) {
		const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 40"><g transform="scale(2,0.5)"><path d="M-40 20 Q40 120 120 20 Z" fill="none" stroke="red" stroke-width="4" stroke-linecap="round" stroke-dasharray="0 6 4 0" stroke-dashoffset="-5"/></g></svg>`
		d := parse(t, src)
		dst := drawInto(d, image.Rect(0, 0, 64, 40), ggfx.GeoM{})
		if got := pixHash16(dst.Pix); got != "d0a96fc57cf092e5" {
			t.Errorf("hash = %s, want HEAD d0a96fc57cf092e5", got)
		}
	})

	t.Run("subquarter-pixel-duty-cycle", func(t *testing.T) {
		// Duty cycle is 0.015625 / (0.015625 + 0.1875) = 1/13 ≈ 7.69% -> red ~ 19-20, not solid 255.
		const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 20"><path d="M0 10H40" fill="none" stroke="#f00" stroke-width="4" stroke-dasharray=".015625 .1875"/></svg>`
		d := parse(t, src)
		dst := drawInto(d, image.Rect(0, 0, 40, 20), ggfx.GeoM{})
		c := dst.RGBAAt(20, 10)
		if c.R < 15 || c.R > 25 || c.A < 15 || c.A > 25 {
			t.Errorf("subquarter-pixel duty pixel (20,10) = %v, want red/alpha near 20 (1/13 duty cycle)", c)
		}
	})

	t.Run("per-draw-budget-exhaustion-duty-cycle", func(t *testing.T) {
		// On a 16x16 target, per-draw dash budget is 16,384 transitions.
		// Emit 300 paths at y=2 exhausting the budget (each ~128 transitions -> 38,400 > 16,384),
		// then draw a final distinct path at y=12 with 10% duty cycle ("0.5 4.5"):
		// it must render at ~10% alpha (~25), never solid 255.
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16">`)
		for range 300 {
			b.WriteString(`<path d="M0 2H16" fill="none" stroke="#000" stroke-width="1" stroke-dasharray="0.125 0.125"/>`)
		}
		b.WriteString(`<path d="M0 12H16" fill="none" stroke="#f00" stroke-width="2" stroke-dasharray="0.5 4.5"/>`)
		b.WriteString(`</svg>`)
		d := parse(t, b.String())
		dst := drawInto(d, image.Rect(0, 0, 16, 16), ggfx.GeoM{})
		c := dst.RGBAAt(8, 12)
		if c.R < 18 || c.R > 35 {
			t.Errorf("post-budget path pixel (8,12) = %v, want ~10%% duty red (~25), not solid 255", c)
		}
	})

	// Item 7a: long left-entering dashed line on prefilled translucent backing matches HEAD bit-for-bit.
	t.Run("long-left-panned-line", func(t *testing.T) {
		const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 40"><path d="M-4100 21.8L72 12.6584" fill="none" stroke="red" stroke-width="4" stroke-linecap="butt" stroke-dasharray=".75 1.25" stroke-dashoffset="-3"/></svg>`
		d := parse(t, src)
		dst := image.NewRGBA(image.Rect(0, 0, 64, 40))
		fillRGBA(dst, color.RGBA{15, 30, 11, 60})
		d.Draw(dst, ggfx.GeoM{})
		if got := dst.RGBAAt(1, 11); got != (color.RGBA{191, 7, 2, 203}) {
			t.Errorf("pixel (1,11) = %v, want HEAD {191 7 2 203}", got)
		}
	})

	// Item 7b: auxiliary clip/mask painters share the main draw's transition budget.
	t.Run("auxiliary-mask-shares-budget", func(t *testing.T) {
		// 270 horizontal 1/1 dashed strokes on 64x40 emit ~17,280 transitions (>16,384 budget),
		// so the 271st stroke (2/2 dashed) inside the luminance mask must render at 50% duty
		// coverage uniformly across y=30 rather than alternating 100%/0% dashes.
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 40"><defs><mask id="m">`)
		for range 270 {
			b.WriteString(`<path d="M0 4H64" fill="none" stroke="#fff" stroke-width="1" stroke-dasharray="1 1"/>`)
		}
		b.WriteString(`<path d="M0 30H64" fill="none" stroke="#fff" stroke-width="4" stroke-dasharray="2 2"/>`)
		b.WriteString(`</mask></defs><rect width="64" height="40" fill="#f00" mask="url(#m)"/></svg>`)
		d := parse(t, b.String())
		dst := drawInto(d, image.Rect(0, 0, 64, 40), ggfx.GeoM{})
		// At y=30, if budget was shared, both x=1 and x=3 (which would be dash vs gap) get ~50% red.
		c1 := dst.RGBAAt(1, 30)
		c3 := dst.RGBAAt(3, 30)
		if c1.R < 100 || c1.R > 155 || c3.R < 100 || c3.R > 155 {
			t.Errorf("mask post-budget pixels (1,30)=%v (3,30)=%v; want both ~50%% duty red", c1, c3)
		}
	})

	// Item 7c: sub-quarter-pixel dot patterns include cap footprints for round/square caps
	// (matching HEAD within ±2 channel tolerance) while butt caps keep pure centreline duty.
	t.Run("subquarter-pixel-dot-caps", func(t *testing.T) {
		for _, capName := range []string{"round", "square"} {
			src := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 20"><path d="M4 10H36" fill="none" stroke="red" stroke-width="4" stroke-linecap="%s" stroke-dasharray="0 .125"/></svg>`, capName)
			d := parse(t, src)
			dst := drawInto(d, image.Rect(0, 0, 40, 20), ggfx.GeoM{})
			c := dst.RGBAAt(20, 10)
			if c.R < 253 || c.A < 253 {
				t.Errorf("linecap=%s dot pixel (20,10) = %v; want opaque red (within ±2 of 255)", capName, c)
			}
		}
		// Butt cap with zero-length dash has 0 cap footprint -> paints nothing.
		buttDot := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 20"><path d="M4 10H36" fill="none" stroke="red" stroke-width="4" stroke-linecap="butt" stroke-dasharray="0 .125"/></svg>`
		dButt := parse(t, buttDot)
		dstButt := drawInto(dButt, image.Rect(0, 0, 40, 20), ggfx.GeoM{})
		if c := dstButt.RGBAAt(20, 10); c.A != 0 {
			t.Errorf("linecap=butt zero-dash pixel (20,10) = %v; want transparent", c)
		}
	})
}

// Item 4a: display cascade resolves winning local display declaration before
// combining with inherited hidden state on both Draw and DrawWith.
func TestDisplayCascadeLegacyAndScene(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantColor color.RGBA
	}{
		{
			name:      "attr-none-style-inline",
			src:       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><rect width="20" height="20" fill="red" display="none" style="display:inline"/></svg>`,
			wantColor: color.RGBA{255, 0, 0, 255},
		},
		{
			name:      "style-none-then-inline",
			src:       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><rect width="20" height="20" fill="red" style="display:none;display:inline"/></svg>`,
			wantColor: color.RGBA{255, 0, 0, 255},
		},
		{
			name:      "style-inline-then-none",
			src:       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><rect width="20" height="20" fill="red" style="display:inline;display:none"/></svg>`,
			wantColor: color.RGBA{0, 0, 0, 0},
		},
		{
			name:      "attr-none-class-inline",
			src:       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><style>.show{display:inline}</style><rect width="20" height="20" fill="red" display="none" class="show"/></svg>`,
			wantColor: color.RGBA{255, 0, 0, 255},
		},
		{
			name:      "inherited-none-cannot-be-unhidden",
			src:       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><g display="none"><rect width="20" height="20" fill="red" style="display:inline"/></g></svg>`,
			wantColor: color.RGBA{0, 0, 0, 0},
		},
		{
			name:      "style-important-none-wins",
			src:       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><rect width="20" height="20" fill="red" style="display:none !important;display:inline"/></svg>`,
			wantColor: color.RGBA{0, 0, 0, 0},
		},
	}

	opts := &svg.DrawOptions{Overrides: svg.Overrides{"unused": svg.NodeOverride{}.Opacity(1)}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := parse(t, tc.src)
			dstDraw := drawInto(d, image.Rect(0, 0, 20, 20), ggfx.GeoM{})
			if got := dstDraw.RGBAAt(10, 10); got != tc.wantColor {
				t.Errorf("Draw (10,10) = %v, want %v", got, tc.wantColor)
			}
			dstWith := image.NewRGBA(image.Rect(0, 0, 20, 20))
			d.DrawWith(dstWith, ggfx.GeoM{}, opts)
			if got := dstWith.RGBAAt(10, 10); got != tc.wantColor {
				t.Errorf("DrawWith (10,10) = %v, want %v", got, tc.wantColor)
			}
		})
	}
}

// Item 4b: hidden descendants inside referenced definitions must not be expanded
// or bulk-charged against the expansion budget.
func TestHiddenDefinitionNotCharged(t *testing.T) {
	// Item 4b & Y1: hidden descendants inside referenced definitions must not fail
	// Parse regardless of how the layer is hidden (all 7 CSS/SVG hiding forms).
	cases := []struct {
		name      string
		earlyCSS  string
		lateCSS   string
		layerAttr string
	}{
		{name: "presentation-attr", layerAttr: `display="none"`},
		{name: "inline-style", layerAttr: `style="display:none"`},
		{name: "early-simple-class", earlyCSS: `.off{display:none}`, layerAttr: `class="off"`},
		{name: "early-important-class", earlyCSS: `.off{display:none !important}`, layerAttr: `class="off"`},
		{name: "early-id-selector", earlyCSS: `#hidden{display:none}`},
		{name: "early-descendant-selector", earlyCSS: `defs #hidden{display:none}`},
		{name: "late-simple-class", lateCSS: `.off{display:none}`, layerAttr: `class="off"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">`)
			if tc.earlyCSS != "" {
				fmt.Fprintf(&b, `<style>%s</style>`, tc.earlyCSS)
			}
			b.WriteString(`<defs><g id="tile">`)
			for range 256 {
				b.WriteString(`<rect width="1" height="1"/>`)
			}
			fmt.Fprintf(&b, `</g><g id="hidden" %s>`, tc.layerAttr)
			for range 256 {
				b.WriteString(`<use href="#tile"/>`)
			}
			b.WriteString(`</g></defs><use href="#hidden"/><rect width="20" height="20" fill="red"/>`)
			if tc.lateCSS != "" {
				fmt.Fprintf(&b, `<style>%s</style>`, tc.lateCSS)
			}
			b.WriteString(`</svg>`)

			d, err := svg.Parse(strings.NewReader(b.String()))
			if err != nil {
				t.Fatalf("Parse hidden definition (%s): %v", tc.name, err)
			}
			dst := drawInto(d, image.Rect(0, 0, 20, 20), ggfx.GeoM{})
			if got := dst.RGBAAt(10, 10); got != (color.RGBA{255, 0, 0, 255}) {
				t.Errorf("Draw (10,10) = %v, want opaque red", got)
			}
			dstWith := image.NewRGBA(image.Rect(0, 0, 20, 20))
			if err := d.DrawWithErr(dstWith, ggfx.GeoM{}, &svg.DrawOptions{Overrides: svg.Overrides{"unused": svg.NodeOverride{}.Opacity(1)}}); err != nil {
				t.Fatalf("DrawWithErr: %v", err)
			}
			if got := dstWith.RGBAAt(10, 10); got != (color.RGBA{255, 0, 0, 255}) {
				t.Errorf("DrawWithErr (10,10) = %v, want opaque red", got)
			}
		})
	}
}

// Item Y2: mask/clip measurement must not charge dash transitions against the
// shared per-draw budget; only the paint pass charges once per logical transition.
func TestDashMaskSingleCharge(t *testing.T) {
	// 140 white 1/1 dashed strokes across 64px in a luminance mask (~8,960 logical
	// transitions < 16,384 floor), followed by an independent red 2/2 dashed stroke
	// at y=23.5: (12,23) must be opaque red and (14,23) must be a transparent gap.
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 40"><defs><mask id="m">`)
	for range 140 {
		b.WriteString(`<path d="M0 10.5H64" fill="none" stroke="#fff" stroke-width="1" stroke-dasharray="1 1"/>`)
	}
	b.WriteString(`</mask></defs><rect width="64" height="20" fill="#0f0" mask="url(#m)"/>`)
	b.WriteString(`<path d="M0 23.5H64" fill="none" stroke="#f00" stroke-width="1" stroke-dasharray="2 2"/></svg>`)

	d := parse(t, b.String())
	check := func(label string, dst *image.RGBA) {
		t.Helper()
		if got := dst.RGBAAt(12, 23); got != (color.RGBA{255, 0, 0, 255}) {
			t.Errorf("%s painted dash pixel (12,23) = %v; want opaque red", label, got)
		}
		if got := dst.RGBAAt(14, 23); got.A != 0 {
			t.Errorf("%s gap pixel (14,23) = %v; want transparent", label, got)
		}
	}

	dstStatic := drawInto(d, image.Rect(0, 0, 64, 40), ggfx.GeoM{})
	check("static Draw", dstStatic)

	dstScene := image.NewRGBA(image.Rect(0, 0, 64, 40))
	if err := d.DrawWithErr(dstScene, ggfx.GeoM{}, &svg.DrawOptions{Overrides: svg.Overrides{"unused": svg.NodeOverride{}.Opacity(1)}}); err != nil {
		t.Fatalf("forced-scene DrawWithErr: %v", err)
	}
	check("forced-scene DrawWithErr", dstScene)
	if !bytes.Equal(dstStatic.Pix, dstScene.Pix) {
		t.Errorf("static Draw and forced-scene DrawWithErr differ")
	}
}

// Item Y3: an unused nested clipPath/mask in <defs> must not prevent a plain
// document from keeping its binary form.
func TestUnusedNestedClipPreservesBinaryForm(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><defs><rect id="s" width="10" height="10"/><clipPath id="unused"><use href="#s"/></clipPath></defs><rect width="20" height="20" fill="#f00"/></svg>`
	d := parse(t, src)
	bin, err := d.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary with unused nested clipPath: %v", err)
	}
	var round svg.Document
	if err := round.UnmarshalBinary(bin); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	want := drawInto(d, image.Rect(0, 0, 20, 20), ggfx.GeoM{})
	got := drawInto(&round, image.Rect(0, 0, 20, 20), ggfx.GeoM{})
	if !bytes.Equal(want.Pix, got.Pix) {
		t.Errorf("unmarshaled binary rendered differently from original")
	}
	if c := got.RGBAAt(10, 10); c != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("pixel (10,10) = %v; want opaque red", c)
	}
}

// Item 8: LocalBounds must propagate aborts (depth or work budget) to the outer
// query and return false without caching a partial rectangle.
func TestNodeLocalBoundsAbortNotCached(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs>`)
	b.WriteString(`<rect id="c0" x="50" y="0" width="10" height="10"/>`)
	for i := 1; i <= 130; i++ {
		fmt.Fprintf(&b, `<g id="c%d"><use href="#c%d"/></g>`, i, i-1)
	}
	b.WriteString(`<g id="map"><rect width="1" height="1"/><use href="#c130"/></g>`)
	// Also test transform-origin fill-box threading into a deep chain.
	b.WriteString(`<g id="originMap"><g transform-box="fill-box" transform-origin="50% 50%" transform="rotate(45)"><rect width="1" height="1"/><use href="#c130"/></g></g>`)
	b.WriteString(`</defs></svg>`)

	d := parse(t, b.String())
	for _, id := range []string{"map", "originMap"} {
		n, ok := d.Node(id)
		if !ok {
			t.Fatalf("Node(%q) not found", id)
		}
		for call := range 2 {
			if r, has := n.LocalBounds(); has {
				t.Errorf("%s call %d: LocalBounds = (%+v, true); want (zero, false) on depth abort", id, call, r)
			}
		}
	}
}

// Item 8: Node.LocalBounds must obey the expansion work budget even when called
// on a hidden/defs container independently of rendering.
func TestNodeLocalBoundsBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><defs><g id="leaf">`)
	for range 400 {
		b.WriteString(`<rect width="1" height="1"/>`)
	}
	b.WriteString(`</g><g id="bomb">`)
	for range 200 {
		b.WriteString(`<use href="#leaf"/>`)
	}
	b.WriteString(`</g></defs></svg>`)

	d := parse(t, b.String())
	node, ok := d.Node("bomb")
	if !ok {
		t.Fatal("Node(bomb) not found")
	}
	start := time.Now()
	if _, has := node.LocalBounds(); has {
		t.Errorf("LocalBounds on over-budget <use> group = true, want false")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("LocalBounds took %v; want fast bounded failure", elapsed)
	}
}
