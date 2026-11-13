// Copyright 2016 Hajime Hoshi
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

package graphicscommand

import (
	"cmp"
	"fmt"
	"image"
	"slices"

	"github.com/ironpark/ggfx/internal/debug"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// Image represents an image that is implemented with OpenGL.
type Image struct {
	image          graphicsdriver.Image
	width          int
	height         int
	internalWidth  int
	internalHeight int
	screen         bool

	// format is the format of the image's pixels, and of the pixels of WritePixels and ReadPixels.
	format graphicsdriver.PixelFormat

	// readable reports whether the screen image can be a rendering source (see ScreenReadable).
	readable bool

	// screenFlipsY reports whether a draw to the screen image takes the projection with Y flipped
	// (see graphicsdriver.Surface.FlipsY).
	screenFlipsY bool

	// attribute is used only for logs.
	attribute string

	// id is an identifier for the image. This is used only when dumping the information.
	id int

	// bufferedWritePixelsArgs are the writes not enqueued yet, and bufferedBounds the union of their
	// regions, or more.
	bufferedWritePixelsArgs []writePixelsCommandArgs
	bufferedBounds          image.Rectangle

	// pendingWrite is the last write-pixels command enqueued for the image, which later writes may
	// join while it is in the current queue, of generation pendingWriteGeneration. touched is the
	// regions of the image the commands enqueued after it read or draw into; a write that overlaps
	// none of them can join pendingWrite, as running it there instead of now changes nothing.
	pendingWrite           *writePixelsCommand
	pendingWriteGeneration uint64
	touched                touchedSet
}

var nextImageID = 1

func genNextImageID() int {
	id := nextImageID
	nextImageID++
	return id
}

// NewImage returns a new RGBA8 image. The pixel data just after NewImage is undetermined.
// A non-nil surface makes the image the screen image of that surface.
func NewImage(width, height int, surface graphicsdriver.Surface, attribute string) *Image {
	return newImage(width, height, graphicsdriver.PixelFormatRGBA8, surface, attribute)
}

// NewImageWithFormat returns a new offscreen image of the given pixel format, which the driver
// must support (see SupportsPixelFormat). The pixel data just after NewImageWithFormat is
// undetermined.
func NewImageWithFormat(width, height int, format graphicsdriver.PixelFormat, attribute string) *Image {
	return newImage(width, height, format, nil, attribute)
}

func newImage(width, height int, format graphicsdriver.PixelFormat, surface graphicsdriver.Surface, attribute string) *Image {
	i := &Image{
		width:     width,
		height:    height,
		screen:    surface != nil,
		format:    format,
		readable:  surface != nil && ScreenReadable(surface),
		id:        genNextImageID(),
		attribute: attribute,
	}
	if surface != nil {
		i.screenFlipsY = surface.FlipsY()
	}
	c := &newImageCommand{
		result:    i,
		width:     width,
		height:    height,
		format:    format,
		surface:   surface,
		attribute: attribute,
	}
	theCommandQueueManager.enqueueCommand(c)
	return i
}

// Format returns the format of the image's pixels.
func (i *Image) Format() graphicsdriver.PixelFormat {
	return i.format
}

// readableSurface is a surface whose screen image can be a rendering source.
type readableSurface interface {
	// Readable reports whether the screen image can be a rendering source within a frame: a
	// draw from it reads what the frame drew to it before, in the order the commands are queued.
	Readable() bool
}

// ScreenReadable reports whether the screen image of surface can be a rendering source. Only a
// driver whose screen is a texture it can sample says so; the others' screens are never a source.
func ScreenReadable(surface graphicsdriver.Surface) bool {
	r, ok := surface.(readableSurface)
	return ok && r.Readable()
}

// flushBufferedWritePixels makes the buffered writes run before the commands enqueued after this
// call. The writes join the image's pending write-pixels command, earlier in the queue, when no
// command since it read or drew into their regions; otherwise they make a new command. Hoisting
// writes this way lets a renderer that creates an image and draws it right away, as a text
// renderer does with each glyph, upload many images in one command rather than one each, with
// the draws between them merged.
func (i *Image) flushBufferedWritePixels() {
	if len(i.bufferedWritePixelsArgs) == 0 {
		return
	}

	if i.canJoinPendingWrite() {
		c := i.pendingWrite
		for _, arg := range i.bufferedWritePixelsArgs {
			c.args = appendReplacing(c.args, &c.bounds, arg)
		}
	} else {
		c := theCommandQueueManager.newWritePixelsCommand()
		c.dst = i
		c.args = append(c.args, i.bufferedWritePixelsArgs...)
		c.bounds = i.bufferedBounds
		theCommandQueueManager.enqueueCommand(c)

		i.pendingWrite = c
		i.pendingWriteGeneration = theCommandQueueManager.generation
		w, h := i.InternalSize()
		i.touched.reset(w, h)
	}

	clear(i.bufferedWritePixelsArgs)
	i.bufferedWritePixelsArgs = keepSmall(i.bufferedWritePixelsArgs, maxKeptWrites)
	i.bufferedBounds = image.Rectangle{}
}

// appendReplacing appends arg to args, the writes of one image in order, and releases and removes
// the writes before it whose regions arg's covers. bounds is the union of the regions of args, or
// more, and is kept so: when arg's region misses it, no write is covered, and none is looked at.
func appendReplacing(args []writePixelsCommandArgs, bounds *image.Rectangle, arg writePixelsCommandArgs) []writePixelsCommandArgs {
	if len(args) > 0 && arg.region.Overlaps(*bounds) {
		var cur int
		for _, a := range args {
			if a.region.In(arg.region) {
				a.pixels.Release()
				continue
			}
			args[cur] = a
			cur++
		}
		clear(args[cur:])
		args = args[:cur]
	}
	if len(args) == 0 {
		*bounds = arg.region
	} else {
		*bounds = bounds.Union(arg.region)
	}
	return append(args, arg)
}

// hasPendingWrite reports whether the image's pending write-pixels command is still in the current
// queue, so that later writes may join it.
func (i *Image) hasPendingWrite() bool {
	return i.pendingWrite != nil && i.pendingWriteGeneration == theCommandQueueManager.generation
}

// canJoinPendingWrite reports whether the buffered writes can join the pending write-pixels
// command: it is still in the current queue, and no command after it read or drew into the
// buffered regions.
func (i *Image) canJoinPendingWrite() bool {
	if !i.hasPendingWrite() {
		return false
	}
	for _, arg := range i.bufferedWritePixelsArgs {
		if i.touched.overlaps(arg.region) {
			return false
		}
	}
	return true
}

// touch records that a command enqueued now reads or draws into region of the image, if a later
// write could otherwise join the pending write-pixels command before it. An empty region stands
// for the whole image.
func (i *Image) touch(region image.Rectangle) {
	if !i.hasPendingWrite() {
		return
	}
	if region.Empty() {
		w, h := i.InternalSize()
		region = image.Rect(0, 0, w, h)
	} else {
		// Filtering may read a pixel past the region's edges.
		region = region.Inset(-1)
	}
	i.touched.add(region)
}

func (i *Image) Dispose() {
	i.bufferedWritePixelsArgs = nil
	i.pendingWrite = nil
	i.touched = touchedSet{}
	c := &disposeImageCommand{
		target: i,
	}
	theCommandQueueManager.enqueueCommand(c)
}

func (i *Image) InternalSize() (int, int) {
	if i.screen {
		return i.width, i.height
	}
	if i.internalWidth == 0 {
		i.internalWidth = graphics.InternalImageSize(i.width)
	}
	if i.internalHeight == 0 {
		i.internalHeight = graphics.InternalImageSize(i.height)
	}
	return i.internalWidth, i.internalHeight
}

// DrawTriangles draws triangles with the given image.
//
// The vertex floats are:
//
//	0: Destination X in pixels
//	1: Destination Y in pixels
//	2: Source X in pixels
//	3: Source Y in pixels
//	4: Color R [0.0-1.0]
//	5: Color G
//	6: Color B
//	7: Color A
//	8: Custom0
//	9: Custom1
//	10: Custom2
//	11: Custom3
//
// The elements whose indices are 2 and 3 are used for the source image position.
//
// If the source image is not specified, i.e., the first element of srcs is nil, the
// elements for the source image position are passed as they are.
func (i *Image) DrawTriangles(srcs [graphics.ShaderSrcImageCount]*Image, vertices []float32, indices []uint32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions [graphics.ShaderSrcImageCount]image.Rectangle, shader *Shader, uniforms []uint32) {
	for _, src := range srcs {
		if src == nil {
			continue
		}
		if src.screen && !src.readable {
			panic("graphicscommand: the screen image cannot be the rendering source")
		}
		src.flushBufferedWritePixels()
	}
	i.flushBufferedWritePixels()

	for k, src := range srcs {
		if src != nil {
			src.touch(srcRegions[k])
		}
	}
	i.touch(dstRegion)

	theCommandQueueManager.enqueueDrawTrianglesCommand(i, srcs, vertices, indices, blend, dstRegion, srcRegions, shader, uniforms)
}

// ReadPixels reads the image's pixels, in the image's format.
// ReadPixels returns an error when an error happens in the graphics driver.
func (i *Image) ReadPixels(graphicsDriver graphicsdriver.Graphics, args []graphicsdriver.PixelsArgs) error {
	i.flushBufferedWritePixels()
	c := &readPixelsCommand{
		img:  i,
		args: args,
	}
	theCommandQueueManager.enqueueCommand(c)
	if err := theCommandQueueManager.flush(graphicsDriver, graphicsdriver.FlushModeIntermediate); err != nil {
		return err
	}
	return nil
}

// WritePixels writes pixels in the image's format to region.
func (i *Image) WritePixels(pixels *graphics.ManagedBytes, region image.Rectangle) {
	if got, want := pixels.Len(), i.format.BytesPerPixel()*region.Dx()*region.Dy(); got != want {
		panic(fmt.Sprintf("graphicscommand: len(pixels) must be %d but %d at WritePixels", want, got))
	}
	// Release the previous pixels if the region is included by the new region.
	// Successive WritePixels calls might accumulate the pixels and never release,
	// especially when the image is unmanaged (#3036).
	i.bufferedWritePixelsArgs = appendReplacing(i.bufferedWritePixelsArgs, &i.bufferedBounds, writePixelsCommandArgs{
		pixels: pixels,
		region: region,
	})
}

func LogImagesInfo(images []*Image) {
	slices.SortFunc(images, func(a, b *Image) int {
		return cmp.Compare(a.id, b.id)
	})
	for _, i := range images {
		w, h := i.InternalSize()
		var attr string
		if i.attribute != "" {
			attr = " (" + i.attribute + ")"
		} else if i.screen {
			attr = " (screen)"
		}
		if i.format != graphicsdriver.PixelFormatRGBA8 {
			attr += " " + i.format.String()
		}
		debug.FrameLogf("  %d: (%d, %d)%s\n", i.id, w, h, attr)
	}
}
