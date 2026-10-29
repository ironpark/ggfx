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

// Package rendertest drives the renderer through its public API with no window system, as one of
// its own would. Metal renders without a surface, so this runs on macOS.
package rendertest_test

import (
	"image"
	"image/color"
	"os"
	"runtime"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/host"
)

func init() {
	// Init must be called on the main thread.
	runtime.LockOSThread()
}

func TestMain(m *testing.M) {
	if err := host.Init(&host.Options{GraphicsLibrary: ggfx.GraphicsLibraryMetal}); err != nil {
		panic(err)
	}
	code := m.Run()
	host.Terminate()
	os.Exit(code)
}

// frame runs f in a frame and flushes it, as a window system's loop does.
func frame(t *testing.T, f func()) {
	t.Helper()
	host.AdvanceTick()
	fr, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	f()
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(false); err != nil {
		t.Fatal(err)
	}
}

func TestDrawWithoutWindow(t *testing.T) {
	src := ggfx.NewImage(4, 4)
	src.Fill(color.RGBA{0xff, 0, 0, 0xff})
	dst := ggfx.NewImage(8, 8)

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

func TestTick(t *testing.T) {
	before := ggfx.Tick()
	frame(t, func() {})
	if got := ggfx.Tick(); got != before+1 {
		t.Errorf("ggfx.Tick() = %d, want %d", got, before+1)
	}
}
