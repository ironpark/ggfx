// Copyright 2022 The Ebiten Authors
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
	"cmp"
	"fmt"
	"image"
	"slices"
	"sync"
	"sync/atomic"
	"weak"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
)

// panicOnErrorOnReadingPixels indicates whether reading pixels panics on an error or not.
// This value is set only on testing.
var panicOnErrorOnReadingPixels bool

func SetPanicOnErrorOnReadingPixelsForTesting(value bool) {
	panicOnErrorOnReadingPixels = value
}

// lastImageID is the last ID given to an image.
var lastImageID atomic.Int64

// Image is an image of the renderer: an atlas image, the pixels of it kept on the CPU for reads and
// for writes of single pixels, and the smaller copies of it a scaled-down draw samples.
type Image struct {
	// img is the image itself, the mipmap's level 0.
	img *atlas.Image

	width     int
	height    int
	imageType atlas.ImageType
	format    graphicsdriver.PixelFormat

	// readable reports whether a screen image can be a rendering source (see CanBeSource).
	readable bool

	// id determines the order to lock mu among multiple images.
	id int64

	// mu protects the members below and img.
	//
	// A ggfx.Image and its sub-images share one Image, so operations on an image and its
	// sub-images are serialized here even when they come from multiple goroutines (#3515).
	mu sync.Mutex

	// cache is the CPU-side cache of img's pixels.
	cache pixelCache

	// levels are the mipmap levels above 0, by level. A level is the format of the image: a
	// linear-filtered draw of an A8 image into another stores the filtered alpha.
	levels map[int]mipmapLevel

	// lastBlend is the lastly-used blend for a draw to the image.
	lastBlend graphicsdriver.Blend

	// modifyCallback is a callback called when DrawTriangles or WritePixels is called.
	// modifyCallback is useful to detect whether the image is manipulated or not after a certain time.
	modifyCallback func()

	tmpVerticesForFill []float32

	// inLevelsRegistry reports whether the image is in theLevelsRegistry.
	inLevelsRegistry bool
}

// levelsRegistry holds the images whose mipmaps hold levels, so that the levels not drawn from for a
// while are dropped (see dropUnusedMipmapLevels). It holds them weakly: an image no one holds is
// not kept for its levels.
type levelsRegistry struct {
	m    sync.Mutex
	imgs map[weak.Pointer[Image]]struct{}
	// tmp is the scratch dropUnusedMipmapLevels copies imgs to.
	tmp []weak.Pointer[Image]
	// lastSweep is the frame dropUnusedMipmapLevels last looked at the images.
	lastSweep int64
}

// levelsSweepInterval is how many frames apart dropUnusedMipmapLevels looks at the images. A level
// goes recent.Window to recent.Window+levelsSweepInterval frames after its last draw.
const levelsSweepInterval = recent.Window / 4

var theLevelsRegistry levelsRegistry

// registerLevels adds i to theLevelsRegistry if its mipmap holds levels. i's mutex must be held.
func (i *Image) registerLevels() {
	if i.inLevelsRegistry || len(i.levels) == 0 {
		return
	}
	i.inLevelsRegistry = true
	r := &theLevelsRegistry
	r.m.Lock()
	defer r.m.Unlock()
	if r.imgs == nil {
		r.imgs = map[weak.Pointer[Image]]struct{}{}
	}
	r.imgs[weak.Make(i)] = struct{}{}
}

// dropUnusedMipmapLevels drops the mipmap levels not drawn from for a while, of every image that
// holds some. It is a frame sweeper, and looks at the images only every levelsSweepInterval
// frames, as no level can expire sooner. No image's mutex may be held.
func dropUnusedMipmapLevels(now int64) {
	r := &theLevelsRegistry
	r.m.Lock()
	if now-r.lastSweep < levelsSweepInterval || len(r.imgs) == 0 {
		r.m.Unlock()
		return
	}
	r.lastSweep = now
	ps := r.tmp[:0]
	for p := range r.imgs {
		ps = append(ps, p)
	}
	r.m.Unlock()

	// Keep the images that still hold levels at the front of ps, and the others after.
	kept := 0
	for k, p := range ps {
		if img := p.Value(); img != nil {
			img.mu.Lock()
			keep := img.dropUnusedLevels()
			if !keep {
				img.inLevelsRegistry = false
			}
			img.mu.Unlock()
			if keep {
				ps[kept], ps[k] = ps[k], ps[kept]
				kept++
			}
		}
	}

	r.m.Lock()
	for _, p := range ps[kept:] {
		delete(r.imgs, p)
	}
	clear(ps)
	r.tmp = ps[:0]
	r.m.Unlock()
}

// NewImage returns an RGBA8 image.
func NewImage(width, height int, imageType atlas.ImageType) *Image {
	return NewImageWithFormat(width, height, imageType, graphicsdriver.PixelFormatRGBA8)
}

