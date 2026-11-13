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

package render

import (
	"fmt"
	"image"
	"math"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
)

// mipmapLevel is a level of an image's mipmap above 0: the image at 1/2^level of its size.
type mipmapLevel struct {
	// img is the level, or nil for a level that cannot be made.
	img *atlas.Image
	// dirty reports whether the image changed since the level was made.
	dirty bool
	// used is the tick the level was last drawn from.
	used int64
}

// maxUnusedLevelTicks is how many ticks a level may go undrawn from before dropUnusedLevels drops it.
const maxUnusedLevelTicks = recent.Window

// maxMipmapLevel is the highest level a draw samples.
const maxMipmapLevel = 6

func canUseMipmap(imageType atlas.ImageType) bool {
	switch imageType {
	case atlas.ImageTypeRegular, atlas.ImageTypeUnmanaged:
		return true
	}
	return false
}

// dropUnusedLevels deallocates the levels not drawn from for maxUnusedLevelTicks ticks, and reports
// whether any level is left. A level drawn again later is made again. i's mutex must be held.
func (i *Image) dropUnusedLevels() bool {
	t := recent.Now()
	for level, l := range i.levels {
		if t-l.used <= maxUnusedLevelTicks {
			continue
		}
		if l.img != nil {
			l.img.Deallocate()
		}
		delete(i.levels, level)
	}
	return len(i.levels) > 0
}

func (i *Image) markLevelsDirty() {
	for level, l := range i.levels {
		l.dirty = true
		i.levels[level] = l
	}
}

func (i *Image) deallocateLevels() {
	for _, l := range i.levels {
		if l.img != nil {
			l.img.Deallocate()
		}
	}
	clear(i.levels)
}

func (i *Image) setLevel(level int, img *atlas.Image) {
	if i.levels == nil {
		i.levels = map[int]mipmapLevel{}
	}
	i.levels[level] = mipmapLevel{
		img:  img,
		used: recent.Now(),
	}
}

// level returns the image of the given level above 0, made anew if the image changed since, or
// nil if the level cannot be made. i's mutex must be held.
func (i *Image) level(level int) *atlas.Image {
	if level <= 0 {
		panic(fmt.Sprintf("render: invalid mipmap level: %d", level))
	}
	if !canUseMipmap(i.imageType) {
		panic("render: mipmap images for a screen image is not implemented yet")
	}

	l, ok := i.levels[level]
	if ok && !l.dirty {
		l.used = recent.Now()
		i.levels[level] = l
		return l.img
	}

	var src *atlas.Image
	if level == 1 {
		// The level is made from the image's pixels, with the pending writes applied.
		i.cache.writeBackPixelsIfNeeded(i.img)
		src = i.img
	} else {
		src = i.level(level - 1)
		if src == nil {
			i.setLevel(level, nil)
			return nil
		}
	}
	srcW := sizeForLevel(i.width, level-1)
	srcH := sizeForLevel(i.height, level-1)

	dstW := sizeForLevel(i.width, level)
	dstH := sizeForLevel(i.height, level)
	if dstW == 0 || dstH == 0 {
		i.setLevel(level, nil)
		return nil
	}
	// An atlas image panics with a too big size when actual allocation happens.
	// 4096 should be a safe size in most environments (#1399).
	// Unfortunately a precise max image size cannot be obtained here since this requires GPU access.
	if dstW > 4096 || dstH > 4096 {
		i.setLevel(level, nil)
		return nil
	}

	// As the level is overwritten, the one it replaces doesn't have to be cleared.
	s := l.img
	if s == nil {
		s = atlas.NewImageWithFormat(dstW, dstH, i.imageType, i.format)
	}

	var vs [4 * graphics.VertexFloatCount]float32
	graphics.QuadVerticesFromSrcAndMatrix(vs[:], 0, 0, float32(srcW), float32(srcH), 0.5, 0, 0, 0.5, 0, 0, 1, 1, 1, 1)
	dstRegion := image.Rect(0, 0, dstW, dstH)
	srcRegion := image.Rect(0, 0, srcW, srcH)
	s.DrawTriangles([graphics.ShaderSrcImageCount]*atlas.Image{src}, vs[:], graphics.QuadIndices(), graphicsdriver.BlendCopy, dstRegion, [graphics.ShaderSrcImageCount]image.Rectangle{srcRegion}, atlas.LinearFilterShader(), nil)
	i.setLevel(level, s)
	return s
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

// mipmapLevelFor returns the level a draw of the triangles samples its source from: the lowest any
// edge of them asks for.
func mipmapLevelFor(vertices []float32, indices []uint32) int {
	const n = graphics.VertexFloatCount
	level := math.MaxInt32
	for i := 0; i < len(indices); i += 3 {
		v0 := vertices[n*indices[i]:]
		v1 := vertices[n*indices[i+1]:]
		v2 := vertices[n*indices[i+2]:]
		level = min(level,
			mipmapLevelFromDistance(v0[0], v0[1], v1[0], v1[1], v0[2], v0[3], v1[2], v1[3]),
			mipmapLevelFromDistance(v1[0], v1[1], v2[0], v2[1], v1[2], v1[3], v2[2], v2[3]),
			mipmapLevelFromDistance(v2[0], v2[1], v0[0], v0[1], v2[2], v2[3], v0[2], v0[3]))
		if level == 0 {
			break
		}
	}
	if level == math.MaxInt32 {
		panic("render: the mipmap level must be calculated at least once but not")
	}
	return level
}

// mipmapLevelFromDistance returns an appropriate mipmap level for the given distance.
func mipmapLevelFromDistance(dx0, dy0, dx1, dy1, sx0, sy0, sx1, sy1 float32) int {
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

	return min(level, maxMipmapLevel)
}
