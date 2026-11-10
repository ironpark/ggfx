// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image"
	"image/color"

	"github.com/srwiley/rasterx"
)

// A shape drawn through a clipPath shows only where the clip's shapes are;
// through a mask, as much as the mask's shapes are light. The clip's shapes
// are compiled where a style names it, in the naming element's user space,
// and drawn alone once a drawing, into a mask the size of the target; each
// shape under it then has its color scaled at each pixel by the mask's
// there. So a group clipped as a whole is clipped shape by shape, which is
// the same for a clip; for a mask, it differs only where the group's
// shapes overlap where the mask is partly light, as the fork already
// applies a group's opacity shape by shape.

// clipUse is a clip or mask as a style names it: its definition, and its
// shapes placed where they apply.
type clipUse struct {
	def   *captured
	paths []SvgPath
}

// compileClip places u's shapes in the user space m of the element naming
// it.
func (c *IconCursor) compileClip(u *clipUse, m rasterx.Matrix2D) error {
	paths, err := c.expandCaptured(u.def, m, false)
	if err != nil {
		return err
	}
	if !u.def.mask {
		// A clip is its shapes' geometry: what they cover, whatever their
		// paint.
		for i := range paths {
			paths[i].fillerColor = color.NRGBA{0xff, 0xff, 0xff, 0xff}
			paths[i].linerColor = nil
			paths[i].FillOpacity = 1
			paths[i].clip = nil
		}
	}
	u.paths = paths
	return nil
}

// maskImage is u drawn under t into an image the size of r's target: how
// much each pixel lets through.
func (u *clipUse) maskImage(r *rasterx.Dasher, t rasterx.Matrix2D) *image.Alpha {
	gv, ok := r.Scanner.(*rasterx.ScannerGV)
	if !ok {
		return nil
	}
	b := gv.Dest.Bounds()
	drawn := image.NewRGBA(b)
	scanner := rasterx.NewScannerGV(b.Dx(), b.Dy(), drawn, b)
	dasher := rasterx.NewDasher(b.Dx(), b.Dy(), scanner)
	for i := range u.paths {
		u.paths[i].DrawTransformed(dasher, 1, t)
	}
	mask := image.NewAlpha(b)
	for i := 0; i < len(mask.Pix); i++ {
		p := drawn.Pix[4*i : 4*i+4 : 4*i+4]
		if u.def.mask {
			// Premultiplied, so the light is already its alpha's.
			mask.Pix[i] = uint8((2126*uint32(p[0]) + 7152*uint32(p[1]) + 722*uint32(p[2])) / 10000)
		} else {
			mask.Pix[i] = p[3]
		}
	}
	return mask
}

// through is paint, a color or a rasterx.ColorFunc, let through mask.
func through(paint any, mask *image.Alpha) any {
	if mask == nil {
		return paint
	}
	at := func(c color.Color, x, y int) color.Color {
		a := uint32(mask.AlphaAt(x, y).A)
		r, g, b, ca := c.RGBA()
		return color.RGBA64{uint16(r * a / 255), uint16(g * a / 255), uint16(b * a / 255), uint16(ca * a / 255)}
	}
	switch p := paint.(type) {
	case color.Color:
		return rasterx.ColorFunc(func(x, y int) color.Color { return at(p, x, y) })
	case rasterx.ColorFunc:
		return rasterx.ColorFunc(func(x, y int) color.Color { return at(p(x, y), x, y) })
	}
	return paint
}
