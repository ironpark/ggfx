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

package mipmap

import (
	"fmt"
	"image"
	"math"
	"sync"
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/buffered"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
)

func canUseMipmap(imageType atlas.ImageType) bool {
	switch imageType {
	case atlas.ImageTypeRegular, atlas.ImageTypeUnmanaged:
		return true
	}
	return false
}

// Mipmap is a set of buffered.Image sorted by the order of mipmap level.
// The level 0 image is a regular image and higher-level images are used for mipmap.
type Mipmap struct {
	width     int
	height    int
	imageType atlas.ImageType
	// format is the pixel format of every level. The levels of an A8 image are A8 too: a
	// linear-filtered draw of an A8 image into another stores the filtered alpha.
	format graphicsdriver.PixelFormat
	orig   *buffered.Image
	imgs   map[int]imageWithDirtyFlag
}

type imageWithDirtyFlag struct {
	img   *buffered.Image
	dirty bool
	// used is the tick the level was last drawn from.
	used int64
}

// now is the tick levels are aged by, the frames render counts (see SetTick).
var now atomic.Int64

// SetTick sets the tick levels are aged by. It must not go back.
func SetTick(tick int64) {
	now.Store(tick)
}

// maxUnusedLevelTicks is how many ticks a level may go undrawn from before DropUnusedLevels drops it.
const maxUnusedLevelTicks = recent.Window

// HasLevels reports whether the mipmap holds any level besides the image itself.
func (m *Mipmap) HasLevels() bool {
	return len(m.imgs) > 0
}

// DropUnusedLevels deallocates the levels not drawn from for maxUnusedLevelTicks ticks, and reports
// whether any level is left. A level drawn again later is made again.
func (m *Mipmap) DropUnusedLevels() bool {
	t := now.Load()
	for level, img := range m.imgs {
		if t-img.used <= maxUnusedLevelTicks {
			continue
		}
		if img.img != nil {
			img.img.Deallocate()
		}
		delete(m.imgs, level)
	}
	return len(m.imgs) > 0
}

// New returns an RGBA8 mipmap.
func New(width, height int, imageType atlas.ImageType) *Mipmap {
	return NewWithFormat(width, height, imageType, graphicsdriver.PixelFormatRGBA8)
}

// NewWithFormat returns a mipmap whose levels are of the given pixel format. WritePixels and
// ReadPixels take pixels in that format.
func NewWithFormat(width, height int, imageType atlas.ImageType, format graphicsdriver.PixelFormat) *Mipmap {
	return &Mipmap{
		width:     width,
		height:    height,
		orig:      buffered.NewImageWithFormat(width, height, imageType, format),
		imageType: imageType,
		format:    format,
	}
}

// Format returns the pixel format of the mipmap.
func (m *Mipmap) Format() graphicsdriver.PixelFormat {
	return m.format
}

// NewScreen returns a mipmap for a screen image presented on surface.
func NewScreen(width, height int, surface graphicsdriver.Surface) *Mipmap {
	return &Mipmap{
		width:     width,
		height:    height,
		orig:      buffered.NewScreenImage(width, height, surface),
		imageType: atlas.ImageTypeScreen,
	}
}

func (m *Mipmap) DumpScreenshot(graphicsDriver graphicsdriver.Graphics, name string, blackbg bool) (string, error) {
	return m.orig.DumpScreenshot(graphicsDriver, name, blackbg)
}

func (m *Mipmap) WritePixels(pix []byte, region image.Rectangle) {
	m.orig.WritePixels(pix, region)
	m.markDirty()
}

func (m *Mipmap) markDirty() {
	for i, img := range m.imgs {
		img.dirty = true
		m.imgs[i] = img
	}
}

func (m *Mipmap) ReadPixels(graphicsDriver graphicsdriver.Graphics, pixels []byte, region image.Rectangle) (ok bool, err error) {
	return m.orig.ReadPixels(graphicsDriver, pixels, region)
}

