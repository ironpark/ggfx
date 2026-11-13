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
	fillerColor, linerColor           paint
	LineGap                           rasterx.GapFunc
	LeadLineCap                       rasterx.CapFunc // This is used if different than LineCap
	LineCap                           rasterx.CapFunc
	LineJoin                          rasterx.JoinMode
	mAdder                            rasterx.MatrixAdder // current transform
	// clip is the clip or mask the shapes are drawn through, inherited by
	// a group's; nil for none. See clip.go.
	clip *clipUse
	// markers are the ids of the markers at a shape's start, its inner
	// vertices and its end; see marker.go.
	markers [3]string
}

// paint is a fill or a stroke as a style names it: a color, a gradient,
// or, with neither, none.
type paint struct {
	color color.Color
	grad  *rasterx.Gradient
}

func (p paint) none() bool { return p.color == nil && p.grad == nil }

// styleAttribute describes draw options, such as {"fill":"black"; "stroke":"white"}.
type styleAttribute = map[string]string

// defaultStyle sets the default PathStyle to fill black, winding rule,
// full opacity, no stroke, ButtCap line end and Bevel line connect.
var defaultStyle = PathStyle{
	FillOpacity: 1.0, LineOpacity: 1.0, LineWidth: 2.0, MiterLimit: 4.0,
	UseNonZeroWinding: true, fillerColor: paint{color: color.NRGBA{0x00, 0x00, 0x00, 0xff}},
	LineCap: rasterx.ButtCap, LineJoin: rasterx.Bevel, mAdder: rasterx.MatrixAdder{M: rasterx.Identity},
}
