// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"image"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// SvgIcon holds data from parsed SVGs.
type SvgIcon struct {
	ViewBox   struct{ X, Y, W, H float64 }
	Grads     map[string]*rasterx.Gradient
	Defs      map[string][]definition
	SVGPaths  []SvgPath
	clips     map[string]*captured // clipPath and mask elements, by id
	markers   map[string]*captured // marker elements, by id
	Transform rasterx.Matrix2D
	classes   map[string]styleAttribute
}

// Draw the compiled SVG icon with p.
// All elements should be contained by the Bounds rectangle of the SvgIcon.
func (s *SvgIcon) Draw(p *Painter, opacity float64) {
	s.DrawTransformed(p, opacity, s.Transform)
}

// DrawTransformed draws the icon as Draw does, under t in place of its
// Transform. It changes nothing in s.
func (s *SvgIcon) DrawTransformed(p *Painter, opacity float64, t rasterx.Matrix2D) {
	// Each clip or mask is drawn once a drawing, for every shape under it.
	var masks map[*clipUse]*image.Alpha
	for i := range s.SVGPaths {
		svgp := &s.SVGPaths[i]
		if svgp.clip == nil {
			svgp.DrawTransformed(p, opacity, t)
			continue
		}
		mask, ok := masks[svgp.clip]
		if !ok {
			mask = svgp.clip.maskImage(p, t)
			if masks == nil {
				masks = map[*clipUse]*image.Alpha{}
			}
			masks[svgp.clip] = mask
		}
		svgp.drawThrough(p, opacity, t, mask)
	}
}
