// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg

import (
	"image"

	"github.com/ironpark/ggfx"
)

// ForceSceneDraw draws d through the scene graph in static mode (test hook for differential tests).
func (d *Document) ForceSceneDraw(dst *image.RGBA, geoM ggfx.GeoM) {
	if d == nil || d.icon == nil || dst == nil {
		return
	}
	b := dst.Bounds()
	if b.Empty() {
		return
	}
	p := painters.Get()
	p.Scanner.Dest, p.Scanner.Targ = dst, b
	p.Dasher.SetBounds(b.Dx(), b.Dy())
	d.icon.ForceSceneDraw(p, 1, geoMToMatrix(geoM))
	p.Release()
	painters.Put(p)
}