// NewImageWithFormat returns an image of the given pixel format. WritePixels and ReadPixels take
// pixels in that format.
func NewImageWithFormat(width, height int, imageType atlas.ImageType, format graphicsdriver.PixelFormat) *Image {
	i := newImage(atlas.NewImageWithFormat(width, height, imageType, format), width, height, imageType)
	i.format = format
	return i
}

// Format returns the image's pixel format.
func (i *Image) Format() graphicsdriver.PixelFormat {
	return i.format
}

// NewScreenImage returns the image presented on surface.
func NewScreenImage(width, height int, surface graphicsdriver.Surface) *Image {
	i := newImage(atlas.NewScreenImage(width, height, surface), width, height, atlas.ImageTypeScreen)
	i.readable = graphicscommand.ScreenReadable(surface)
	return i
}

// CanBeSource reports whether i can be a rendering source: every image can but a screen whose
// graphics driver cannot read it.
func (i *Image) CanBeSource() bool {
	return i.imageType != atlas.ImageTypeScreen || i.readable
}

// Size returns the image's size in pixels.
func (i *Image) Size() (width, height int) {
	return i.width, i.height
}

func newImage(img *atlas.Image, width, height int, imageType atlas.ImageType) *Image {
	i := &Image{
		img:       img,
		width:     width,
		height:    height,
		imageType: imageType,
		id:        lastImageID.Add(1),
		lastBlend: graphicsdriver.BlendSourceOver,
	}
	i.cache.init(width, height, img.Format().BytesPerPixel())
	return i
}

// imagesLocker locks the mutexes of a destination image and its source images.
//
// The mutexes are locked in the order of the image IDs, so that two draws in the opposite
// directions cannot deadlock each other.
type imagesLocker struct {
	imgs  [graphics.ShaderSrcImageCount + 1]*Image
	count int
}

func (l *imagesLocker) lock(dst *Image, srcs [graphics.ShaderSrcImageCount]*Image) {
	// Most draws have one source. Order the two by id as the sort below does, without sorting.
	if onlyFirst(srcs) {
		src := srcs[0]
		switch {
		case src == nil || src == dst:
			l.imgs[0] = dst
			l.count = 1
		case dst.id < src.id:
			l.imgs[0], l.imgs[1] = dst, src
			l.count = 2
		default:
			l.imgs[0], l.imgs[1] = src, dst
			l.count = 2
		}
		for _, img := range l.imgs[:l.count] {
			img.mu.Lock()
		}
		return
	}

	l.imgs[0] = dst
	copy(l.imgs[1:], srcs[:])

	// Sorting gathers the nil sources at the end, and puts duplicates next to each other.
	// One Image is shared by an image and its sub-images, so a source can be the same as the
	// destination or another source, and locking the same mutex twice would deadlock.
	slices.SortFunc(l.imgs[:], func(a, b *Image) int {
		switch {
		case a == b:
			return 0
		case a == nil:
			return 1
		case b == nil:
			return -1
		default:
			return cmp.Compare(a.id, b.id)
		}
	})
	imgs := slices.Compact(l.imgs[:])
	if imgs[len(imgs)-1] == nil {
		imgs = imgs[:len(imgs)-1]
	}

	l.count = len(imgs)
	for _, img := range imgs {
		img.mu.Lock()
	}
}

// onlyFirst reports whether srcs has no image but the first.
func onlyFirst(srcs [graphics.ShaderSrcImageCount]*Image) bool {
	for _, src := range srcs[1:] {
		if src != nil {
			return false
		}
	}
	return true
}

func (l *imagesLocker) unlock() {
	for _, img := range slices.Backward(l.imgs[:l.count]) {
		img.mu.Unlock()
	}
}

func (i *Image) Deallocate() {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.deallocateLevels()
	i.img.Deallocate()
	i.cache.deallocate()
}

// DrawTriangles draws triangles from srcs. It changes vertices in place: copying them is the
// caller's.
func (i *Image) DrawTriangles(srcs [graphics.ShaderSrcImageCount]*Image, vertices []float32, indices []uint32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions [graphics.ShaderSrcImageCount]image.Rectangle, shader *Shader, uniforms []uint32, canSkipMipmap bool) {
	var l imagesLocker
	l.lock(i, srcs)
	defer l.unlock()

	i.drawTriangles(srcs, vertices, indices, blend, dstRegion, srcRegions, shader, uniforms, canSkipMipmap)
}

