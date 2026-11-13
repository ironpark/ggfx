// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"image"
	"strings"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/goldentest"
	"github.com/ironpark/ggfx/internal/rasterx"
	"github.com/ironpark/ggfx/svg"
)

// The goldens pin what Draw draws, byte for byte, as a hash of the pixels
// of each document drawn each way; see goldentest.
var goldens = goldentest.File{
	Path:   "testdata/golden.txt",
	Header: "# case sha256[:12] of the pixels; go test ./svg -run TestGolden -golden.update",
}

func TestMain(m *testing.M) { goldens.Main(m) }

// goldenView is a way of drawing a document of a 100 by 100 viewBox.
type goldenView struct {
	name   string
	bounds image.Rectangle
	geoM   func(*viewM)
}

func (v goldenView) transform() ggfx.GeoM {
	var m viewM
	v.geoM(&m)
	var g ggfx.GeoM
	g.SetElement(0, 0, m.a1+1)
	g.SetElement(0, 1, m.b)
	g.SetElement(0, 2, m.tx)
	g.SetElement(1, 0, m.c)
	g.SetElement(1, 1, m.d1+1)
	g.SetElement(1, 2, m.ty)
	return g
}

// viewM is a GeoM, kept as GeoM keeps one and composed as its methods
// compose one, but with none of its products fused with an addition, as
// the compiler fuses GeoM's on arm64, and with rasterx's trigonometry, as
// it fuses the math package's: the goldens are then the same on every
// architecture.
type viewM struct{ a1, b, c, d1, tx, ty float64 }

func (m *viewM) Scale(x, y float64) {
	m.a1, m.b, m.tx = float64((m.a1+1)*x)-1, float64(m.b*x), float64(m.tx*x)
	m.c, m.d1, m.ty = float64(m.c*y), float64((m.d1+1)*y)-1, float64(m.ty*y)
}

func (m *viewM) Translate(x, y float64) { m.tx, m.ty = m.tx+x, m.ty+y }

func (m *viewM) Rotate(theta float64) {
	sin, cos := rasterx.Sin(theta), rasterx.Cos(theta)
	*m = viewM{
		a1: float64(cos*(m.a1+1)) - float64(sin*m.c) - 1, b: float64(cos*m.b) - float64(sin*(m.d1+1)), tx: float64(cos*m.tx) - float64(sin*m.ty),
		c: float64(sin*(m.a1+1)) + float64(cos*m.c), d1: float64(sin*m.b) + float64(cos*(m.d1+1)) - 1, ty: float64(sin*m.tx) + float64(cos*m.ty),
	}
}

func (m *viewM) Skew(skewX, skewY float64) {
	sx, sy := rasterx.Tan(skewX), rasterx.Tan(skewY)
	*m = viewM{
		a1: (m.a1 + 1) + float64(m.c*sx) - 1, b: m.b + float64((m.d1+1)*sx), tx: m.tx + float64(m.ty*sx),
		c: float64((m.a1+1)*sy) + m.c, d1: float64(m.b*sy) + (m.d1 + 1) - 1, ty: m.ty + float64(m.tx*sy),
	}
}

// The vector rasterizer accumulates in fixed point up to 512 pixels a
// side, and in floating point past that: the views straddle it.
var goldenViews = []goldenView{
	{"s24", image.Rect(0, 0, 24, 24), func(g *viewM) { g.Scale(0.24, 0.24) }},
	{"s64", image.Rect(0, 0, 64, 48), func(g *viewM) { g.Scale(0.64, 0.64); g.Translate(0.37, -3.61) }},
	{"s512", image.Rect(0, 0, 512, 512), func(g *viewM) { g.Scale(5.12, 5.12); g.Translate(0.5, 0.25) }},
	{"w513", image.Rect(0, 0, 513, 160), func(g *viewM) { g.Scale(5.13, 5.13); g.Translate(-0.3, -200.7) }},
	{"h513", image.Rect(0, 0, 160, 513), func(g *viewM) { g.Scale(5.13, 5.13); g.Translate(-200.2, 0.4) }},
	{"s700", image.Rect(0, 0, 700, 700), func(g *viewM) { g.Scale(8, 8); g.Translate(-50.25, -60.5) }},
	{"min", image.Rect(7, 11, 107, 91), func(g *viewM) { g.Scale(1.1, 1.1); g.Translate(2.5, 1.75) }},
	{"rot", image.Rect(0, 0, 300, 200), func(g *viewM) {
		g.Translate(-50, -50)
		g.Rotate(0.3)
		g.Skew(0.1, 0)
		g.Scale(2.5, 1.9)
		g.Translate(150.3, 100.6)
	}},
}

