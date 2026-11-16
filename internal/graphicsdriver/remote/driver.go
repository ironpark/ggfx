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

package remote

import (
	"errors"
	"image"
	"sync"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/shader"
)

// Transport carries batches to the viewer. Its methods are called on the render thread, one at a
// time; the batch is the driver's to reuse once they return, so a transport copies what it keeps.
type Transport interface {
	// Send ships a batch that ends a frame. present reports that it ends with a Present of the
	// screen. Send may block, as a transport that keeps the app from running ahead of the viewer
	// does.
	Send(batch []byte, present bool) error

	// ReadPixels ships the batch so far and blocks until the viewer has replayed it and read the
	// regions of image id into dsts, in the image's pixel format, rows tightly packed.
	ReadPixels(batch []byte, id uint32, regions []image.Rectangle, dsts [][]byte) error
}

// Graphics is the app's side: a graphicsdriver.Graphics that encodes each call.
type Graphics struct {
	info Info
	t    Transport
	e    enc
	next uint32

	// screen is the screen image, and drawn whether it was drawn to since the last present.
	screen *Image
	drawn  bool

	// damageM guards the damage, which the app sets on its own goroutine while it draws a frame.
	damageM sync.Mutex
	damage  []image.Rectangle
	damaged bool
}

var _ graphicsdriver.Graphics = (*Graphics)(nil)

// NewGraphics returns a driver that answers its queries as info says and ships its batches
// through t.
func NewGraphics(info Info, t Transport) *Graphics {
	g := &Graphics{info: info, t: t}
	g.reset()
	return g
}

// reset starts the next batch, its header to be written when it is shipped.
func (g *Graphics) reset() {
	g.e.b = append(g.e.b[:0], make([]byte, HeaderSize)...)
}

// batch writes the header and returns the batch.
func (g *Graphics) batch() []byte {
	var h Header
	if g.screen != nil {
		h = Header{Screen: g.screen.id, Width: g.screen.width, Height: g.screen.height}
	}
	var he enc
	he.header(h)
	copy(g.e.b, he.b)
	return g.e.b
}

func (g *Graphics) newID() uint32 {
	g.next++
	if g.next == 0 {
		g.next++
	}
	return g.next
}

func (g *Graphics) Initialize() error            { return nil }
func (g *Graphics) ColorSpace() color.ColorSpace { return color.ColorSpace(g.info.ColorSpace) }
func (g *Graphics) Begin() error                 { return nil }
func (g *Graphics) MaxImageSize() int            { return g.info.MaxImageSize }
func (g *Graphics) FlipsY() bool                 { return g.info.FlipsY }

// NeedsClearingScreen reports true: the viewer's target holds the last frame, which a frame the
// app does not draw over (the first one at a new size) must not show.
func (g *Graphics) NeedsClearingScreen() bool { return true }

func (g *Graphics) SupportsPixelFormat(format graphicsdriver.PixelFormat) bool {
	switch format {
	case graphicsdriver.PixelFormatRGBA8:
		return true
	case graphicsdriver.PixelFormatA8:
		return g.info.A8
	}
	return false
}

// End ships the batch at the end of a frame. An intermediate flush ships nothing: the viewer
// replays a frame's commands in order all the same, and one batch a frame costs least.
func (g *Graphics) End(mode graphicsdriver.FlushMode) error {
	if mode == graphicsdriver.FlushModeIntermediate {
		return nil
	}
	present := mode == graphicsdriver.FlushModePresent && g.screen != nil && g.drawn
	if !present && len(g.e.b) == HeaderSize {
		return nil
	}
	if present {
		g.damageM.Lock()
		damage, damaged := g.damage, g.damaged
		g.damage, g.damaged = g.damage[:0], false
		g.damageM.Unlock()
		g.e.u8(opPresent)
		g.e.u32(g.screen.id)
		if !damaged || damage == nil {
			g.e.u32(allDamaged)
		} else {
			g.e.u32(uint32(len(damage)))
			for _, r := range damage {
				g.e.rect(r)
			}
		}
		g.drawn = false
	}
	err := g.t.Send(g.batch(), present)
	g.reset()
	return err
}

func (g *Graphics) SetVertices(vertices []float32, indices []uint32) error {
	g.e.u8(opSetVertices)
	g.e.f32s(vertices)
	g.e.u32s(indices)
	return nil
}

func (g *Graphics) NewImage(width, height int, format graphicsdriver.PixelFormat) (graphicsdriver.Image, error) {
	i := &Image{g: g, id: g.newID(), width: width, height: height, format: format}
	g.e.u8(opNewImage)
	g.e.u32(i.id)
	g.e.u32(uint32(width))
	g.e.u32(uint32(height))
	g.e.u8(uint8(format))
	return i, nil
}

