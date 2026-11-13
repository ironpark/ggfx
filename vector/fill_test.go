// Copyright 2026 The Ebitengine Authors
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

package vector_test

import (
	"image"
	"image/color"
	"runtime"
	"testing"
	"time"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/vector"
)

func TestFillPathDoesNotRetainDestination(t *testing.T) {
	collected := make(chan struct{})
	func() {
		dst := ggfx.NewImage(16, 16)
		sub := dst.SubImage(image.Rect(4, 4, 12, 12)).(*ggfx.Image)
		var path vector.Path
		path.MoveTo(0, 0)
		path.LineTo(16, 0)
		path.LineTo(16, 16)
		path.Close()
		op := &vector.DrawPathOptions{}
		op.ColorScale.ScaleWithColor(color.White)
		vector.FillPath(sub, &path, nil, op)

		// The paths filled to a sub-image are pending in the original image's slot.
		if !vector.HasPendingPaths(dst) {
			t.Errorf("vector.HasPendingPaths(dst): got false, want true")
		}
		runtime.AddCleanup(dst, func(ch chan struct{}) { close(ch) }, collected)
	}()

	// The pending paths must not keep the destination alive.
	deadline := time.Now().Add(5 * time.Second)
	for {
		runtime.GC()
		select {
		case <-collected:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the destination with pending paths was not collected")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFillPathPendingIsFlushedOnUse(t *testing.T) {
	dst := ggfx.NewImage(16, 16)
	defer dst.Deallocate()
	sub := dst.SubImage(image.Rect(0, 0, 8, 8)).(*ggfx.Image)
	var path vector.Path
	path.MoveTo(0, 0)
	path.LineTo(16, 0)
	path.LineTo(16, 16)
	path.LineTo(0, 16)
	path.Close()
	op := &vector.DrawPathOptions{}
	op.ColorScale.ScaleWithColor(color.White)
	vector.FillPath(sub, &path, nil, op)
	if !vector.HasPendingPaths(dst) {
		t.Fatal("vector.HasPendingPaths(dst): got false, want true")
	}
	// A use of the original image fills the paths pending for its sub-image, and empties the slot.
	if got, want := dst.At(4, 4), (color.RGBA{0xff, 0xff, 0xff, 0xff}); got != want {
		t.Errorf("dst.At(4, 4): got %v, want %v", got, want)
	}
	if got, want := dst.At(12, 12), (color.RGBA{}); got != want {
		t.Errorf("dst.At(12, 12): got %v, want %v", got, want)
	}
	if vector.HasPendingPaths(dst) {
		t.Error("vector.HasPendingPaths(dst) after a use: got true, want false")
	}
}