func (m *Mipmap) DrawTriangles(srcs [graphics.ShaderSrcImageCount]*Mipmap, vertices []float32, indices []uint32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions [graphics.ShaderSrcImageCount]image.Rectangle, shader *atlas.Shader, uniforms []uint32, canSkipMipmap bool) {
	if len(indices) == 0 {
		return
	}

	// Use the fast path if mipmap is not used.
	if canSkipMipmap || srcs[0] == nil || !canUseMipmap(srcs[0].imageType) {
		var imgs [graphics.ShaderSrcImageCount]*buffered.Image
		for i, src := range srcs {
			if src == nil {
				continue
			}
			imgs[i] = src.orig
		}
		m.orig.DrawTriangles(imgs, vertices, indices, blend, dstRegion, srcRegions, shader, uniforms)
		m.markDirty()
		return
	}

	level := math.MaxInt32
	for i := 0; i < len(indices); i += 3 {
		idx0 := indices[i]
		idx1 := indices[i+1]
		idx2 := indices[i+2]
		dx0 := vertices[graphics.VertexFloatCount*idx0]
		dy0 := vertices[graphics.VertexFloatCount*idx0+1]
		sx0 := vertices[graphics.VertexFloatCount*idx0+2]
		sy0 := vertices[graphics.VertexFloatCount*idx0+3]
		dx1 := vertices[graphics.VertexFloatCount*idx1]
		dy1 := vertices[graphics.VertexFloatCount*idx1+1]
		sx1 := vertices[graphics.VertexFloatCount*idx1+2]
		sy1 := vertices[graphics.VertexFloatCount*idx1+3]
		dx2 := vertices[graphics.VertexFloatCount*idx2]
		dy2 := vertices[graphics.VertexFloatCount*idx2+1]
		sx2 := vertices[graphics.VertexFloatCount*idx2+2]
		sy2 := vertices[graphics.VertexFloatCount*idx2+3]
		if l := mipmapLevelFromDistance(dx0, dy0, dx1, dy1, sx0, sy0, sx1, sy1); level > l {
			level = l
		}
		if l := mipmapLevelFromDistance(dx1, dy1, dx2, dy2, sx1, sy1, sx2, sy2); level > l {
			level = l
		}
		if l := mipmapLevelFromDistance(dx2, dy2, dx0, dy0, sx2, sy2, sx0, sy0); level > l {
			level = l
		}
	}
	if level == math.MaxInt32 {
		panic("mipmap: level must be calculated at least once but not")
	}

	var imgs [graphics.ShaderSrcImageCount]*buffered.Image
	for i, src := range srcs {
		if src == nil {
			continue
		}
		if level != 0 {
			if img := src.level(level); img != nil {
				s := float32(pow2(level))
				for i := 0; i < len(vertices); i += graphics.VertexFloatCount {
					vertices[i+2] /= s
					vertices[i+3] /= s
				}
				imgs[i] = img
				continue
			}
		}
		imgs[i] = src.orig
	}

	m.orig.DrawTriangles(imgs, vertices, indices, blend, dstRegion, srcRegions, shader, uniforms)
	m.markDirty()
}

func (m *Mipmap) setImg(level int, img *buffered.Image) {
	if m.imgs == nil {
		m.imgs = map[int]imageWithDirtyFlag{}
	}
	m.imgs[level] = imageWithDirtyFlag{
		img:   img,
		dirty: false,
		used:  now.Load(),
	}
}