// svgDoc is a document of body in viewBox.
func svgDoc(viewBox, body string) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="` + viewBox + `">` + body + `</svg>`
}

func goldenDoc(body string) string { return svgDoc("0 0 100 100", body) }

var goldenDocs = []struct{ name, src string }{
	{"solid", goldenDoc(`
<rect x="5" y="5" width="40" height="30" fill="#c33"/>
<rect x="50" y="5" width="45" height="30" rx="8" fill="rgb(20,120,200)"/>
<circle cx="25" cy="65" r="20" fill="hsl(120, 60%, 40%)"/>
<ellipse cx="72" cy="62" rx="22" ry="12" fill="goldenrod"/>
<polygon points="55,95 75,75 95,95" fill="#123"/>
<rect x="10.3" y="88.7" width="30.4" height="0.6" fill="#000"/>`)},
	{"opacity", goldenDoc(`
<rect x="5" y="5" width="60" height="60" fill="#f00" fill-opacity="0.5"/>
<circle cx="60" cy="60" r="35" fill="#00f" opacity="0.7"/>
<g opacity="0.5"><rect x="30" y="30" width="40" height="40" fill="#0f0" stroke="#000" stroke-width="4" stroke-opacity="0.6"/></g>
<rect x="70" y="5" width="25" height="25" fill="#fa0" style="fill-opacity:0.25;stroke:#06c;stroke-width:3;opacity:0.9"/>`)},
	{"caps", goldenDoc(`
<g stroke="#246" stroke-width="7" fill="none">
<path d="M15,10 L85,14" stroke-linecap="butt"/>
<path d="M15,25 L85,29" stroke-linecap="round"/>
<path d="M15,40 L85,44" stroke-linecap="square"/>
<path d="M15,55 L85,59" stroke-linecap="cubic"/>
<path d="M15,70 L85,74" stroke-linecap="quadratic"/>
<path d="M15,85 Q50,70 85,89" stroke-linecap="round" stroke-leadlinecap="square"/>
</g>`)},
	{"joins", goldenDoc(`
<g stroke="#a42" stroke-width="5" fill="none">
<polyline points="5,15 20,5 35,15 50,5" stroke-linejoin="miter"/>
<polyline points="55,15 70,5 85,15 97,7" stroke-linejoin="miter-clip" stroke-miterlimit="1.5"/>
<polyline points="5,35 20,25 35,35 50,25" stroke-linejoin="arc-clip"/>
<polyline points="55,35 70,25 85,35 97,27" stroke-linejoin="round"/>
<polyline points="5,55 20,45 35,55 50,45" stroke-linejoin="arc"/>
<polyline points="55,55 70,45 85,55 97,47" stroke-linejoin="bevel"/>
<path d="M10,70 C30,60 30,95 50,80 S80,60 90,90" stroke-linejoin="round"/>
<polygon points="60,65 90,68 75,95" stroke-linejoin="miter" stroke-miterlimit="10"/>
<path d="M5,95 L30,94 L5,93" stroke-linejoin="miter" stroke-miterlimit="40" stroke-width="2"/>
</g>`)},
	{"dashes", goldenDoc(`
