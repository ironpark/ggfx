// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"github.com/ironpark/ggfx/internal/rasterx"
	"github.com/ironpark/ggfx/internal/svganim"
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
	classes   map[string][]svganim.Decl

	scene               *scene
	usesCurrentColor    bool
	gradHasCurrentColor bool
}

// Draw the compiled SVG icon with p.
// All elements should be contained by the Bounds rectangle of the SvgIcon.
func (s *SvgIcon) Draw(p *Painter, opacity float64) {
	s.DrawTransformed(p, opacity, s.Transform)
}

// DrawTransformed draws the icon as Draw does, under t in place of its
// Transform. It changes nothing in s.
func (s *SvgIcon) DrawTransformed(p *Painter, opacity float64, t rasterx.Matrix2D) {
	_ = s.DrawTransformedErr(p, opacity, t)
}

// DrawTransformedErr draws the icon under t, returning any evaluation limit
// error (ErrNestingDepth or ErrExpansionLimit) without touching p.Scanner.Dest.
func (s *SvgIcon) DrawTransformedErr(p *Painter, opacity float64, t rasterx.Matrix2D) error {
	if s.needsSceneForStatic() {
		fr := framePool.Get().(*frame)
		err := s.scene.evaluate(fr, &EvalOptions{Animate: false})
		if err == nil {
			renderFrame(p, fr, nil, opacity, t)
		}
		fr.reset()
		framePool.Put(fr)
		return err
	}
	s.drawLegacyTransformed(p, opacity, t)
	return nil
}

func (s *SvgIcon) drawLegacyTransformed(p *Painter, opacity float64, t rasterx.Matrix2D) {
	// Each clip or mask is drawn once a drawing, for every shape under it.
	p.resetAlphaPool()
	p.Dasher.BeginDraw()
	defer p.Dasher.EndDraw()
	usedClips := false
	for i := range s.SVGPaths {
		svgp := &s.SVGPaths[i]
		if svgp.clip == nil {
			svgp.DrawTransformed(p, opacity, t)
			continue
		}
		if !usedClips {
			usedClips = true
			if p.clipMasks == nil {
				p.clipMasks = make(map[*clipUse]clipResult, 4)
			} else {
				clear(p.clipMasks)
			}
		}
		clip := svgp.clip.maskImage(p, t, p.clipMasks)
		svgp.drawThrough(p, opacity, t, clip)
	}
	if usedClips {
		clear(p.clipMasks)
		p.resetAlphaPool()
	}
}
