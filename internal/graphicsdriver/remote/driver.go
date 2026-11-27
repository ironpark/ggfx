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
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"slices"
	"sync"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
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
	info     Info
	t        Transport
	e        enc
	batchUse recent.Peak
	next     uint32

	// screen is the screen image, and drawn is whether it was drawn to since the last present.
	screen *Image
	drawn  bool

	// damageM guards the accumulated damage hint for the next final End. Kept and SetDamage run
	// on the app goroutine while End runs on the render thread. Multiple logical frames can
	// coalesce into a single final End: multiple SetDamage calls union their rectangles, while
	// SetDamage(nil) or any Kept screen frame without a SetDamage call widens the batch to full
	// damage (damageAll). Every final End retires the recorded damage conservatively.
	damageM     sync.Mutex
	damage      []image.Rectangle
	damaged     bool
	damageAll   bool
	pendingKept bool

	hasSurface bool
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
	const minRetainedBatch = 64 * 1024
	limit := max(g.batchUse.Max(), minRetainedBatch)
	if cap(g.e.b) > recent.ShrinkFactor*limit {
		g.e.b = make([]byte, HeaderSize, limit)
		return
	}
	g.e.b = slices.Grow(g.e.b[:0], HeaderSize)[:HeaderSize]
	clear(g.e.b)
}

// batch writes the header and returns the batch.
func (g *Graphics) batch() []byte {
	var h Header
	if g.screen != nil {
		h = Header{Screen: g.screen.id, Width: g.screen.width, Height: g.screen.height}
	}
	writeHeader(g.e.b, h)
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
	// Retire any recorded damage on every final End, even when the batch is header-only and
	// returns early, so an unpresented frame's hint cannot leak into a later presentation.
	g.damageM.Lock()
	if !present && len(g.e.b) == HeaderSize {
		g.resetDamageLocked()
		g.damageM.Unlock()
		return nil
	}
	if present {
		g.e.u8(opPresent)
		g.e.u32(g.screen.id)
		if g.damageAll || g.pendingKept || !g.damaged {
			g.e.u32(allDamaged)
		} else {
			g.e.u32(uint32(len(g.damage)))
			for _, r := range g.damage {
				g.e.rect(r)
			}
		}
		g.drawn = false
	}
	g.resetDamageLocked()
	g.damageM.Unlock()
	g.batchUse.ObserveNow(len(g.e.b))
	err := g.t.Send(g.batch(), present)
	g.reset()
	return err
}

func (g *Graphics) resetDamageLocked() {
	if cap(g.damage) > 64 {
		g.damage = nil
	} else {
		g.damage = g.damage[:0]
	}
	g.damaged = false
	g.damageAll = false
	g.pendingKept = false
}

