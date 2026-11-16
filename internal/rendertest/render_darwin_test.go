// Copyright 2026 The Ebiten Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rendertest_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/render"
)

func TestDrawWithoutWindow(t *testing.T) {
	// The images are deallocated rather than left to the garbage collector, which would take
	// them from the atlas in the middle of a later test that counts its images.
	src := ggfx.NewImage(4, 4)
	defer src.Deallocate()
	src.Fill(color.RGBA{0xff, 0, 0, 0xff})
	dst := ggfx.NewImage(8, 8)
	defer dst.Deallocate()

	frame(t, func() {
		op := &ggfx.DrawImageOptions{}
		op.GeoM.Translate(2, 2)
		dst.DrawImage(src, op)
	})

	var got color.RGBA
	frame(t, func() {
		got = dst.At(3, 3).(color.RGBA)
		if c := dst.At(0, 0).(color.RGBA); c != (color.RGBA{}) {
			t.Errorf("dst.At(0, 0) = %v, want transparent", c)
		}
	})
	if want := (color.RGBA{0xff, 0, 0, 0xff}); got != want {
		t.Errorf("dst.At(3, 3) = %v, want %v", got, want)
	}
	if b := dst.Bounds(); b != image.Rect(0, 0, 8, 8) {
		t.Errorf("dst.Bounds() = %v", b)
	}
}

func TestFlushAdvancesTick(t *testing.T) {
	before := render.Tick()
	frame(t, func() {})
	if got := render.Tick(); got != before+1 {
		t.Errorf("render.Tick() = %d, want %d", got, before+1)
	}
}