func (m *Mipmap) level(level int) *buffered.Image {
	if level == 0 {
		panic("mipmap: level must be non-zero at level")
	}

	if !canUseMipmap(m.imageType) {
		panic("mipmap: mipmap images for a screen image is not implemented yet")
	}

	img, ok := m.imgs[level]
	if ok && !img.dirty {
		img.used = now.Load()
		m.imgs[level] = img
		return img.img
	}

	var srcW, srcH int
	var src *buffered.Image
	vs := make([]float32, 4*graphics.VertexFloatCount)
	switch {
	case level == 1:
		src = m.orig
		srcW = m.width
		srcH = m.height
	case level > 1:
		src = m.level(level - 1)
		if src == nil {
			m.setImg(level, nil)
			return nil
		}
		srcW = sizeForLevel(m.width, level-1)
		srcH = sizeForLevel(m.height, level-1)
	default:
		panic(fmt.Sprintf("mipmap: invalid level: %d", level))
	}

	graphics.QuadVerticesFromSrcAndMatrix(vs, 0, 0, float32(srcW), float32(srcH), 0.5, 0, 0, 0.5, 0, 0, 1, 1, 1, 1)

	is := graphics.QuadIndices()

	dstW := sizeForLevel(m.width, level)
	dstH := sizeForLevel(m.height, level)
	if dstW == 0 || dstH == 0 {
		m.setImg(level, nil)
		return nil
	}
	// buffered.NewImage panics with a too big size when actual allocation happens.
	// 4096 should be a safe size in most environments (#1399).
	// Unfortunately a precise max image size cannot be obtained here since this requires GPU access.
	if dstW > 4096 || dstH > 4096 {
		m.setImg(level, nil)
		return nil
	}

	var s *buffered.Image
	if img.img != nil {
		// As s is overwritten, this doesn't have to be cleared.
		s = img.img
	} else {
		s = buffered.NewImageWithFormat(dstW, dstH, m.imageType, m.format)
	}

	dstRegion := image.Rect(0, 0, dstW, dstH)
	srcRegion := image.Rect(0, 0, srcW, srcH)
	s.DrawTriangles([graphics.ShaderSrcImageCount]*buffered.Image{src}, vs, is, graphicsdriver.BlendCopy, dstRegion, [graphics.ShaderSrcImageCount]image.Rectangle{srcRegion}, atlas.LinearFilterShader, nil)
	m.setImg(level, s)

	return m.imgs[level].img
}

func sizeForLevel(x int, level int) int {
	for range level {
		x /= 2
		if x == 0 {
			return 0
		}
	}
	return x
}

func (m *Mipmap) Deallocate() {
	for _, img := range m.imgs {
		if img.img == nil {
			continue
		}
		img.img.Deallocate()
	}
	clear(m.imgs)
	m.orig.Deallocate()
}

// mipmapLevel returns an appropriate mipmap level for the given distance.
func mipmapLevelFromDistance(dx0, dy0, dx1, dy1, sx0, sy0, sx1, sy1 float32) int {
	const maxLevel = 6

	d := (dx1-dx0)*(dx1-dx0) + (dy1-dy0)*(dy1-dy0)
	s := (sx1-sx0)*(sx1-sx0) + (sy1-sy0)*(sy1-sy0)
	if s == 0 {
		return 0
	}
	scale := d / s

	// Scale can be infinite when the specified scale is extremely big (#1398).
	if math.IsInf(float64(scale), 0) {
		return 0
	}

	// Scale can be zero when the specified scale is extremely small (#1398).
	if scale == 0 {
		return 0
	}

	var level int
	for scale < 0.25 {
		level++
		scale *= 4
	}

	if level > 0 {
		// If the image can be scaled into 0 size, adjust the level. (#839)
		w, h := int(sx1-sx0), int(sy1-sy0)
		for level >= 0 {
			if (w > 0 && sizeForLevel(w, level) == 0) || (h > 0 && sizeForLevel(h, level) == 0) {
				level--
				continue
			}
			break
		}

		if level < 0 {
			// As the render source is too small, nothing is rendered.
			return 0
		}
	}

	level = min(level, maxLevel)

	return level
}

func pow2(power int) float32 {
	x := 1
	return float32(x << uint(power))
}

var (
	quadSourcesM sync.Mutex
	quadSources  []*buffered.Image
)

// DrawQuads draws quads, quad k of four vertices from srcs[k] within srcRegions[k], from the
// sources' own images and not their mipmaps, as one DrawTriangles a quad would with
// canSkipMipmap. See atlas.Image.DrawQuads.
func (m *Mipmap) DrawQuads(srcs []*Mipmap, vertices []float32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions []image.Rectangle, shader *atlas.Shader) {
	if len(srcs) == 0 {
		return
	}
	quadSourcesM.Lock()
	defer quadSourcesM.Unlock()
	imgs := quadSources[:0]
	for _, src := range srcs {
		imgs = append(imgs, src.orig)
	}
	m.orig.DrawQuads(imgs, vertices, blend, dstRegion, srcRegions, shader)
	clear(imgs)
	quadSources = imgs[:0]
	m.markDirty()
}