<g stroke="#262" stroke-width="3" fill="none">
<path d="M5,10 L95,12" stroke-dasharray="10 4"/>
<path d="M5,22 L95,24" stroke-dasharray="7 3 2" stroke-dashoffset="5" stroke-linecap="round"/>
<path d="M5,34 Q50,20 95,40" stroke-dasharray="4,4" stroke-dashoffset="-3" stroke-linecap="square"/>
<circle cx="30" cy="70" r="20" stroke-dasharray="12 5" stroke-linejoin="round"/>
<rect x="60" y="50" width="30" height="40" stroke-dasharray="15 5 1 5" stroke-linejoin="miter" stroke-linecap="butt"/>
<path d="M55,95 L95,95" stroke-dasharray="0 0"/>
</g>`)},
	{"linegap", goldenDoc(`
<g stroke="#538" stroke-width="8" fill="none" stroke-linejoin="arc" stroke-miterlimit="1">
<path d="M10,30 C20,5 40,5 45,30 L30,45" stroke-linegap="flat"/>
<path d="M55,30 C65,5 85,5 90,30 L75,45" stroke-linegap="round"/>
<path d="M10,80 C20,55 40,55 45,80 L30,95" stroke-linegap="cubic"/>
<path d="M55,80 C65,55 85,55 90,80 L75,95" stroke-linegap="quadratic"/>
</g>`)},
	{"linear-obb", goldenDoc(`
<defs>
<linearGradient id="pad"><stop offset="0.2" stop-color="#f00"/><stop offset="0.8" stop-color="#00f" stop-opacity="0.5"/></linearGradient>
<linearGradient id="reflect" x1="0.2" y1="0.1" x2="0.5" y2="0.6" spreadMethod="reflect"><stop offset="0" stop-color="#ff0"/><stop offset="0.5" stop-color="#0a0"/><stop offset="1" stop-color="#003"/></linearGradient>
<linearGradient id="repeat" x2="0.3" spreadMethod="repeat" gradientTransform="rotate(30) scale(1.2,0.8)"><stop offset="0.1" stop-color="#fff"/><stop offset="0.9" stop-color="#804"/></linearGradient>
<linearGradient id="unsorted" x1="0" y1="0" x2="1" y2="1"><stop offset="0.7" stop-color="#0ff"/><stop offset="0.2" stop-color="#f0f"/><stop offset="0.5" stop-color="#ff0"/><stop offset="0.5" stop-color="#000"/></linearGradient>
</defs>
<rect x="5" y="5" width="40" height="40" fill="url(#pad)"/>
<rect x="55" y="5" width="40" height="40" fill="url(#reflect)"/>
<circle cx="25" cy="72" r="22" fill="url(#repeat)"/>
<path d="M55,55 L95,60 L80,95 Z" fill="url(#unsorted)"/>`)},
	{"linear-user", goldenDoc(`
<defs>
<linearGradient id="u" gradientUnits="userSpaceOnUse" x1="10" y1="10" x2="60" y2="30"><stop offset="0" stop-color="#e40"/><stop offset="0.5" stop-color="#4e0" stop-opacity="0.3"/><stop offset="1" stop-color="#04e"/></linearGradient>
<linearGradient id="ur" gradientUnits="userSpaceOnUse" x1="50" y1="50" x2="60" y2="55" spreadMethod="reflect" gradientTransform="translate(5,3) skewX(10)"><stop offset="0" stop-color="#000"/><stop offset="1" stop-color="#9cf"/></linearGradient>
<linearGradient id="up" gradientUnits="userSpaceOnUse" x1="50" y1="50" x2="56" y2="50" spreadMethod="repeat"><stop offset="0.25" stop-color="#fc0"/><stop offset="0.75" stop-color="#306"/></linearGradient>
</defs>
<rect x="5" y="5" width="90" height="30" fill="url(#u)"/>
<g transform="translate(10,40) rotate(15) scale(0.8)"><rect x="0" y="0" width="60" height="35" fill="url(#u)"/></g>
<rect x="45" y="45" width="50" height="25" fill="url(#ur)"/>
<ellipse cx="50" cy="85" rx="45" ry="12" fill="url(#up)" transform="skewY(-5)"/>`)},
	{"radial-obb", goldenDoc(`
