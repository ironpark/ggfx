// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image"
	"image/color"

	"github.com/ironpark/ggfx/internal/rasterx"
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
func (c *iconCursor) compileClip(u *clipUse, m rasterx.Matrix2D) error {
	paths, err := c.expandCaptured(u.def, m, false)
	if err != nil {
		return err
	}
	if !u.def.mask {
		// A clip is its shapes' geometry: what they cover, whatever their
		// paint.
		for i := range paths {
			paths[i].fillerColor = paint{color: color.NRGBA{0xff, 0xff, 0xff, 0xff}}
			paths[i].linerColor = paint{}
			paths[i].FillOpacity = 1
			paths[i].clip = nil
			// A clip's shapes are filled by its rule, not their fill's.
			paths[i].UseNonZeroWinding = paths[i].nonZeroClip
		}
	}
	u.paths = paths
	return nil
}

// maskImage is u drawn under t over p's target: how much each pixel lets
// through, in an image over just the part of the target u's shapes cover,
// the rest letting nothing through, placed as the target's pixels are to
// the paint.
func (u *clipUse) maskImage(p *Painter, t rasterx.Matrix2D) *image.Alpha {
	b := p.Scanner.Dest.Bounds()
	if p.clips == nil {
		p.clips = NewPainter(0, 0, nil, image.Rectangle{})
	}
	c := p.clips
	c.Dasher.SetBounds(b.Dx(), b.Dy())
	c.Scanner.Targ = b
	draw := func(dst *image.RGBA) {
		c.Scanner.Dest, c.Scanner.Touched = dst, image.Rectangle{}
		for i := range u.paths {
			u.paths[i].DrawTransformed(c, 1, t)
		}
	}
	// Find the shapes' windows, without rasterizing them, then draw them
	// over just those.
	draw(nil)
	area := c.Scanner.Touched
	draw(c.scratch(area))
	drawn := c.Scanner.Dest
	c.Scanner.Dest = nil
	mask := image.NewAlpha(area)
	for i := 0; i < len(mask.Pix); i++ {
		px := drawn.Pix[4*i : 4*i+4 : 4*i+4]
		if u.def.mask {
			// Premultiplied, so the light is already its alpha's.
			mask.Pix[i] = uint8((2126*uint32(px[0]) + 7152*uint32(px[1]) + 722*uint32(px[2])) / 10000)
		} else {
			mask.Pix[i] = px[3]
		}
	}
	// The paint is asked its color in the target's own coordinates, from
	// 0 at its corner, not in the image's.
	mask.Rect = mask.Rect.Sub(b.Min)
	return mask
}

// through is paint let through mask.
func through(paint rasterx.Paint, mask *image.Alpha) rasterx.Paint {
	if mask == nil {
		return paint
	}
	f, c := paint.Func, paint.Color
	return rasterx.FuncPaint(func(x, y int) color.RGBA64 {
		a := uint32(mask.AlphaAt(x, y).A)
		if a == 0 {
			return color.RGBA64{}
		}
		c := c
		if f != nil {
			c = f(x, y)
		}
		return color.RGBA64{uint16(uint32(c.R) * a / 255), uint16(uint32(c.G) * a / 255), uint16(uint32(c.B) * a / 255), uint16(uint32(c.A) * a / 255)}
	})
}
