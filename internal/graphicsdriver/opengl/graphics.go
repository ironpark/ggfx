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
	"fmt"
	"slices"
	"unsafe"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl/gl"
	ggshader "github.com/ironpark/ggfx/internal/shader"
)

type activatedTexture struct {
	textureNative textureNative
	index         int
}

// Presenter is what the rendered frame is presented through: a window on a
// desktop, or the context of a system that has no window system.
type Presenter interface {
	MakeContextCurrent() error
	SwapInterval(interval int) error
	SwapBuffers() error
}

type Graphics struct {
	state      openGLState
	context    context
	colorSpace color.ColorSpace

	// drawCalled is true just after Draw is called. This holds true until WritePixels is called.
	drawCalled bool

	// surfaces are the presentation targets, in creation order.
	surfaces []*Surface

	// drawingContext is the context everything is drawn in. It is nil in a browser, where the one
	// context is always current.
	drawingContext interface{ MakeContextCurrent() error }

	// activatedTextures is a set of activated textures.
	// textureNative cannot be a map key unfortunately.
	activatedTextures []activatedTexture

	// a8Supported reports whether images can be PixelFormatA8. It is decided at Initialize.
	a8Supported bool

	graphicsPlatform
}

func newGraphics(ctx gl.Context, colorSpace color.ColorSpace) *Graphics {
	g := &Graphics{
		colorSpace: colorSpace,
	}
	if isDebug {
		g.context.ctx = &gl.DebugContext{Context: ctx}
	} else {
		g.context.ctx = ctx
	}
	return g
}

func (g *Graphics) ColorSpace() color.ColorSpace {
	return g.colorSpace
}

func (g *Graphics) Begin() error {
	// Do nothing.
	return nil
}

func (g *Graphics) End(mode graphicsdriver.FlushMode) error {
	// Call glFlush to prevent black flicking (especially on Android (#226) and iOS).
	// TODO: examples/sprites worked without this. Is this really needed?
	g.context.ctx.Flush()

	// The last uniforms must be reset before swapping the buffer (#2517).
	if mode == graphicsdriver.FlushModePresent {
		g.state.resetLastUniforms()
		if err := g.present(); err != nil {
			return err
		}
	}

	return nil
}

// present swaps the buffers of every surface drawn to since the previous present, and makes the
// drawing context current again.
func (g *Graphics) present() error {
	var presented bool
	for _, s := range g.surfaces {
		if !s.drawn {
			continue
		}
		s.drawn = false
		if s.presenter == nil {
			continue
		}
		if err := s.presenter.MakeContextCurrent(); err != nil {
			return err
		}
		s.copyScreen()

		// Only the first swap of a frame waits for the display. Waiting on every window would
		// wait one refresh per window.
		//
		// Call SwapInterval even though vsync is not changed.
		// When toggling to fullscreen, vsync state might be reset unexpectedly (#1787).
		//
		// SwapInterval is affected by the current monitor of the window.
		// This needs to be called at least after SetMonitor.
		// Without SwapInterval after SetMonitor, vsynch doesn't work (#375).
		interval := 1
		if presented {
			interval = 0
		}
		if err := s.presenter.SwapInterval(interval); err != nil {
			return err
		}
		if err := s.presenter.SwapBuffers(); err != nil {
			return err
		}
		presented = true
	}
	if presented {
		return g.makeContextCurrent()
	}
	return nil
}

// SetDrawingContext sets the context everything is drawn in. It must share objects with every
// surface's context, and is never presented. SetDrawingContext must be called before Initialize.
func (g *Graphics) SetDrawingContext(context interface{ MakeContextCurrent() error }) {
	g.drawingContext = context
}

func (g *Graphics) makeContextCurrent() error {
	if g.drawingContext == nil {
		return nil
	}
	return g.drawingContext.MakeContextCurrent()
}

func (g *Graphics) checkSize(width, height int) {
	graphicsdriver.CheckSize("opengl", width, height, g.context.getMaxTextureSize())
}

// SupportsPixelFormat reports PixelFormatA8 where a texture can have a swizzle: OpenGL 3.3+ and
// OpenGL ES 3.0+, but not WebGL. An A8 image is a GL_R8 texture sampled as RRRR.
func (g *Graphics) SupportsPixelFormat(format graphicsdriver.PixelFormat) bool {
	switch format {
	case graphicsdriver.PixelFormatRGBA8:
		return true
	case graphicsdriver.PixelFormatA8:
		return g.a8Supported
	}
	return false
}

func (g *Graphics) NewImage(width, height int, format graphicsdriver.PixelFormat) (graphicsdriver.Image, error) {
	if !g.SupportsPixelFormat(format) {
		return nil, fmt.Errorf("opengl: unsupported pixel format %s", format)
	}
	i := &Image{
		graphics: g,
		width:    width,
		height:   height,
		format:   format,
	}
	w := graphics.InternalImageSize(width)
	h := graphics.InternalImageSize(height)
	g.checkSize(w, h)
	t, err := g.context.newTexture(w, h, format)
	if err != nil {
		return nil, err
	}
	i.texture = t
	return i, nil
}

// Surface is a window's GL context and its default framebuffer, or the canvas in a browser.
//
// Vertex arrays, framebuffers and the state cached in Graphics belong to one context, so everything is
// drawn in the drawing context, which shares textures with the windows' contexts. A window's screen
// image is a texture there, copied to the window's default framebuffer when presenting.
//
// Without a Presenter, as in a browser, there is one context: the screen image is its default
// framebuffer, and the surface is the only one.
type Surface struct {
	graphics  *Graphics
	presenter Presenter

	// screen is the current screen image.
	screen *Image

	// drawn reports whether the screen image was drawn to since the previous present.
	drawn bool

	// readFramebuffer is the framebuffer in the surface's context that the screen image is copied
	// from. It is zero until the first copy.
	readFramebuffer framebufferNative
}