<defs>
<radialGradient id="c"><stop offset="0" stop-color="#fff"/><stop offset="0.6" stop-color="#f80"/><stop offset="1" stop-color="#400"/></radialGradient>
<radialGradient id="f" fx="0.3" fy="0.25" r="0.6"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#06c"/></radialGradient>
<radialGradient id="out" cx="0.5" cy="0.5" r="0.3" fx="0.05" fy="0.1" spreadMethod="reflect"><stop offset="0" stop-color="#ff0"/><stop offset="0.5" stop-color="#0c6"/><stop offset="1" stop-color="#60c"/></radialGradient>
<radialGradient id="rep" r="0.2" spreadMethod="repeat" gradientTransform="rotate(20,0.5,0.5)"><stop offset="0" stop-color="#c00" stop-opacity="0.2"/><stop offset="1" stop-color="#00c"/></radialGradient>
</defs>
<rect x="5" y="5" width="40" height="30" fill="url(#c)"/>
<circle cx="75" cy="25" r="20" fill="url(#f)"/>
<rect x="5" y="50" width="45" height="45" fill="url(#out)"/>
<ellipse cx="75" cy="72" rx="20" ry="23" fill="url(#rep)"/>`)},
	{"radial-user", goldenDoc(`
<defs>
<radialGradient id="u" gradientUnits="userSpaceOnUse" cx="30" cy="30" r="25"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#930"/></radialGradient>
<radialGradient id="uf" gradientUnits="userSpaceOnUse" cx="70" cy="70" r="20" fx="62" fy="65" spreadMethod="reflect"><stop offset="0" stop-color="#ffe"/><stop offset="0.4" stop-color="#3a3"/><stop offset="1" stop-color="#113"/></radialGradient>
<radialGradient id="ut" gradientUnits="userSpaceOnUse" cx="0" cy="0" r="10" fx="3" fy="2" spreadMethod="repeat" gradientTransform="translate(70,25) scale(1.5,1)"><stop offset="0" stop-color="#0ff"/><stop offset="1" stop-color="#f0f"/></radialGradient>
</defs>
<rect x="5" y="5" width="50" height="50" fill="url(#u)"/>
<g transform="translate(-5,-3) rotate(-8,70,70)"><rect x="45" y="45" width="50" height="50" fill="url(#uf)"/></g>
<circle cx="72" cy="25" r="20" fill="url(#ut)"/>
<rect x="5" y="60" width="30" height="35" fill="url(#u)" transform="scale(1.2,0.9)"/>`)},
	{"gradient-stroke", goldenDoc(`
<defs>
<linearGradient id="l"><stop offset="0" stop-color="#f06"/><stop offset="1" stop-color="#60f"/></linearGradient>
<radialGradient id="r" gradientUnits="userSpaceOnUse" cx="50" cy="70" r="30"><stop offset="0" stop-color="#ff0"/><stop offset="1" stop-color="#030"/></radialGradient>
</defs>
<path d="M10,10 C40,40 60,0 90,30" stroke="url(#l)" stroke-width="8" fill="none" stroke-linecap="round"/>
<circle cx="50" cy="70" r="22" stroke="url(#r)" stroke-width="6" fill="url(#l)" fill-opacity="0.4" stroke-dasharray="9 4"/>`)},
	{"many-stops", goldenDoc(`
