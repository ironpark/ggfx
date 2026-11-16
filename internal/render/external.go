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

package render

import (
	"errors"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// overrideDriver is the driver Init uses instead of the platform's, set by SetDriver. The remote
// driver is one: it is not the platform's, and nothing of the platform is to be opened for it.
var overrideDriver struct {
	g   graphicsdriver.Graphics
	lib GraphicsLibrary
}

// SetDriver makes Init use g, reported as lib, instead of the platform's driver. It is called
// before Init.
func SetDriver(g graphicsdriver.Graphics, lib GraphicsLibrary) error {
	if state.Load() != stateNew {
		return errors.New("render: SetDriver is called after Init")
	}
	overrideDriver.g, overrideDriver.lib = g, lib
	return nil
}

// DrawExternally runs f on the render thread with the driver, after every draw made so far, and
// waits for it: see atlas.DrawExternally. dst, nil or an unmanaged RGBA image, is given to f as
// its target; what f draws to it replaces what this side knew of its pixels and its mipmaps. It
// must be called inside a frame.
func DrawExternally(dst *Image, f func(g graphicsdriver.Graphics, target *atlas.ExternalTarget) error) error {
	if dst == nil {
		return atlas.DrawExternally(driver, nil, f)
	}
	if dst.format != graphicsdriver.PixelFormatRGBA8 {
		return errors.New("render: DrawExternally draws to an RGBA image alone")
	}
	// Pixels written to dst but not yet to the GPU go first, as for any draw.
	dst.mu.Lock()
	dst.cache.writeBackPixelsIfNeeded(dst.img)
	dst.mu.Unlock()

	err := atlas.DrawExternally(driver, dst.img, f)

	dst.mu.Lock()
	dst.cache.reset()
	dst.markLevelsDirty()
	dst.mu.Unlock()
	return err
}

// DriverLimits returns the largest image the driver makes, and whether it makes A8 images,
// initializing it as the first frame does if no frame has run yet. It is called after Init, and on
// OpenGL after the window system set its context.
func DriverLimits() (maxImageSize int, a8 bool, err error) {
	if driver == nil {
		return 0, false, errors.New("render: DriverLimits is called before Init")
	}
	if err := atlas.Initialize(driver); err != nil {
		return 0, false, err
	}
	return atlas.MaxImageSize(), atlas.SupportsA8(), nil
}