// drawTriangles must be called with the mutexes of the image and the source images locked.
func (i *Image) drawTriangles(srcs [graphics.ShaderSrcImageCount]*Image, vertices []float32, indices []uint32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions [graphics.ShaderSrcImageCount]image.Rectangle, shader *Shader, uniforms []uint32, canSkipMipmap bool) {
	if i.modifyCallback != nil {
		i.modifyCallback()
	}

	i.lastBlend = blend

	if len(indices) == 0 {
		return
	}

	// The level every source is drawn from: 0, the image itself, unless the draw shrinks the
	// first source.
	var level int
	if !canSkipMipmap && srcs[0] != nil && canUseMipmap(srcs[0].imageType) {
		level = mipmapLevelFor(vertices, indices)
	}

	var imgs [graphics.ShaderSrcImageCount]*atlas.Image
	for k, src := range srcs {
		if src == nil {
			continue
		}
		if src == i {
			panic("render: Image.DrawTriangles: source images must be different from the receiver")
		}
		// src's pending pixel writes have to be applied to the GPU, but its cache doesn't have to
		// be cleared since src is not modified here.
		src.cache.writeBackPixelsIfNeeded(src.img)
		imgs[k] = src.img
		if level == 0 {
			continue
		}
		if img := src.level(level); img != nil {
			s := float32(int(1) << level)
			for j := 0; j < len(vertices); j += graphics.VertexFloatCount {
				vertices[j+2] /= s
				vertices[j+3] /= s
			}
			imgs[k] = img
		}
	}

	i.cache.writeBackPixelsIfNeeded(i.img)
	i.img.DrawTriangles(imgs, vertices, indices, blend, dstRegion, srcRegions, shader.shader, uniforms)

	// After rendering, the pixel cache and the levels are no longer valid.
	i.cache.reset()
	i.markLevelsDirty()

	if level != 0 {
		for _, src := range srcs {
			if src != nil {
				src.registerLevels()
			}
		}
	}
}

// WritePixels replaces the pixels of region.
func (i *Image) WritePixels(pix []byte, region image.Rectangle) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.modifyCallback != nil {
		i.modifyCallback()
	}
	if l := i.cache.bpp * region.Dx() * region.Dy(); len(pix) != l {
		panic(fmt.Sprintf("render: len(pix) was %d but must be %d", len(pix), l))
	}
	i.cache.writePixels(i.img, pix, region)
	i.markLevelsDirty()
}

func (i *Image) ReadPixels(pixels []byte, region image.Rectangle) {
	// Check the error existence and avoid unnecessary calls.
	if loadError() != nil {
		return
	}

	if err := readPixels(i, pixels, region); err != nil {
		if panicOnErrorOnReadingPixels {
			panic(err)
		}
		setError(err)
	}
}

// readPixels reports whether the pixels were read.
//
// The caller must not hold the mutex: a failed read is retried in a frame, and the frame needs the
// mutex to draw the image.
func (i *Image) readPixels(pixels []byte, region image.Rectangle) (bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.cache.readPixels(i.img, driver, pixels, region)
}

// Clear clears the whole image.
func (i *Image) Clear() {
	i.Fill(0, 0, 0, 0, image.Rect(0, 0, i.width, i.height))
}

// Fill fills region with the premultiplied color (r, g, b, a). An A8 image keeps a.
func (i *Image) Fill(r, g, b, a float32, region image.Rectangle) {
	if i.format == graphicsdriver.PixelFormatA8 {
		// A draw into an A8 image stores red: draw (a, a, a, a).
		r, g, b = a, a, a
	}

	// The white image is not locked: it is never modified after the initialization, and a fill
	// skips mipmaps, so drawing from it cannot modify it.
	i.mu.Lock()
	defer i.mu.Unlock()

	srcs := [graphics.ShaderSrcImageCount]*Image{whiteImage}

	if len(i.tmpVerticesForFill) < 4*graphics.VertexFloatCount {
		i.tmpVerticesForFill = make([]float32, 4*graphics.VertexFloatCount)
	}
	// i.tmpVerticesForFill can be reused as this is sent to drawTriangles immediately.
	graphics.QuadVerticesFromSrcAndMatrix(
		i.tmpVerticesForFill,
		1, 1, float32(whiteImage.width-1), float32(whiteImage.height-1),
		float32(i.width), 0, 0, float32(i.height), 0, 0,
		r, g, b, a)
	is := graphics.QuadIndices()

	blend := graphicsdriver.BlendCopy
	// If possible, use BlendSourceOver to encourage batching (#2817).
	if a == 1 && i.lastBlend == graphicsdriver.BlendSourceOver {
		blend = graphicsdriver.BlendSourceOver
	}
	sr := image.Rect(0, 0, whiteImage.width, whiteImage.height)
	// i.lastBlend is updated in drawTriangles.
	i.drawTriangles(srcs, i.tmpVerticesForFill, is, blend, region, [graphics.ShaderSrcImageCount]image.Rectangle{sr}, NearestFilterShader, nil, true)
}
