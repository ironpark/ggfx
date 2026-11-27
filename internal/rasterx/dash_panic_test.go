// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package rasterx

import (
	"image"
	"testing"

	"golang.org/x/image/math/fixed"
)

type panicScanner struct {
	*ScannerGV
	lastScale float64
}

func (p *panicScanner) SetCoverageScale(scale float64) {
	p.lastScale = scale
	p.ScannerGV.SetCoverageScale(scale)
}

func (p *panicScanner) Draw() {
	panic("simulated scanner panic")
}

// Item M1: a panic inside Scanner.Draw while duty-cycle coverage scaling is
// active must still restore the scanner's coverage scale to 1.0 via defer.
func TestDasherCoverageScaleRestoredOnPanic(t *testing.T) {
	dst := image.NewRGBA(image.Rect(0, 0, 16, 16))
	ps := &panicScanner{ScannerGV: NewScannerGV(16, 16, dst, dst.Bounds()), lastScale: 1}
	d := NewDasher(16, 16, ps)
	d.BeginDraw()
	d.SetStroke(2*64, 4*64, ButtCap, ButtCap, FlatGap, MiterClip, []float64{0.02, 0.06}, 0)
	d.Start(fixed.P(0, 8))
	d.Line(fixed.P(16, 8))
	d.Stop(false)

	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		d.Draw()
	}()
	d.EndDraw()

	if !panicked {
		t.Fatal("expected Draw to panic")
	}
	if ps.lastScale != 1 {
		t.Errorf("lastScale after panic = %v; want 1", ps.lastScale)
	}
	if ps.covScale != 0 || ps.covSkip {
		t.Errorf("scanner coverage state not reset: covScale=%d covSkip=%v", ps.covScale, ps.covSkip)
	}
}
