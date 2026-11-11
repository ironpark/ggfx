// Copyright 2014 Hajime Hoshi
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

package ggfx

import (
	"fmt"
	"image"
	"image/color"
	"sync"
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/bridge"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/render"
)

// Image represents a rectangle set of pixels.
// The pixel format is alpha-premultiplied RGBA, or alpha only for an image made with
// [PixelFormatAlpha], which still reads and writes as RGBA through the image.Image interfaces.
// Image implements the standard image.Image and draw.Image interfaces.
type Image struct {
	// addr holds self to check copying.
	// See strings.Builder for similar examples.
	addr     *Image
	image    *render.Image
	original *Image
	bounds   image.Rectangle

	// tmpVertices must not be reused until render.Image.Draw* is called.
	tmpVertices []float32

	// tmpIndices must not be reused until render.Image.Draw* is called.
	tmpIndices []uint32

	// tmpUniforms must not be reused until render.Image.Draw* is called.
	tmpUniforms []uint32

	// tmpQuadSources and tmpQuadRegions are DrawImages' lists of its images and their regions.
	tmpQuadSources []*render.Image
	tmpQuadRegions []image.Rectangle

	// subImageCache is a cache for sub-images.
	// subImageCache is valid only when the image is not a sub-image.
	subImageCache map[image.Rectangle]*Image

	// subImageGCLastTick is the last tick when old sub images are removed from the cache.
	subImageGCLastTick int64

	// subImageCacheM is a mutex for subImageCache.
	// subImageCache can be accessed from the image and its sub-images at the same time,
	// so the map must be protected by a mutex.
	subImageCacheM sync.Mutex

	// atime is the last access time.
	// atime needs to be an atomic value since a sub-image atime can be accessed from its original image.
	atime atomic.Int64

	// recyclable reports whether the image was created via [Image.RecyclableSubImage]
	// and can be returned to the pool via [Image.Recycle].
	recyclable bool

	// usageCallbacks are callbacks that are invoked when the image is used.
	// usageCallbacks is valid only when the image is not a sub-image.
	usageCallbacks map[int64]usageCallback

	// usageCallbackCount is len(usageCallbacks), readable without usageCallbacksM so that a draw
	// skips invokeUsageCallbacks's work when there are no callbacks.
	usageCallbackCount atomic.Int32

	// inUsageCallbacks reports whether the image is in usageCallbacks.
	inUsageCallbacks atomic.Bool

	// usageCallbacksM is a mutex for usageCallbacks.
	usageCallbacksM sync.Mutex

	// Do not add a 'buffering' member that are resolved lazily.
	// This tends to forget resolving the buffer easily (#2362).
}

func (i *Image) copyCheck() {
	if i.addr != i {
		panic("ggfx: illegal use of non-zero Image copied by value")
	}
}

func (i *Image) updateAccessTime() {
	i.atime.Store(render.Tick())
}

func (i *Image) isDisposed() bool {
	return i.image == nil
}

func (i *Image) isSubImage() bool {
	return i.original != nil
}

// adjustPosition converts the position in the *ebiten.Image coordinate to the *render.Image coordinate.
func (i *Image) adjustPosition(x, y int) (int, int) {
	if i.isSubImage() {
		or := i.original.Bounds()
		x -= or.Min.X
		y -= or.Min.Y
		return x, y
	}

	r := i.Bounds()
	x -= r.Min.X
	y -= r.Min.Y
	return x, y
}

// positionOffsetF32 returns the offset that converts a position in the *ebiten.Image coordinate to
// the *render.Image coordinate by addition.
//
// The offset is the negated origin, so adding it to x equals x - origin exactly.
func (i *Image) positionOffsetF32() (float32, float32) {
	if i.isSubImage() {
		or := i.original.Bounds()
		return -float32(or.Min.X), -float32(or.Min.Y)
	}

	r := i.Bounds()
	return -float32(r.Min.X), -float32(r.Min.Y)
}

func (i *Image) adjustedBounds() image.Rectangle {
	b := i.Bounds()
	x, y := i.adjustPosition(b.Min.X, b.Min.Y)
	return image.Rect(x, y, x+b.Dx(), y+b.Dy())
}

// Bounds returns the bounds of the image.
//
// Bounds implements the standard image.Image's Bounds.
func (i *Image) Bounds() image.Rectangle {
	if i.isDisposed() {
		panic("ggfx: the image is already disposed")
	}
	return i.bounds
}

