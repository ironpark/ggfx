// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"image/color"

	"github.com/srwiley/rasterx"
)

// PathStyle holds the state of the SVG style.
type PathStyle struct {
	FillOpacity, LineOpacity          float64
	LineWidth, DashOffset, MiterLimit float64
	Dash                              []float64
	UseNonZeroWinding                 bool
	fillerColor, linerColor           any // either color.Color or rasterx.Gradient
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

// styleAttribute describes draw options, such as {"fill":"black"; "stroke":"white"}.
type styleAttribute = map[string]string

// DefaultStyle sets the default PathStyle to fill black, winding rule,
// full opacity, no stroke, ButtCap line end and Bevel line connect.
var DefaultStyle = PathStyle{
	FillOpacity: 1.0, LineOpacity: 1.0, LineWidth: 2.0, MiterLimit: 4.0,
	UseNonZeroWinding: true, fillerColor: color.NRGBA{0x00, 0x00, 0x00, 0xff},
	LineCap: rasterx.ButtCap, LineJoin: rasterx.Bevel, mAdder: rasterx.MatrixAdder{M: rasterx.Identity},
}