// NewSurface ignores transparent: on the desktop the GLFW framebuffer hint decides whether the
// window composites, and in the browser the canvas does. On the desktop, target is a Presenter whose
// context shares objects with the drawing context.
func (g *Graphics) NewSurface(target any, transparent bool) (graphicsdriver.Surface, error) {
	p, err := presenterFromTarget(target)
	if err != nil {
		return nil, err
	}
	if p == nil && len(g.surfaces) > 0 {
		return nil, errors.New("opengl: only one surface is supported without a Presenter")
	}
	s := &Surface{
		graphics:  g,
		presenter: p,
	}
	g.surfaces = append(g.surfaces, s)
	return s, nil
}

func (s *Surface) NewScreenImage(width, height int) (graphicsdriver.Image, error) {
	g := s.graphics
	var i *Image
	if s.presenter != nil {
		img, err := g.NewImage(width, height, graphicsdriver.PixelFormatRGBA8)
		if err != nil {
			return nil, err
		}
		i = img.(*Image)
	} else {
		g.checkSize(width, height)
		i = &Image{
			graphics: g,
			width:    width,
			height:   height,
			screen:   true,
		}
	}
	i.surface = s
	s.screen = i
	return i, nil
}

// FlipsY reports whether the screen image is the default framebuffer, which it is without a
// Presenter: clip-space Y = -1 is the window's bottom row there, not the image's top.
func (s *Surface) FlipsY() bool {
	return s.presenter == nil
}

// copyScreen copies the screen image to the default framebuffer. The surface's context must be
// current, and the drawing context must have been flushed since the screen image was drawn.
func (s *Surface) copyScreen() {
	if s.screen == nil {
		return
	}
	ctx := s.graphics.context.ctx
	if s.readFramebuffer == 0 {
		s.readFramebuffer = framebufferNative(ctx.CreateFramebuffer())
	}
	// Attaching the texture again each time is what makes the drawing context's changes to it
	// visible here.
	ctx.BindFramebuffer(gl.READ_FRAMEBUFFER, uint32(s.readFramebuffer))
	ctx.FramebufferTexture2D(gl.READ_FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, uint32(s.screen.texture), 0)
	// The texture's rows go upward from the image's top, and the default framebuffer's go upward from
	// the window's bottom: flip the rows.
	w, h := int32(s.screen.width), int32(s.screen.height)
	ctx.BlitFramebuffer(0, 0, w, h, 0, h, w, 0, gl.COLOR_BUFFER_BIT, gl.NEAREST)
}

// Dispose forgets the surface. The objects in its context go with the context, which the window
// destroys.
func (s *Surface) Dispose() {
	g := s.graphics
	g.surfaces = slices.DeleteFunc(g.surfaces, func(t *Surface) bool { return t == s })
}

func (g *Graphics) Initialize() error {
	if err := g.makeContextCurrent(); err != nil {
		return err
	}
	if err := g.state.reset(&g.context); err != nil {
		return err
	}
	g.a8Supported = g.context.supportsTextureSwizzle()
	return nil
}

// Reset resets or initializes the current OpenGL state.
func (g *Graphics) Reset() error {
	return g.state.reset(&g.context)
}

func (g *Graphics) SetVertices(vertices []float32, indices []uint32) error {
	g.state.setVertices(&g.context, vertices, indices)
	return nil
}

func (g *Graphics) DrawTriangles(dst graphicsdriver.Image, srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image, shader graphicsdriver.Shader, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32, versions graphicsdriver.UniformVersions) error {
	if shader == nil {
		return fmt.Errorf("opengl: shader is nil")
	}

	destination := dst.(*Image)
	if destination.surface != nil {
		destination.surface.drawn = true
	}

	g.drawCalled = true

	if err := destination.setViewport(); err != nil {
		return err
	}
	g.context.blend(blend)

	// The projection is already flipped for the default framebuffer (see Surface.FlipsY).
	var imgs [graphics.ShaderSrcImageCount]textureVariable
	for i, src := range srcs {
		if src == nil {
			continue
		}
		imgs[i].valid = true
		imgs[i].native = src.(*Image).texture
	}

	if err := g.useProgram(shader.(*Shader), uniforms, versions, imgs); err != nil {
		return err
	}

	for _, dstRegion := range dstRegions {
		// The scissor box is in window coordinates, whose Y is upward too, so
		// a region of the final framebuffer is flipped like the projection.
		y := dstRegion.Region.Min.Y
		if destination.screen {
			y = destination.framebuffer.viewportHeight - dstRegion.Region.Max.Y
		}
		g.context.ctx.Scissor(
			int32(dstRegion.Region.Min.X),
			int32(y),
			int32(dstRegion.Region.Dx()),
			int32(dstRegion.Region.Dy()),
		)
		g.context.ctx.DrawElements(gl.TRIANGLES, int32(dstRegion.IndexCount), gl.UNSIGNED_INT, indexOffset*int(unsafe.Sizeof(uint32(0))))
		indexOffset += dstRegion.IndexCount
	}

	return nil
}

func (g *Graphics) NeedsClearingScreen() bool {
	return true
}

func (g *Graphics) MaxImageSize() int {
	return g.context.getMaxTextureSize()
}

// FlipsY reports false: clip-space Y = -1 is a texture's first row, which is the image's top.
func (g *Graphics) FlipsY() bool {
	return false
}

func (g *Graphics) NewShader(program *ggshader.Program) (graphicsdriver.Shader, error) {
	s, err := newShader(g, program)
	if err != nil {
		return nil, err
	}
	return s, nil
}
