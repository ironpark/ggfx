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
	vsync      bool
	colorSpace color.ColorSpace

	nextImageID graphicsdriver.ImageID
	images      map[graphicsdriver.ImageID]*Image

	nextShaderID graphicsdriver.ShaderID
	shaders      map[graphicsdriver.ShaderID]*Shader

	// drawCalled is true just after Draw is called. This holds true until WritePixels is called.
	drawCalled bool

	surface *Surface

	tmpUniforms []uint32

	// activatedTextures is a set of activated textures.
	// textureNative cannot be a map key unfortunately.
	activatedTextures []activatedTexture

	graphicsPlatform
}

func newGraphics(ctx gl.Context, colorSpace color.ColorSpace) *Graphics {
	g := &Graphics{
		vsync:      true,
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
		if err := g.swapBuffers(); err != nil {
			return err
		}
	}

	return nil
}

func (g *Graphics) SetTransparent(transparent bool) {
	// Do nothing.
}

func (g *Graphics) checkSize(width, height int) {
	if width < 1 {
		panic(fmt.Sprintf("opengl: width (%d) must be equal or more than %d", width, 1))
	}
	if height < 1 {
		panic(fmt.Sprintf("opengl: height (%d) must be equal or more than %d", height, 1))
	}
	m := g.context.getMaxTextureSize()
	if width > m {
		panic(fmt.Sprintf("opengl: width (%d) must be less than or equal to %d", width, m))
	}
	if height > m {
		panic(fmt.Sprintf("opengl: height (%d) must be less than or equal to %d", height, m))
	}
}

func (g *Graphics) genNextImageID() graphicsdriver.ImageID {
	g.nextImageID++
	return g.nextImageID
}

func (g *Graphics) genNextShaderID() graphicsdriver.ShaderID {
	g.nextShaderID++
	return g.nextShaderID
}

func (g *Graphics) NewImage(width, height int) (graphicsdriver.Image, error) {
	i := &Image{
		id:       g.genNextImageID(),
		graphics: g,
		width:    width,
		height:   height,
	}
	w := graphics.InternalImageSize(width)
	h := graphics.InternalImageSize(height)
	g.checkSize(w, h)
	t, err := g.context.newTexture(w, h)
	if err != nil {
		return nil, err
	}
	i.texture = t
	g.addImage(i)
	return i, nil
}

// Surface is the default framebuffer of the GL context. Only one surface is supported: GL state
// like vertex arrays is per context and the state cache is not.
type Surface struct {
	graphics *Graphics
}

func (g *Graphics) NewSurface(target any) (graphicsdriver.Surface, error) {
	if g.surface != nil {
		return nil, errors.New("opengl: only one surface is supported")
	}
	if err := g.initSurface(target); err != nil {
		return nil, err
	}
	g.surface = &Surface{graphics: g}
	return g.surface, nil
}

func (s *Surface) NewScreenImage(width, height int) (graphicsdriver.Image, error) {
	g := s.graphics
	g.checkSize(width, height)
	i := &Image{
		id:       g.genNextImageID(),
		graphics: g,
		width:    width,
		height:   height,
		screen:   true,
	}
	g.addImage(i)
	return i, nil
}

func (s *Surface) Dispose() {
	s.graphics.surface = nil
}

func (g *Graphics) addImage(img *Image) {
	if g.images == nil {
		g.images = map[graphicsdriver.ImageID]*Image{}
	}
	if _, ok := g.images[img.id]; ok {
		panic(fmt.Sprintf("opengl: image ID %d was already registered", img.id))
	}
	g.images[img.id] = img
}

func (g *Graphics) removeImage(img *Image) {
	delete(g.images, img.id)
}

func (g *Graphics) Initialize() error {
	if err := g.makeContextCurrent(); err != nil {
		return err
	}
	if err := g.state.reset(&g.context); err != nil {
		return err
	}
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

func (g *Graphics) DrawTriangles(dstID graphicsdriver.ImageID, srcIDs [graphics.ShaderSrcImageCount]graphicsdriver.ImageID, shaderID graphicsdriver.ShaderID, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32) error {
	if shaderID == graphicsdriver.InvalidShaderID {
		return fmt.Errorf("opengl: shader ID is invalid")
	}

	destination := g.images[dstID]

	g.drawCalled = true

	if err := destination.setViewport(); err != nil {
		return err
	}
	g.context.blend(blend)

	shader := g.shaders[shaderID]

	// In OpenGL, the NDC's Y direction is upward, so flip the Y direction for the final framebuffer.
	if destination.screen {
		g.tmpUniforms = append(g.tmpUniforms[:0], uniforms...)
		uniforms = g.tmpUniforms
		const p = graphics.ProjectionMatrixUniformDwordIndex
		// Invert the sign bits as float32 values.
		uniforms[p+1] ^= 1 << 31
		uniforms[p+5] ^= 1 << 31
		uniforms[p+9] ^= 1 << 31
		uniforms[p+13] ^= 1 << 31
	}

	var imgs [graphics.ShaderSrcImageCount]textureVariable
	for i, srcID := range srcIDs {
		if srcID == graphicsdriver.InvalidImageID {
			continue
		}
		imgs[i].valid = true
		imgs[i].native = g.images[srcID].texture
	}

	if err := g.useProgram(shader, uniforms, imgs); err != nil {
		return err
	}

	for _, dstRegion := range dstRegions {
		g.context.ctx.Scissor(
			int32(dstRegion.Region.Min.X),
			int32(dstRegion.Region.Min.Y),
			int32(dstRegion.Region.Dx()),
			int32(dstRegion.Region.Dy()),
		)
		g.context.ctx.DrawElements(gl.TRIANGLES, int32(dstRegion.IndexCount), gl.UNSIGNED_INT, indexOffset*int(unsafe.Sizeof(uint32(0))))
		indexOffset += dstRegion.IndexCount
	}

	return nil
}

func (g *Graphics) SetVsyncEnabled(enabled bool) {
	g.vsync = enabled
}

func (g *Graphics) NeedsClearingScreen() bool {
	return true
}

func (g *Graphics) MaxImageSize() int {
	return g.context.getMaxTextureSize()
}

func (g *Graphics) NewShader(program *ggshader.Program) (graphicsdriver.Shader, error) {
	s, err := newShader(g.genNextShaderID(), g, program)
	if err != nil {
		return nil, err
	}
	g.addShader(s)
	return s, nil
}

func (g *Graphics) addShader(shader *Shader) {
	if g.shaders == nil {
		g.shaders = map[graphicsdriver.ShaderID]*Shader{}
	}
	if _, ok := g.shaders[shader.id]; ok {
		panic(fmt.Sprintf("opengl: shader ID %d was already registered", shader.id))
	}
	g.shaders[shader.id] = shader
}

func (g *Graphics) removeShader(shader *Shader) {
	delete(g.shaders, shader.id)
}
