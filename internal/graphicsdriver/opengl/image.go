// Copyright 2018 The Ebiten Authors
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

//go:build !playstation5

package opengl

import (
	"errors"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl/gl"
)

type Image struct {
	id          graphicsdriver.ImageID
	graphics    *Graphics
	texture     textureNative
	framebuffer *framebuffer
	width       int
	height      int

	// surface is the surface the image is presented on. It is non-nil iff the image is a screen image.
	surface *Surface

	// screen reports whether the image is the default framebuffer of the context everything is drawn
	// in, which is the screen image of a surface without a Presenter. The other screen images are
	// textures.
	screen bool
}

// framebuffer is a wrapper of OpenGL's framebuffer.
type framebuffer struct {
	native         framebufferNative
	viewportWidth  int
	viewportHeight int
}

func (i *Image) ID() graphicsdriver.ImageID {
	return i.id
}

func (i *Image) Dispose() {
	if i.framebuffer != nil {
		i.graphics.context.deleteFramebuffer(i.framebuffer.native)
	}
	if i.texture != 0 {
		i.graphics.context.deleteTexture(i.texture)
	}
	if i.surface != nil && i.surface.screen == i {
		i.surface.screen = nil
	}

	i.graphics.removeImage(i)
}

func (i *Image) setViewport() error {
	if err := i.ensureFramebuffer(); err != nil {
		return err
	}
	i.graphics.context.setViewport(i.framebuffer)
	return nil
}

func (i *Image) ReadPixels(args []graphicsdriver.PixelsArgs) error {
	if err := i.ensureFramebuffer(); err != nil {
		return err
	}
	for _, arg := range args {
		if err := i.graphics.context.framebufferPixels(arg.Pixels, i.framebuffer, arg.Region); err != nil {
			return err
		}
	}
	return nil
}

func (i *Image) viewportSize() (int, int) {
	if i.surface != nil {
		// A screen image is projected at its own size, whether it is a texture or the default
		// framebuffer, whose size can't be converted to a power of 2.
		// On browsers, i.width and i.height are used as viewport size and
		// Edge can't treat a bigger viewport than the drawing area (#71).
		return i.width, i.height
	}
	return graphics.InternalImageSize(i.width), graphics.InternalImageSize(i.height)
}

func (i *Image) ensureFramebuffer() error {
	if i.framebuffer != nil {
		return nil
	}

	w, h := i.viewportSize()
	if i.screen {
		i.framebuffer = i.graphics.context.newScreenFramebuffer(w, h)
		return nil
	}

	f, err := i.graphics.context.newFramebuffer(i.texture, w, h)
	if err != nil {
		return err
	}
	i.framebuffer = f
	return nil
}

func (i *Image) WritePixels(args []graphicsdriver.PixelsArgs) error {
	if i.surface != nil {
		return errors.New("opengl: WritePixels cannot be called on the screen")
	}
	if len(args) == 0 {
		return nil
	}

	// Some drivers process glTexSubImage2D without waiting for pending draw commands, even though
	// commands in a single context must be processed in order (#211, #593, #3487).
	// Wait for completion of the pending draw commands explicitly before updating the texture.
	// The opposite order, draw commands issued after glTexSubImage2D, does not need an explicit wait,
	// as drivers process this ordering correctly.
	if i.graphics.drawCalled {
		i.graphics.context.ctx.Finish()
	}
	i.graphics.drawCalled = false

	i.graphics.context.bindTexture(i.texture)
	for _, a := range args {
		x := int32(a.Region.Min.X)
		y := int32(a.Region.Min.Y)
		width := int32(a.Region.Dx())
		height := int32(a.Region.Dy())
		i.graphics.context.ctx.TexSubImage2D(gl.TEXTURE_2D, 0, x, y, width, height, gl.RGBA, gl.UNSIGNED_BYTE, a.Pixels)
	}

	return nil
}