<defs>
<linearGradient id="m" x1="0" x2="1">
<stop offset="0.9" stop-color="#100"/><stop offset="0.1" stop-color="#200"/><stop offset="0.5" stop-color="#f00"/><stop offset="0.5" stop-color="#0f0"/>
<stop offset="0.3" stop-color="#00f"/><stop offset="0.3" stop-color="#ff0"/><stop offset="0.7" stop-color="#0ff"/><stop offset="0.2" stop-color="#f0f"/>
<stop offset="0.6" stop-color="#888"/><stop offset="0.5" stop-color="#fff"/><stop offset="0.8" stop-color="#480"/><stop offset="0.4" stop-color="#048"/>
<stop offset="1" stop-color="#840"/><stop offset="0" stop-color="#084"/><stop offset="0.5" stop-color="#408"/><stop offset="0.3" stop-color="#804"/>
</linearGradient>
<linearGradient id="s" x1="0" x2="0" y1="0" y2="1"><stop offset="0.5" stop-color="#f00"/><stop offset="0.5" stop-color="#00f"/></linearGradient>
<linearGradient id="nocolor"><stop offset="0"/><stop offset="1" stop-color="#fff"/></linearGradient>
</defs>
<rect x="5" y="5" width="90" height="40" fill="url(#m)"/>
<rect x="5" y="50" width="40" height="45" fill="url(#s)"/>
<rect x="55" y="50" width="40" height="45" fill="url(#nocolor)" stroke="#c0c" stroke-width="2"/>`)},
	{"clip", goldenDoc(`
<defs>
<clipPath id="c"><circle cx="50" cy="50" r="35"/></clipPath>
<clipPath id="ct" transform="rotate(20,50,50)"><rect x="20" y="40" width="60" height="20" rx="5"/><path d="M10,10 Q50,30 90,10 L90,20 Q50,40 10,20 Z"/></clipPath>
<linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient>
</defs>
<g clip-path="url(#c)"><rect x="0" y="0" width="100" height="100" fill="url(#g)"/><path d="M0,0 L100,100" stroke="#fff" stroke-width="6"/></g>
<rect x="0" y="0" width="100" height="100" fill="#0a0" fill-opacity="0.5" clip-path="url(#ct)"/>
<g transform="translate(60,60) scale(0.4)" clip-path="url(#c)"><circle cx="50" cy="50" r="50" fill="#fc0"/></g>`)},
	{"mask", goldenDoc(`
<defs>
<linearGradient id="fade"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#000"/></linearGradient>
<mask id="m"><rect x="0" y="0" width="100" height="100" fill="url(#fade)"/><circle cx="70" cy="30" r="15" fill="#000"/></mask>
<mask id="m2"><circle cx="50" cy="75" r="20" fill="#808080"/><circle cx="60" cy="75" r="10" fill="#fff" opacity="0.5"/></mask>
<radialGradient id="r"><stop offset="0" stop-color="#ff0"/><stop offset="1" stop-color="#f0f"/></radialGradient>
</defs>
<rect x="5" y="5" width="90" height="50" fill="#c00" mask="url(#m)"/>
<rect x="5" y="5" width="90" height="50" fill="url(#r)" stroke="#000" stroke-width="3" mask="url(#m)" opacity="0.6"/>
<g mask="url(#m2)"><rect x="20" y="50" width="60" height="50" fill="#06c"/><rect x="40" y="60" width="50" height="30" fill="#0c6"/></g>`)},
	{"use", goldenDoc(`
<defs>
<g id="tri"><path d="M0,0 L20,0 L10,15 Z" fill="#a50"/><circle cx="10" cy="5" r="3" fill="#fff"/></g>
<g id="pair"><use href="#tri"/><use xlink:href="#tri" x="25" y="5"/></g>
</defs>
<use href="#tri" x="5" y="5"/>
<use href="#pair" x="40" y="30"/>
<g transform="rotate(30,50,50)"><use href="#pair" x="10" y="60"/></g>`)},
	{"markers", goldenDoc(`
