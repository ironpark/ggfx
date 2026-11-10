// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"image"

	"github.com/srwiley/rasterx"
)

// SvgIcon holds data from parsed SVGs.
type SvgIcon struct {
	ViewBox      struct{ X, Y, W, H float64 }
	Titles       []string // Title elements collect here
	Descriptions []string // Description elements collect here
	Grads        map[string]*rasterx.Gradient
	Defs         map[string][]definition
	SVGPaths     []SvgPath
	clips        map[string]*captured // clipPath and mask elements, by id
	markers      map[string]*captured // marker elements, by id
	Transform    rasterx.Matrix2D
	classes      map[string]styleAttribute
}

// Draw the compiled SVG icon into the GraphicContext.
// All elements should be contained by the Bounds rectangle of the SvgIcon.
func (s *SvgIcon) Draw(r *rasterx.Dasher, opacity float64) {
	// Each clip or mask is drawn once a drawing, for every shape under it.
	var masks map[*clipUse]*image.Alpha
	for _, svgp := range s.SVGPaths {
		if svgp.clip == nil {
			svgp.DrawTransformed(r, opacity, s.Transform)
			continue
		}
		mask, ok := masks[svgp.clip]
		if !ok {
			mask = svgp.clip.maskImage(r, s.Transform)
			if masks == nil {
				masks = map[*clipUse]*image.Alpha{}
			}
			masks[svgp.clip] = mask
		}
		svgp.drawThrough(r, opacity, s.Transform, mask)
	}
}

// SetTarget sets the Transform matrix to draw within the bounds of the rectangle arguments
func (s *SvgIcon) SetTarget(x, y, w, h float64) {
	scaleW := w / s.ViewBox.W
	scaleH := h / s.ViewBox.H
	s.Transform = rasterx.Identity.Translate(x-s.ViewBox.X, y-s.ViewBox.Y).Scale(scaleW, scaleH)
}
