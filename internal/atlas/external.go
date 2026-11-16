// Copyright 2026 The ggfx Authors
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

package atlas

import (
	"errors"
	"image"

	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// ExternalTarget is the driver's image of a destination of DrawExternally, as f sees it.
type ExternalTarget struct {
	Image graphicsdriver.Image
	// Origin is where the destination's top-left pixel is in Image.
	Origin image.Point
	// InternalWidth and InternalHeight are the size Image draws at.
	InternalWidth, InternalHeight int
}

// DrawExternally runs f on the render thread with the driver, after every draw queued so far, and
// waits for it. f makes its own driver calls, which a remote app's replay is; it may draw to dst,
// given to it as target, behind the back of every cache of dst's, which the caller resets. dst is
// nil or an image off the atlas, which stays where it is: a page would move it under f's draws.
// It must be called inside a frame.
func DrawExternally(graphicsDriver graphicsdriver.Graphics, dst *Image, f func(g graphicsdriver.Graphics, target *ExternalTarget) error) error {
	backendsM.Lock()
	defer backendsM.Unlock()

	if !inFrame {
		return errors.New("atlas: DrawExternally must be called inside a frame")
	}
	flushDeferred()

	var b *graphicscommand.Image
	var origin image.Point
	if dst != nil {
		if dst.imageType != ImageTypeUnmanaged {
			return errors.New("atlas: DrawExternally draws to an unmanaged image alone")
		}
		// Allocated now if it is not yet, as a draw to it would be.
		dst.ensureIsolatedFromSource(nil)
		b = dst.backend.backendImage
		origin = dst.regionWithPadding().Min
	}
	return graphicscommand.RunExternal(graphicsDriver, func(g graphicsdriver.Graphics) error {
		if b == nil {
			return f(g, nil)
		}
		w, h := b.InternalSize()
		return f(g, &ExternalTarget{Image: b.DriverImage(), Origin: origin, InternalWidth: w, InternalHeight: h})
	})
}
