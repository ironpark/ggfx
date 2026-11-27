// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"image/color"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// PathStyle holds the state of the SVG style.
type PathStyle struct {
	FillOpacity, LineOpacity          float64
	LineWidth, DashOffset, MiterLimit float64
	Dash                              []float64
	UseNonZeroWinding                 bool
	nonZeroClip                       bool // clip-rule: nonzero, for a clipPath's shapes
	displayNone                       bool // element or ancestor has display:none
	fillerColor, linerColor           paint
	LineGap                           rasterx.GapFunc
	LeadLineCap                       rasterx.CapFunc // This is used if different than LineCap
	LineCap                           rasterx.CapFunc
	LineJoin                          rasterx.JoinMode
	m                                 rasterx.Matrix2D // current transform
	// clip is the clip or mask the shapes are drawn through, inherited by
	// a group's; nil for none. See clip.go.
	clip *clipUse
	// markers are the ids of the markers at a shape's start, its inner
	// vertices and its end; see marker.go.
	markers [3]string
	// color is the inherited CSS color used to resolve currentColor on this element.
	// nil means inherit from the draw options root CurrentColor (default black).
	color color.Color
}

// paint is a fill or a stroke as a style names it: a color, a gradient,
// currentColor, or, with none set, none.
type paint struct {
	color   color.Color
	grad    *rasterx.Gradient
	gradID  string
	current bool
}

func (p paint) none() bool { return !p.current && p.color == nil && p.grad == nil }

// currentColorSentinel is placed in rasterx.GradStop.StopColor for stops with
// stop-color="currentColor". Its RGBA method falls back to opaque black.
type currentColorSentinel struct{}

func (currentColorSentinel) RGBA() (r, g, b, a uint32) { return 0, 0, 0, 0xffff }

var currentColorMark color.Color = currentColorSentinel{}

// defaultStyle sets the default PathStyle to fill black, winding rule,
// full opacity, no stroke, ButtCap line end and Bevel line connect.
var defaultStyle = PathStyle{
	FillOpacity: 1.0, LineOpacity: 1.0, LineWidth: 2.0, MiterLimit: 4.0,
	UseNonZeroWinding: true, nonZeroClip: true, fillerColor: paint{color: color.NRGBA{0x00, 0x00, 0x00, 0xff}},
	LineCap: rasterx.ButtCap, LineJoin: rasterx.Bevel, m: rasterx.Identity,
}