<defs>
<marker id="dot" viewBox="0 0 10 10" refX="5" refY="5" markerWidth="4" markerHeight="4"><circle cx="5" cy="5" r="5" fill="#00f"/></marker>
<marker id="arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="5" markerHeight="5" orient="auto"><path d="M0,0 L10,5 L0,10 z" fill="#f00"/></marker>
<marker id="rev" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="#080"/></marker>
<marker id="fixed" markerWidth="8" markerHeight="8" refX="4" refY="4" orient="45" markerUnits="userSpaceOnUse"><rect width="8" height="8" fill="#fa0"/></marker>
</defs>
<path d="M10,20 L50,10 L90,25" stroke="#000" stroke-width="2" fill="none" marker-start="url(#dot)" marker-mid="url(#dot)" marker-end="url(#arrow)"/>
<polyline points="10,50 40,70 70,45 90,60" stroke="#333" stroke-width="1.5" fill="none" marker-start="url(#rev)" marker-end="url(#rev)"/>
<path d="M15,85 C30,70 60,100 85,85" stroke="#555" stroke-width="1" fill="none" marker="url(#fixed)"/>
<line x1="5" y1="95" x2="95" y2="92" stroke="#000" marker-end="url(#arrow)"/>`)},
	{"curves", goldenDoc(`
<path d="M10,30 C20,5 40,5 50,30 S80,55 90,30" fill="none" stroke="#036" stroke-width="2"/>
<path d="M10,50 Q30,30 50,50 T90,50" fill="none" stroke="#630" stroke-width="2"/>
<path d="M10,70 A15,10 0 0 1 40,70 A15,10 30 1 0 70,70 a10,10 0 1 1 20,0" fill="none" stroke="#360" stroke-width="2"/>
<path d="m10,80 h20 v10 h-20 z m30,0 l10,10 l10,-10 z M70,80 c5,0 10,5 10,10 s-5,5 -10,0 q-5,-5 0,-10 t5,5" fill="#c9c" stroke="#000" stroke-width="0.5"/>
<path d="M60,5 A40,40 0 0 0 95,25 L95,5 Z" fill="#9c9"/>`)},
	{"outside", goldenDoc(`
<rect x="-30" y="-30" width="50" height="50" fill="#f80"/>
<rect x="80" y="-10" width="60" height="40" fill="#08f"/>
<circle cx="100" cy="100" r="25" fill="#8f0"/>
<rect x="-50" y="60" width="200" height="8" fill="#f08"/>
<rect x="200" y="200" width="10" height="10" fill="#000"/>
<rect x="-200" y="-200" width="10" height="10" fill="#000"/>
<path d="M-20,50 L120,40" stroke="#444" stroke-width="3"/>
<rect x="-1000" y="45" width="3000" height="2" fill="#0aa"/>
<circle cx="50" cy="-40" r="45" fill="none" stroke="#a0a" stroke-width="4"/>`)},
	{"slivers", goldenDoc(`
<polygon points="0,0 100,3 100,4 0,1" fill="#000"/>
<polygon points="0,20 100,22 100,22.4 0,20.4" fill="#f00"/>
<path d="M0,40 L100,43" stroke="#00f" stroke-width="0.3"/>
<path d="M2,99 L98,50" stroke="#0a0" stroke-width="0.2"/>
<polygon points="30,60 99,61 31,61.5" fill="#808"/>
<rect x="20.1" y="70.1" width="0.3" height="0.3" fill="#000"/>
<circle cx="70.5" cy="80.5" r="0.4" fill="#000"/>
<polygon points="50,98 100,97 100,99" fill="#550"/>
<polygon points="-10,10 110,30 110,30.5 -10,10.5" fill="#055"/>`)},
	{"transforms", goldenDoc(`