// CanBeSource reports whether the image can be the source of a draw, such as the image given to
// [Image.DrawImage]. Every image can but a screen whose graphics driver cannot sample it: the
// screen of a Metal surface can, and a draw from it reads what the frame drew to it before. A
// sub-image answers for its image. A disposed image cannot.
func (i *Image) CanBeSource() bool {
	return !i.isDisposed() && i.image.CanBeSource()
}

// ColorModel returns the color model of the image.
//
// ColorModel implements the standard image.Image's ColorModel.
func (i *Image) ColorModel() color.Model {
	return color.RGBAModel
}

// Deallocate clears the image and deallocates the internal state of the image.
// Even after Deallocate is called, the image is still available.
// In this case, the image's internal state is allocated again.
//
// Usually, you don't have to call Deallocate since the internal state is automatically released by GC.
// However, if you are sure that the image is no longer used but not sure how this image object is referred,
// you can call Deallocate to make sure that the internal state is deallocated.
//
// If the image is a sub-image, Deallocate does nothing.
//
// If the image is disposed, Deallocate does nothing.
func (i *Image) Deallocate() {
	i.copyCheck()

	if i.isDisposed() {
		return
	}
	if i.isSubImage() {
		return
	}
	i.invokeUsageCallbacks()
	i.image.Deallocate()
	i.usageCallbacks = nil
	i.usageCallbackCount.Store(0)
}

// MaxImageSize returns the device-dependent maximum width and height of an image in pixels.
//
// MaxImageSize returns 0 before the game starts.
func MaxImageSize() int {
	return atlas.MaxImageSize()
}

// NewImage returns an empty image.
//
// If width or height is less than 1, NewImage panics.
// If width or height is more than [MaxImageSize], NewImage panics when the image is used.
//
// NewImage should be called only when necessary.
// For example, you should avoid to call NewImage every Update or Draw call.
// Reusing the same image by Clear is much more efficient than creating a new image.
//
// NewImage panics once host.Terminate was called.
func NewImage(width, height int) *Image {
	return newImage(image.Rect(0, 0, width, height), atlas.ImageTypeRegular, PixelFormatRGBA)
}

// NewImageOptions represents options for NewImage.
type NewImageOptions struct {
	// Unmanaged represents whether the image is unmanaged or not.
	// The default (zero) value is false, that means the image is managed.
	//
	// An unmanaged image is never on an internal automatic texture atlas.
	// A regular image is a part of an internal texture atlas, and locating them is done automatically in Ebitengine.
	// Unmanaged is useful when you want finer controls over the image for performance and memory reasons.
	Unmanaged bool

	// Format is the image's pixel format. The default (zero) value is PixelFormatRGBA.
	// See [PixelFormatAlpha].
	Format PixelFormat
}

// NewImageWithOptions returns an empty image with the given bounds and the options.
//
// If width or height is less than 1, NewImageWithOptions panics.
// If width or height is more than [MaxImageSize], NewImageWithOptions panics when the image is used.
//
// The rendering origin position is (0, 0) of the given bounds.
// If DrawImage is called on a new image created by NewImageOptions,
// for example, the center of scaling and rotating is (0, 0), that might not be an upper-left position.
//
// If options is nil, the default setting is used.
//
// NewImageWithOptions should be called only when necessary.
// For example, you should avoid to call NewImageWithOptions every Update or Draw call.
// Reusing the same image by Clear is much more efficient than creating a new image.
//
// NewImageWithOptions panics once host.Terminate was called.
func NewImageWithOptions(bounds image.Rectangle, options *NewImageOptions) *Image {
	imageType := atlas.ImageTypeRegular
	format := PixelFormatRGBA
	if options != nil {
		if options.Unmanaged {
			imageType = atlas.ImageTypeUnmanaged
		}
		format = options.Format
	}
	return newImage(bounds, imageType, format)
}

// What ggfx/host and ggfx/vector need of Image that is not exported goes through the bridge: host
// makes the Image of a surface's screen, and vector runs its pending fills when an image is used.
func init() {
	bridge.SetImageHooks(&bridge.ImageHooks[*Image]{
		ScreenImage: func(cur *Image, img *render.Image) *Image {
			if cur != nil && cur.image == img {
				return cur
			}
			width, height := img.Size()
			i := &Image{image: img, bounds: image.Rect(0, 0, width, height)}
			i.addr = i
			return i
		},
		Original:            originalImage,
		AddUsageCallback:    (*Image).addUsageCallback,
		RemoveUsageCallback: (*Image).removeUsageCallback,
	})
}