// NewSurface returns the surface of the viewer's window. target must be a SurfaceTarget.
func (g *Graphics) NewSurface(target any, transparent bool) (graphicsdriver.Surface, error) {
	if _, ok := target.(SurfaceTarget); !ok {
		return nil, errors.New("remote: a surface of the remote driver is made for a SurfaceTarget")
	}
	if transparent {
		return nil, errors.New("remote: a transparent window is not supported")
	}
	return &Surface{g: g}, nil
}

func (g *Graphics) NewShader(program *shader.Program) (graphicsdriver.Shader, error) {
	if program.Source == nil {
		return nil, errors.New("remote: the shader program has no source to send")
	}
	s := &Shader{g: g, id: g.newID()}
	g.e.u8(opNewShader)
	g.e.u32(s.id)
	g.e.u32(uint32(program.TextureCount))
	g.e.bytes(program.Source)
	return s, nil
}

func idOf(i graphicsdriver.Image) uint32 {
	if i == nil {
		return 0
	}
	return i.(*Image).id
}

func (g *Graphics) DrawTriangles(dst graphicsdriver.Image, srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image, sh graphicsdriver.Shader, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32, versions graphicsdriver.UniformVersions) error {
	d := dst.(*Image)
	if d.screen {
		g.drawn = true
	}
	g.e.u8(opDrawTriangles)
	g.e.u32(d.id)
	for _, s := range srcs {
		g.e.u32(idOf(s))
	}
	g.e.u32(sh.(*Shader).id)
	g.e.u32(uint32(len(dstRegions)))
	for _, r := range dstRegions {
		g.e.rect(r.Region)
		g.e.u32(uint32(r.IndexCount))
	}
	g.e.u32(uint32(indexOffset))
	g.e.u8(uint8(blend.BlendFactorSourceRGB))
	g.e.u8(uint8(blend.BlendFactorSourceAlpha))
	g.e.u8(uint8(blend.BlendFactorDestinationRGB))
	g.e.u8(uint8(blend.BlendFactorDestinationAlpha))
	g.e.u8(uint8(blend.BlendOperationRGB))
	g.e.u8(uint8(blend.BlendOperationAlpha))
	g.e.u64(versions.Internal)
	g.e.u64(versions.User)
	g.e.u32s(uniforms)
	return nil
}

// SurfaceTarget is the surface target of the remote driver: the viewer's window.
type SurfaceTarget struct{}

// Surface is the viewer's window. Its screen image is the viewer's target, which keeps the last
// frame, across a restart of the app too.
type Surface struct{ g *Graphics }

func (s *Surface) NewScreenImage(width, height int) (graphicsdriver.Image, error) {
	g := s.g
	i := &Image{g: g, id: g.newID(), width: width, height: height, screen: true}
	g.e.u8(opNewScreenImage)
	g.e.u32(i.id)
	g.e.u32(uint32(width))
	g.e.u32(uint32(height))
	g.screen = i
	return i, nil
}

// FlipsY reports the viewer's answer for an offscreen image: the target is one.
func (s *Surface) FlipsY() bool { return s.g.info.FlipsY }

func (s *Surface) Dispose() {}

// Kept reports true: the viewer's target keeps the last frame.
func (s *Surface) Kept() bool { return true }

// SetDamage keeps what the frame being drawn changes, for its Present.
func (s *Surface) SetDamage(rects []image.Rectangle) {
	g := s.g
	g.damageM.Lock()
	defer g.damageM.Unlock()
	g.damage = append(g.damage[:0], rects...)
	g.damaged = rects != nil
}

// Image is an image of the app's, made on the viewer's side by the replay.
type Image struct {
	g             *Graphics
	id            uint32
	width, height int
	format        graphicsdriver.PixelFormat
	screen        bool
}

func (i *Image) Dispose() {
	g := i.g
	g.e.u8(opDisposeImage)
	g.e.u32(i.id)
	if g.screen == i {
		g.screen = nil
		g.drawn = false
	}
}

func (i *Image) WritePixels(args []graphicsdriver.PixelsArgs) error {
	g := i.g
	g.e.u8(opWritePixels)
	g.e.u32(i.id)
	g.e.u32(uint32(len(args)))
	for _, a := range args {
		g.e.rect(a.Region)
		g.e.bytes(a.Pixels)
	}
	return nil
}

// ReadPixels is a round trip: it ships the batch so far and waits for the viewer's answer.
func (i *Image) ReadPixels(args []graphicsdriver.PixelsArgs) error {
	g := i.g
	regions := make([]image.Rectangle, len(args))
	dsts := make([][]byte, len(args))
	for k, a := range args {
		regions[k], dsts[k] = a.Region, a.Pixels
	}
	err := g.t.ReadPixels(g.batch(), i.id, regions, dsts)
	g.reset()
	return err
}

// Shader is a shader of the app's, compiled again on the viewer's side.
type Shader struct {
	g  *Graphics
	id uint32
}

func (s *Shader) Dispose() {
	g := s.g
	g.e.u8(opDisposeShader)
	g.e.u32(s.id)
}
