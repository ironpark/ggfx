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
	"image"

	"github.com/ironpark/ggfx/internal/pool"
	"github.com/ironpark/ggfx/internal/recent"
	"github.com/ironpark/ggfx/internal/render"
)

// theImagePool is a global pool of Image structs to reduce allocations.
// [Image.RecyclableSubImage] draws from this pool; [Image.Recycle] returns to it.
var theImagePool = pool.Pool[*Image]{
	New: func() *Image { return &Image{} },
}

// SubImage returns an image representing the portion of the image p visible through r.
// The returned value shares pixels with the original image.
//
// The returned value is always *ebiten.Image.
//
// If the image is disposed, SubImage returns nil.
//
// A sub-image returned by SubImage can be used as a rendering source and a rendering destination.
// If a sub-image is used as a rendering source, the image is used as if it is a small image.
// If a sub-image is used as a rendering destination, the region being rendered is clipped.
//
// Successive uses of multiple various regions as rendering destination is still efficient
// when all the underlying images are the same, but some platforms like browsers might not work efficiently.
func (i *Image) SubImage(r image.Rectangle) image.Image {
	i.copyCheck()
	if i.isDisposed() {
		return nil
	}

	if i.isSubImage() {
		return i.original.SubImage(r.Intersect(i.Bounds()))
	}

	r = r.Intersect(i.Bounds())
	// Need to check Empty explicitly. See the standard image package implementations.
	if r.Empty() {
		r = image.Rectangle{}
	}

	i.subImageCacheM.Lock()
	defer i.subImageCacheM.Unlock()

	// The image might already be disposed in another goroutine.
	// Recheck this.
	if i.isDisposed() {
		return nil
	}

	if img, ok := i.subImageCache[r]; ok {
		img.updateAccessTime()
		return img
	}

	if tick := render.Tick(); i.subImageGCLastTick < tick {
		i.subImageGCLastTick = tick

		for _, img := range i.subImageCache {
			if img.atime.Load()+recent.Window < tick {
				delete(i.subImageCache, img.bounds)
			}
		}
	}

	img := &Image{}
	img.image = i.image
	img.bounds = r
	img.original = i
	img.addr = img

	if i.subImageCache == nil {
		i.subImageCache = map[image.Rectangle]*Image{}
	}
	i.subImageCache[r] = img
	img.updateAccessTime()

	return img
}

// RecyclableSubImage returns a sub-image of the image from a global pool.
// The returned sub-image can be returned to the pool by calling [Image.Recycle].
//
// RecyclableSubImage is useful when you need to create many sub-images with different bounds,
// and want to avoid repeated allocations.
//
// Unlike [Image.SubImage], the returned sub-image is not cached internally.
// The caller is responsible for managing the lifecycle of the returned image.
//
// If the image is disposed, RecyclableSubImage panics.
func (i *Image) RecyclableSubImage(r image.Rectangle) *Image {
	i.copyCheck()
	if i.isDisposed() {
		panic("ggfx: the image is already disposed")
	}

	if i.isSubImage() {
		return i.original.RecyclableSubImage(r.Intersect(i.Bounds()))
	}

	r = r.Intersect(i.Bounds())
	// Need to check Empty explicitly. See the standard image package implementations.
	if r.Empty() {
		r = image.Rectangle{}
	}

	img := theImagePool.Get()
	img.image = i.image
	img.bounds = r
	img.original = i
	img.addr = img
	img.recyclable = true

	return img
}

// Recycle puts the Image struct back into a global pool for reuse, reducing allocations.
// After Recycle is called, the image must not be used; the behavior is undefined.
//
// Recycle can only be called on images created by [Image.RecyclableSubImage].
// Calling Recycle on any other image causes a panic.
func (i *Image) Recycle() {
	i.copyCheck()
	if !i.recyclable {
		panic("ggfx: Recycle can only be called on an image created by RecyclableSubImage")
	}

	// Clear all fields to release references and reset state.
	i.addr = nil
	i.image = nil
	i.original = nil
	i.bounds = image.Rectangle{}
	i.tmpVertices = i.tmpVertices[:0]
	i.tmpIndices = i.tmpIndices[:0]
	i.tmpUniforms = i.tmpUniforms[:0]
	clear(i.subImageCache)
	i.subImageGCLastTick = 0
	i.atime.Store(0)
	clear(i.usageCallbacks)
	i.usageCallbackCount.Store(0)
	i.recyclable = false

	theImagePool.Put(i)
}