func newImage(bounds image.Rectangle, imageType atlas.ImageType, format PixelFormat) *Image {
	if render.Terminated() {
		panic("ggfx: NewImage cannot be called after Terminate")
	}

	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 {
		panic(fmt.Sprintf("ggfx: width at NewImage must be positive but %d", width))
	}
	if height <= 0 {
		panic(fmt.Sprintf("ggfx: height at NewImage must be positive but %d", height))
	}
	if format != PixelFormatRGBA && format != PixelFormatAlpha {
		panic(fmt.Sprintf("ggfx: unknown pixel format %d at NewImage", int(format)))
	}

	i := &Image{}
	i.image = render.NewImageWithFormat(width, height, imageType, graphicsdriver.PixelFormat(format))
	i.bounds = bounds
	i.addr = i
	return i
}

// NewImageFromImage creates a new image with the given image (source).
//
// If source's width or height is less than 1, NewImageFromImage panics.
// If source's width or height is more than [MaxImageSize], NewImageFromImage panics when the image is used.
//
// NewImageFromImage should be called only when necessary.
// For example, you should avoid to call NewImageFromImage every Update or Draw call.
// Reusing the same image by Clear and WritePixels is much more efficient than creating a new image.
//
// NewImageFromImage panics once host.Terminate was called.
//
// The returned image's upper-left position is always (0, 0). The source's bounds are not respected.
func NewImageFromImage(source image.Image) *Image {
	return NewImageFromImageWithOptions(source, nil)
}

// NewImageFromImageOptions represents options for NewImageFromImage.
type NewImageFromImageOptions struct {
	// Unmanaged represents whether the image is unmanaged or not.
	// The default (zero) value is false, that means the image is managed.
	//
	// An unmanaged image is never on an internal automatic texture atlas.
	// A regular image is a part of an internal texture atlas, and locating them is done automatically in Ebitengine.
	// Unmanaged is useful when you want finer controls over the image for performance and memory reasons.
	Unmanaged bool

	// PreserveBounds represents whether the new image's bounds are the same as the given image.
	// The default (zero) value is false, that means the new image's upper-left position is adjusted to (0, 0).
	PreserveBounds bool

	// Format is the new image's pixel format. The default (zero) value is PixelFormatRGBA.
	//
	// With PixelFormatAlpha, the new image takes the alpha channel of the source, whatever its
	// type: an *image.Alpha is copied as it is, and an *Image of PixelFormatRGBA is drawn
	// through a shader that keeps its alpha.
	Format PixelFormat
}

// NewImageFromImageWithOptions creates a new image with the given image (source) with the given options.
//
// If source's width or height is less than 1, NewImageFromImageWithOptions panics.
// If source's width or height is more than [MaxImageSize], NewImageFromImageWithOptions panics when the image is used.
//
// If options is nil, the default setting is used.
//
// NewImageFromImageWithOptions should be called only when necessary.
// For example, you should avoid to call NewImageFromImageWithOptions every Update or Draw call.
// Reusing the same image by Clear and WritePixels is much more efficient than creating a new image.
//
// NewImageFromImageWithOptions panics once host.Terminate was called.
func NewImageFromImageWithOptions(source image.Image, options *NewImageFromImageOptions) *Image {
	if options == nil {
		options = &NewImageFromImageOptions{}
	}

	var r image.Rectangle
	if options.PreserveBounds {
		r = source.Bounds()
	} else {
		size := source.Bounds().Size()
		r = image.Rect(0, 0, size.X, size.Y)
	}
	i := NewImageWithOptions(r, &NewImageOptions{
		Unmanaged: options.Unmanaged,
		Format:    options.Format,
	})

	// If the given image is an Ebitengine image, use DrawImage instead of reading pixels from the source.
	// This works even before the game loop runs.
	if source, ok := source.(*Image); ok {
		var geoM GeoM
		if options.PreserveBounds {
			b := source.Bounds()
			geoM.Translate(float64(b.Min.X), float64(b.Min.Y))
		}
		if options.Format == PixelFormatAlpha && source.Format() != PixelFormatAlpha {
			// A draw into an alpha image keeps red: draw the source's alpha as (a, a, a, a).
			size := source.Bounds().Size()
			op := &DrawRectShaderOptions{GeoM: geoM, Blend: BlendCopy}
			op.Images[0] = source
			i.DrawRectShader(size.X, size.Y, alphaShader(), op)
			return i
		}
		i.DrawImage(source, &DrawImageOptions{GeoM: geoM})
		return i
	}

	if options.Format == PixelFormatAlpha {
		i.WritePixels(imageToAlpha(source))
		return i
	}
	i.WritePixels(imageToBytes(source, true))
	return i
}