<g transform="rotate(25,50,50)"><rect x="20" y="20" width="60" height="15" fill="#47a"/></g>
<g transform="skewX(20)"><rect x="10" y="40" width="30" height="20" fill="#a74"/></g>
<g transform="skewY(-15) translate(0,20)"><rect x="55" y="40" width="30" height="20" fill="#7a4"/></g>
<g transform="matrix(0.8,0.3,-0.2,0.9,20,60)"><circle cx="20" cy="10" r="12" fill="#a47"/></g>
<g transform="scale(0.5) translate(120,150)"><rect width="40" height="40" fill="#4a7" stroke="#000" stroke-width="4"/></g>`)},
	{"classes", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><style>.a{fill:#c60;stroke:#000;stroke-width:2}.b{fill:#06c;opacity:0.5}</style></defs>
<rect class="a" x="10" y="10" width="50" height="50"/><circle class="b" cx="60" cy="60" r="30"/></svg>`},
	{"icon", iconSVG},
	{"evenodd", goldenDoc(`
<defs><style>.eo{fill-rule:evenodd}</style></defs>
<polygon points="25,5 36.76,41.18 5.98,18.82 44.02,18.82 13.24,41.18" fill="#36c" fill-rule="evenodd"/>
<polygon points="75,5 86.76,41.18 55.98,18.82 94.02,18.82 63.24,41.18" fill="#c63" fill-rule="nonzero"/>
<path d="M5,50 h30 v30 h-30 z M12,57 h16 v16 h-16 z" fill="#393" fill-rule="evenodd"/>
<path d="M40,50 h30 v30 h-30 z M47,57 v16 h16 v-16 z" fill="#939"/>
<g fill-rule="evenodd"><path d="M75,55 a10,10 0 1 0 0.01,0 z M85,62 a10,10 0 1 0 0.01,0 z" fill="#c90" fill-opacity="0.8"/></g>
<path class="eo" d="M5,85 h20.5 v10 h-20.5 z M25.5,85 h20 v10 h-20 z M15,87 h20 v6 h-20 z" fill="#069"/>
<polygon style="fill-rule:evenodd" points="72,78 77.88,96.09 62.49,84.91 81.51,84.91 66.12,96.09" fill="#6c6" stroke="#000" stroke-width="2"/>`)},
	{"cliprule", goldenDoc(`
<defs>
<clipPath id="star"><polygon points="25,5 36.76,41.18 5.98,18.82 44.02,18.82 13.24,41.18" clip-rule="evenodd"/></clipPath>
<clipPath id="ring" clip-rule="evenodd"><path d="M55,5 h40 v40 h-40 z M65,15 h20 v20 h-20 z"/></clipPath>
<clipPath id="fillrule"><path d="M5,55 h40 v40 h-40 z M15,65 h20 v20 h-20 z" fill-rule="evenodd"/></clipPath>
<mask id="m"><path d="M55,55 h40 v40 h-40 z M65,65 h20 v20 h-20 z" fill="#fff" fill-rule="evenodd"/></mask>
</defs>
<rect width="50" height="50" fill="#c33" clip-path="url(#star)"/>
<rect x="50" width="50" height="50" fill="#33c" clip-path="url(#ring)"/>
<rect y="50" width="50" height="50" fill="#3c3" clip-path="url(#fillrule)"/>
<rect x="50" y="50" width="50" height="50" fill="#cc3" mask="url(#m)"/>`)},
	{"stop-order", goldenDoc(`
<defs>
<linearGradient id="back"><stop offset="0.6" stop-color="#f00"/><stop offset="0.3" stop-color="#0f0"/><stop offset="0.9" stop-color="#00f"/></linearGradient>
<linearGradient id="range"><stop offset="-0.5" stop-color="#ff0"/><stop offset="1.5" stop-color="#0ff"/></linearGradient>
<linearGradient id="hard"><stop offset="0" stop-color="#000"/><stop offset="0.5" stop-color="#f80"/><stop offset="0.5" stop-color="#08f"/><stop offset="1" stop-color="#fff"/></linearGradient>
<linearGradient id="rep" x2="0.25" spreadMethod="repeat"><stop offset="0" stop-color="#f0f"/><stop offset="1" stop-color="#0f0"/></linearGradient>
</defs>
<rect x="5" y="5" width="90" height="20" fill="url(#back)"/>
<rect x="5" y="28" width="90" height="20" fill="url(#range)"/>
<rect x="5" y="51" width="90" height="20" fill="url(#hard)"/>
<rect x="5" y="74" width="90" height="20" fill="url(#rep)" fill-opacity="0.5"/>`)},
	{"grad-in-mask", goldenDoc(`
<mask id="m"><linearGradient id="fade"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#000"/></linearGradient><rect width="100" height="40" fill="url(#fade)"/></mask>
<rect width="100" height="40" fill="#c00" mask="url(#m)"/>
<defs><g id="later"><rect width="20" height="20" fill="#06c"/></g></defs>
<use href="#later" x="40" y="60"/>`)},
	{"grad-in-clip", goldenDoc(`
<clipPath id="never"><linearGradient id="l"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient><rect width="10" height="10"/></clipPath>
<mask id="later"><radialGradient id="r"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#080"/></radialGradient><rect width="100" height="100" fill="url(#r)"/></mask>
<rect x="5" y="5" width="90" height="25" fill="url(#l)"/>
<rect x="5" y="35" width="90" height="25" fill="url(#r)"/>
<rect x="5" y="65" width="90" height="30" fill="#c60" mask="url(#later)"/>`)},
	{"stroke-bbox", goldenDoc(`
<defs><linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="0.5" stop-color="#ff0"/><stop offset="1" stop-color="#00f"/></linearGradient>
<radialGradient id="r"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#080"/></radialGradient></defs>
<rect x="15" y="10" width="70" height="30" fill="url(#g)" stroke="url(#g)" stroke-width="16"/>
<path d="M15,60 L85,60 L50,92 Z" fill="none" stroke="url(#r)" stroke-width="12" stroke-linejoin="round"/>`)},
}

