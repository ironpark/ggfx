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

	"github.com/ebitengine/purego/objc"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/host"
	"github.com/ironpark/ggfx/vector"
)

// viewSurface is a Metal surface on an NSView in no window, made on the main thread, or nil when
// one could not be made.
var (
	viewSurface    *host.Surface
	viewSurfaceErr error
)

func newViewSurface() {
	cls := objc.GetClass("NSView")
	if cls == 0 {
		return
	}
	view := objc.ID(cls).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("init"))
	viewSurface, viewSurfaceErr = host.NewSurface(host.NSView(view), false)
}

// TestScreenAsSource draws from a Metal screen mid-frame: the draw reads what the frame drew to
// the screen before it, and not what it draws after.
func TestScreenAsSource(t *testing.T) {
	if viewSurface == nil {
		t.Skipf("no Metal surface: %v", viewSurfaceErr)
	}
	red := color.RGBA{0xff, 0, 0, 0xff}
	green := color.RGBA{0, 0xff, 0, 0xff}
	blue := color.RGBA{0, 0, 0xff, 0xff}
	dst := ggfx.NewImage(8, 8)
	defer dst.Deallocate()
	kept := ggfx.NewImage(1, 1)
	defer kept.Deallocate()
	for n := range 3 {
		fr, err := host.BeginFrame()
		if err != nil {
			t.Fatal(err)
		}
		screen := fr.Screen(viewSurface, 16, 16)
		if !screen.CanBeSource() {
			t.Fatal("a Metal screen cannot be a source")
		}
		keptFrame := viewSurface.Kept()
		if keptFrame {
			// A kept frame starts from the last, which ended blue: read before drawing anything.
			kept.DrawImage(screen.SubImage(image.Rect(12, 12, 13, 13)).(*ggfx.Image), &ggfx.DrawImageOptions{Blend: ggfx.BlendCopy})
		}
		screen.Fill(red)
		screen.SubImage(image.Rect(4, 4, 8, 8)).(*ggfx.Image).Fill(green)
		// The screen's part (2, 2)-(10, 10), drawn at dst's origin: green at (2, 2)-(6, 6).
		op := &ggfx.DrawImageOptions{Blend: ggfx.BlendCopy}
		dst.DrawImage(screen.SubImage(image.Rect(2, 2, 10, 10)).(*ggfx.Image), op)
		screen.Fill(blue)
		if err := fr.End(); err != nil {
			t.Fatal(err)
		}
		if _, err := host.Flush(true); err != nil {
			t.Fatal(err)
		}
		if n > 0 && !keptFrame {
			t.Errorf("frame %d is not kept", n)
		}
		frame(t, func() {
			if keptFrame {
				if got := kept.At(0, 0).(color.RGBA); got != blue {
					t.Errorf("frame %d: the kept screen read %v, want %v", n, got, blue)
				}
			}
			for _, c := range []struct {
				x, y int
				want color.RGBA
			}{{0, 0, red}, {1, 1, red}, {2, 2, green}, {5, 5, green}, {6, 6, red}, {7, 7, red}} {
				if got := dst.At(c.x, c.y).(color.RGBA); got != c.want {
					t.Errorf("frame %d: dst.At(%d, %d) = %v, want %v", n, c.x, c.y, got, c.want)
				}
			}
		})
	}
}

func TestOffscreenCanBeSource(t *testing.T) {
	img := ggfx.NewImage(4, 4)
	defer img.Deallocate()
	if !img.CanBeSource() {
		t.Error("an offscreen image cannot be a source")
	}
	if !img.SubImage(image.Rect(1, 1, 2, 2)).(*ggfx.Image).CanBeSource() {
		t.Error("an offscreen sub-image cannot be a source")
	}
}

