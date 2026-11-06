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
	kept := ggfx.NewImage(1, 1)
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
	if !img.CanBeSource() {
		t.Error("an offscreen image cannot be a source")
	}
	if !img.SubImage(image.Rect(1, 1, 2, 2)).(*ggfx.Image).CanBeSource() {
		t.Error("an offscreen sub-image cannot be a source")
	}
}