func (g *Graphics) SetVertices(vertices []float32, indices []uint32) error {
	g.e.b = slices.Grow(g.e.b, 1+7+4*len(vertices)+7+4*len(indices))
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

// NewSurface returns the surface of the viewer's window. desc.Kind must be SurfaceKindRemote.
func (g *Graphics) NewSurface(desc graphicsdriver.SurfaceDescriptor) (graphicsdriver.Surface, error) {
	if desc.Kind != graphicsdriver.SurfaceKindRemote {
		return nil, fmt.Errorf("remote: NewSurface needs SurfaceKindRemote but got %s", desc.Kind)
	}
	if desc.Transparent {
		return nil, errors.New("remote: a transparent window is not supported")
	}
	if g.hasSurface {
		return nil, errors.New("remote: only one surface is supported")
	}
	g.hasSurface = true
	return &Surface{g: g}, nil
}

func (g *Graphics) NewShader(desc *graphicsdriver.ShaderDescriptor) (graphicsdriver.Shader, error) {
	if desc == nil || desc.WGSL == nil {
		return nil, errors.New("remote: the shader descriptor has no WGSL source to send")
	}
	s := &Shader{g: g, id: g.newID()}
	g.e.u8(opNewShader)
	g.e.u32(s.id)
	g.e.u32(uint32(desc.TextureCount))
	g.e.bytes(desc.WGSL)
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
	nRegions := len(dstRegions)
	const headerLen = 1 + 4 + 4*graphics.ShaderSrcImageCount + 4 + 4
	fixedLen := headerLen + 20*nRegions + 26
	g.e.b = slices.Grow(g.e.b, fixedLen+7+4*len(uniforms))

	start := len(g.e.b)
	b := g.e.b[start : start+fixedLen]
	b[0] = opDrawTriangles
	binary.LittleEndian.PutUint32(b[1:], d.id)
	p := 5
	for _, s := range srcs {
		binary.LittleEndian.PutUint32(b[p:], idOf(s))
		p += 4
	}
	binary.LittleEndian.PutUint32(b[p:], sh.(*Shader).id)
	binary.LittleEndian.PutUint32(b[p+4:], uint32(nRegions))
	p += 8
	for _, r := range dstRegions {
		binary.LittleEndian.PutUint32(b[p:], uint32(int32(r.Region.Min.X)))
		binary.LittleEndian.PutUint32(b[p+4:], uint32(int32(r.Region.Min.Y)))
		binary.LittleEndian.PutUint32(b[p+8:], uint32(int32(r.Region.Max.X)))
		binary.LittleEndian.PutUint32(b[p+12:], uint32(int32(r.Region.Max.Y)))
		binary.LittleEndian.PutUint32(b[p+16:], uint32(r.IndexCount))
		p += 20
	}
	binary.LittleEndian.PutUint32(b[p:], uint32(indexOffset))
	b[p+4] = uint8(blend.BlendFactorSourceRGB)
	b[p+5] = uint8(blend.BlendFactorSourceAlpha)
	b[p+6] = uint8(blend.BlendFactorDestinationRGB)
	b[p+7] = uint8(blend.BlendFactorDestinationAlpha)
	b[p+8] = uint8(blend.BlendOperationRGB)
	b[p+9] = uint8(blend.BlendOperationAlpha)
	binary.LittleEndian.PutUint64(b[p+10:], versions.Internal)
	binary.LittleEndian.PutUint64(b[p+18:], versions.User)
	g.e.b = g.e.b[:start+fixedLen]
	g.e.u32s(uniforms)
	return nil
}

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

func (s *Surface) Dispose() {
	s.g.hasSurface = false
}

// Kept reports true: the viewer's target keeps the last frame. Recording a pending Kept frame
// ensures that if a screen frame omits SetDamage (defaulting to full-screen damage), any other
// frame coalesced into the same final End cannot narrow the emitted damage below full-screen.
func (s *Surface) Kept() bool {
	g := s.g
	g.damageM.Lock()
	if g.pendingKept {
		// A previous Kept frame ended without calling SetDamage, so its changes cover the whole screen.
		g.damageAll = true
		g.damage = g.damage[:0]
		g.damaged = false
	}
	g.pendingKept = true
	g.damageM.Unlock()
	return true
}

// SetDamage records what the frame being drawn changes, for its Present. If damage from an
// earlier frame whose final End has not yet run is still recorded, SetDamage unions the
// rectangles conservatively; a nil slice marks the whole screen damaged.
func (s *Surface) SetDamage(rects []image.Rectangle) {
	g := s.g
	g.damageM.Lock()
	defer g.damageM.Unlock()
	g.pendingKept = false
	if rects == nil {
		g.damageAll = true
		g.damage = g.damage[:0]
		g.damaged = false
		return
	}
	if g.damageAll {
		return
	}
	if g.damaged {
		g.damage = append(g.damage, rects...)
	} else {
		g.damage = append(g.damage[:0], rects...)
		g.damaged = true
	}
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
	total := 1 + 4 + 4
	for _, a := range args {
		total += 16 + 7 + len(a.Pixels)
	}
	g.e.b = slices.Grow(g.e.b, total)
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