// TestScreenFlushesPendingVectorAtFrameEnd checks that pending vector fills on a screen are
// flushed when Frame.End runs, even when no other ggfx.Image call follows them.
func TestScreenFlushesPendingVectorAtFrameEnd(t *testing.T) {
	if viewSurface == nil {
		t.Skipf("no Metal surface: %v", viewSurfaceErr)
	}
	dst := ggfx.NewImage(16, 16)
	defer dst.Deallocate()
	red := color.RGBA{0xff, 0, 0, 0xff}

	fr, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	screen := fr.Screen(viewSurface, 16, 16)
	screen.Clear()
	vector.FillRect(screen, 0, 0, 16, 16, red, true)
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(true); err != nil {
		t.Fatal(err)
	}

	// On the next kept frame, the screen must contain the vector fill from the previous frame.
	fr, err = host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	screen = fr.Screen(viewSurface, 16, 16)
	kept := viewSurface.Kept()
	dst.DrawImage(screen, &ggfx.DrawImageOptions{Blend: ggfx.BlendCopy})
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(true); err != nil {
		t.Fatal(err)
	}
	if !kept {
		t.Skip("surface did not keep previous frame")
	}
	var got color.RGBA
	frame(t, func() { got = dst.At(8, 8).(color.RGBA) })
	if got != red {
		t.Errorf("screen pixel after Frame.End with pending vector fill = %v, want %v", got, red)
	}
}

// TestScreenDamageNilAndIntermediateFlush checks that:
//  1. SetDamage(nil) after a partial-damage frame updates the entire surface rather than retaining
//     the previous partial damage rect.
//  2. Presenting after a mid-frame offscreen ReadPixels (which flushes the Metal command buffer
//     early) still presents the surface.
func TestScreenDamageNilAndIntermediateFlush(t *testing.T) {
	if viewSurface == nil {
		t.Skipf("no Metal surface: %v", viewSurfaceErr)
	}
	red := color.RGBA{0xff, 0, 0, 0xff}
	green := color.RGBA{0, 0xff, 0, 0xff}
	blue := color.RGBA{0, 0, 0xff, 0xff}

	off := ggfx.NewImage(4, 4)
	defer off.Deallocate()
	off.Fill(green)

	dst := ggfx.NewImage(16, 16)
	defer dst.Deallocate()

	// Frame 1: fill red with full damage.
	fr, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	screen := fr.Screen(viewSurface, 16, 16)
	viewSurface.SetDamage(nil)
	screen.Fill(red)
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(true); err != nil {
		t.Fatal(err)
	}

	// Frame 2: partial damage (0,0)-(4,4) only.
	fr, err = host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	screen = fr.Screen(viewSurface, 16, 16)
	viewSurface.SetDamage([]image.Rectangle{image.Rect(0, 0, 4, 4)})
	screen.SubImage(image.Rect(0, 0, 4, 4)).(*ggfx.Image).Fill(green)
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(true); err != nil {
		t.Fatal(err)
	}

	// Frame 3: clear damage to nil (full damage), draw blue, then trigger an intermediate flush
	// via off.At(0, 0) before Frame.End + Flush(true).
	fr, err = host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	screen = fr.Screen(viewSurface, 16, 16)
	viewSurface.SetDamage(nil)
	screen.Fill(blue)
	if got := off.At(0, 0).(color.RGBA); got != green {
		t.Fatalf("off.At(0, 0) = %v, want %v", got, green)
	}
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(true); err != nil {
		t.Fatal(err)
	}

	// Frame 4: read back the kept screen at (12, 12) (outside Frame 2's partial damage).
	fr, err = host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	screen = fr.Screen(viewSurface, 16, 16)
	kept := viewSurface.Kept()
	dst.DrawImage(screen, &ggfx.DrawImageOptions{Blend: ggfx.BlendCopy})
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(true); err != nil {
		t.Fatal(err)
	}
	if !kept {
		t.Skip("surface did not keep previous frame")
	}
	var got color.RGBA
	frame(t, func() { got = dst.At(12, 12).(color.RGBA) })
	if got != blue {
		t.Errorf("screen(12,12) after SetDamage(nil) + intermediate flush = %v, want %v", got, blue)
	}
}
