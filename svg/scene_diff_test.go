// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"bytes"
	"image"
	"testing"

	"github.com/ironpark/ggfx"
)

// Hard Gate: every golden document x view and every binary doc rendered through
// the scene graph in static mode must be 100% pixel-identical to legacy Draw.
func TestSceneDifferentialMatchesLegacyGoldens(t *testing.T) {
	for _, doc := range goldenDocs {
		t.Run(doc.name, func(t *testing.T) {
			d := parse(t, doc.src)
			for _, v := range goldenViews {
				t.Run(v.name, func(t *testing.T) {
					g := v.transform()
					want := drawInto(d, v.bounds, g)
					got := image.NewRGBA(v.bounds)
					d.ForceSceneDraw(got, g)
					if !bytes.Equal(want.Pix, got.Pix) {
						// Report first differing pixel for fast diagnosis.
						for y := v.bounds.Min.Y; y < v.bounds.Max.Y; y++ {
							for x := v.bounds.Min.X; x < v.bounds.Max.X; x++ {
								wc, gc := want.RGBAAt(x, y), got.RGBAAt(x, y)
								if wc != gc {
									t.Fatalf("pixel (%d,%d): scene=%v legacy=%v", x, y, gc, wc)
								}
							}
						}
						t.Fatal("pixels differ")
					}
				})
			}
		})
	}
}

func TestSceneDifferentialMatchesBinaryDocs(t *testing.T) {
	for i, src := range binaryDocs {
		d := parse(t, src)
		x, y, _, _ := d.ViewBox()
		var g ggfx.GeoM
		g.Translate(-x, -y)
		g.Scale(3.7, 3.7)
		bounds := image.Rect(0, 0, 96, 96)
		want := drawInto(d, bounds, g)
		got := image.NewRGBA(bounds)
		d.ForceSceneDraw(got, g)
		if !bytes.Equal(want.Pix, got.Pix) {
			t.Errorf("binaryDoc %d: scene draw differs from legacy Draw", i)
		}
	}
}