// iconSVG is a small icon in the style of an emoji glyph: a few paths, a
// gradient, and a clip.
const iconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
<defs>
<radialGradient id="face" cx="0.45" cy="0.4" r="0.6" fx="0.35" fy="0.3"><stop offset="0" stop-color="#ffe680"/><stop offset="0.7" stop-color="#ffc21a"/><stop offset="1" stop-color="#e08a00"/></radialGradient>
<linearGradient id="shine" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fff" stop-opacity="0.8"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></linearGradient>
<clipPath id="round"><circle cx="50" cy="50" r="46"/></clipPath>
</defs>
<circle cx="50" cy="50" r="46" fill="url(#face)"/>
<g clip-path="url(#round)">
<ellipse cx="50" cy="22" rx="30" ry="14" fill="url(#shine)"/>
<path d="M0,78 Q50,62 100,78 L100,100 L0,100 Z" fill="#c76b00" fill-opacity="0.35"/>
</g>
<ellipse cx="35" cy="42" rx="6" ry="9" fill="#5b3a00"/>
<ellipse cx="65" cy="42" rx="6" ry="9" fill="#5b3a00"/>
<path d="M28,62 Q50,82 72,62" fill="none" stroke="#5b3a00" stroke-width="5" stroke-linecap="round"/>
<circle cx="50" cy="50" r="46" fill="none" stroke="#a65c00" stroke-width="2"/>
</svg>`

// parse parses src, failing tb if it cannot.
func parse(tb testing.TB, src string) *svg.Document {
	tb.Helper()
	d, err := svg.Parse(strings.NewReader(src))
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

// drawInto draws d into a new image of bounds r through g.
func drawInto(d *svg.Document, r image.Rectangle, g ggfx.GeoM) *image.RGBA {
	dst := image.NewRGBA(r)
	d.Draw(dst, g)
	return dst
}

// scale is a GeoM scaling by s.
func scale(s float64) ggfx.GeoM {
	var g ggfx.GeoM
	g.Scale(s, s)
	return g
}

func TestGolden(t *testing.T) {
	for _, doc := range goldenDocs {
		t.Run(doc.name, func(t *testing.T) {
			d := parse(t, doc.src)
			for _, v := range goldenViews {
				t.Run(v.name, func(t *testing.T) {
					goldens.Check(t, doc.name+"/"+v.name, drawInto(d, v.bounds, v.transform()))
				})
			}
		})
	}
}
