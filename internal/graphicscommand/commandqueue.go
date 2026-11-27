// Copyright 2023 The Ebitengine Authors
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
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/debug"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
)

const (
	is32bit = 1 >> (^uint(0) >> 63)
	is64bit = 1 - is32bit

	// MaxVertexCount is the maximum number of vertices for one draw call.
	//
	// On 64bit architectures, this value is 2^32-1, as the index type is uint32.
	// This value cannot be exactly 2^32 especially with WebGL 2, as 2^32th vertex is not rendered correctly.
	// See https://registry.khronos.org/webgl/specs/latest/2.0/#5.18 .
	//
	// On 32bit architectures, this value is an adjusted number so that maxVertexFloatCount doesn't overflow int.
	MaxVertexCount = is64bit*math.MaxUint32 + is32bit*(math.MaxInt32/graphics.VertexFloatCount)

	maxVertexFloatCount = MaxVertexCount * graphics.VertexFloatCount
)

// FlushCommands executes queued commands with the given flush mode.
func FlushCommands(graphicsDriver graphicsdriver.Graphics, mode graphicsdriver.FlushMode) error {
	if err := theCommandQueueManager.flush(graphicsDriver, mode); err != nil {
		return err
	}
	return nil
}

// commandQueue is a command queue for drawing commands.
type commandQueue struct {
	// mgr is the manager that owns this queue, or nil for standalone queues in tests.
	mgr *commandQueueManager

	// commands is a queue of drawing commands.
	commands []command

	// vertices represents a vertices data in OpenGL's array buffer.
	vertices []float32
	indices  []uint32

	// verticesUse and indicesUse track how many vertices and indices the flushes of recent frames
	// held for standalone queues without a manager. Managed queues share peaks in mgr.
	verticesUse recent.Peak
	indicesUse  recent.Peak

	tmpNumVertexFloats int

	// reportedBufferBytes is this queue's last contribution to the global bufferBytes counter.
	reportedBufferBytes int64

	// lastDstRegion and lastSrcRegions are the regions of the draw enqueued last, which the last
	// command holds when it is a draw (see sameRegionsAsLast).
	lastDstRegion  image.Rectangle
	lastSrcRegions [graphics.ShaderSrcImageCount]image.Rectangle

	// lastDraw is the draw command enqueued last, whose uniform blocks a new draw command's are
	// compared with, for merging and for their versions.
	lastDraw *drawTrianglesCommand

	// execVersions and execFlipsY are those of the draw command run last in the flush in progress;
	// zero versions before the first.
	execVersions graphicsdriver.UniformVersions
	execFlipsY   bool

	drawTrianglesCommandPool drawTrianglesCommandPool
	writePixelsCommandPool   writePixelsCommandPool

	// uploadedBytes are the pixels the write-pixels commands uploaded, given back to the pool of
	// bytes at the end of the frame, and tmpPixelsArgs the scratch they are uploaded with.
	uploadedBytes [][]byte
	tmpPixelsArgs []graphicsdriver.PixelsArgs

	uint32sBuffer uint32sBuffer

	// flushDriver, flushMode, flushSync and flushLogger are the arguments of the flush in
	// progress, flushErr its error when it is synchronous, and runFlushFunc q.runFlush, bound once.
	flushDriver  graphicsdriver.Graphics
	flushMode    graphicsdriver.FlushMode
	flushSync    bool
	flushLogger  debug.FrameLogger
	flushErr     error
	runFlushFunc func()
}

// growSlice ensures s has room for add more elements. When growing, it steps geometrically (2x)
// and seeds at least to seed so new or shrunk queues allocate once instead of stepping through
// Go's ~1.25x append growth.
func growSlice[T any](s []T, add, seed int) []T {
	need := len(s) + add
	if need <= cap(s) {
		return s
	}
	target := max(need, 2*cap(s), seed, minRetainedElements)
	return slices.Grow(s, target-len(s))
}

func (q *commandQueue) vertexSeed() int {
	if q.mgr != nil {
		return q.mgr.vertexSeed()
	}
	return 0
}

func (q *commandQueue) indexSeed() int {
	if q.mgr != nil {
		return q.mgr.indexSeed()
	}
	return 0
}

func (q *commandQueue) updateBufferBytes() {
	if q.mgr != &theCommandQueueManager {
		return
	}
	cur := int64(cap(q.vertices))*4 + int64(cap(q.indices))*4
	if diff := cur - q.reportedBufferBytes; diff != 0 {
		bufferBytes.Add(diff)
		q.reportedBufferBytes = cur
	}
}

func (q *commandQueue) appendIndices(indices []uint32, offset uint32) {
	n := len(q.indices)
	if n+len(indices) > cap(q.indices) {
		q.indices = growSlice(q.indices, len(indices), q.indexSeed())
		q.updateBufferBytes()
	}
	q.indices = append(q.indices, indices...)
	graphics.OffsetIndices(q.indices[n:], offset)
}

// mustUseDifferentVertexBuffer reports whether a different vertex buffer must be used.
func mustUseDifferentVertexBuffer(nextNumVertexFloats int) bool {
	return nextNumVertexFloats < 0 || nextNumVertexFloats > maxVertexFloatCount
}

// EnqueueDrawTrianglesCommand enqueues a drawing-image command.
func (q *commandQueue) EnqueueDrawTrianglesCommand(dst *Image, srcs [graphics.ShaderSrcImageCount]*Image, vertices []float32, indices []uint32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions [graphics.ShaderSrcImageCount]image.Rectangle, shader *Shader, uniforms []uint32) {
	if len(vertices) > maxVertexFloatCount {
		panic(fmt.Sprintf("graphicscommand: len(vertices) must equal to or less than %d but was %d", maxVertexFloatCount, len(vertices)))
	}

	var split bool
	if mustUseDifferentVertexBuffer(q.tmpNumVertexFloats + len(vertices)) {
		q.tmpNumVertexFloats = 0
		split = true
	}

	// Assume that all the image sizes are same.
	// Assume that the images are packed from the front in the slice srcs.
	if len(q.vertices)+len(vertices) > cap(q.vertices) {
		q.vertices = growSlice(q.vertices, len(vertices), q.vertexSeed())
		q.updateBufferBytes()
	}
	q.vertices = append(q.vertices, vertices...)
	q.appendIndices(indices, uint32(q.tmpNumVertexFloats/graphics.VertexFloatCount))
	q.tmpNumVertexFloats += len(vertices)

	// TODO: If dst is the screen, reorder the command to be the last.
	var last *drawTrianglesCommand
	if !split && 0 < len(q.commands) {
		if c, ok := q.commands[len(q.commands)-1].(*drawTrianglesCommand); ok && c.canMergeWithoutUniforms(dst, srcs, blend, shader) {
			last = c
		}
	}

	// Whether the user's block is the last draw command's tells both whether the draw merges and,
	// if it does not, whether its block keeps that command's version.
	prev := q.lastDraw
	sameUser := prev != nil && slices.Equal(prev.uniforms[graphics.PreservedUniformDwordCount:], uniforms)

	// Most draws that merge are told so by what the uniforms are made from, without making them.
	if last != nil && sameUser && q.sameRegionsAsLast(shader, dstRegion, srcRegions) {
		q.mergeIntoLast(last, len(vertices), len(indices), dstRegion)
		return
	}
	q.lastDstRegion = dstRegion
	q.lastSrcRegions = srcRegions

	// prependPreservedUniforms not only prepends values to the given slice but also creates a new slice.
	// Allocating a new slice is necessary to make EnqueueDrawTrianglesCommand safe so far.
	uniforms = q.prependPreservedUniforms(uniforms, dst, srcs, dstRegion, srcRegions)

	// Zero the internal uniforms the shader never reads, so that draws which differ only in
	// their regions can be merged.
	if shader.ir != nil {
		shader.ir.FilterInternalUniforms(uniforms[:graphics.PreservedUniformDwordCount])
	}

	sameInternal := prev != nil && slices.Equal(prev.uniforms[:graphics.PreservedUniformDwordCount], uniforms[:graphics.PreservedUniformDwordCount])
	if last != nil && sameUser && sameInternal {
		q.mergeIntoLast(last, len(vertices), len(indices), dstRegion)
		// The merged command keeps the last command's uniforms, which are equal.
		q.uint32sBuffer.free(uniforms)
		return
	}

	c := q.drawTrianglesCommandPool.get()
	c.dst = dst
	c.srcs = srcs
	c.numVertexFloats = len(vertices)
	c.blend = blend
	c.dstRegions = append(c.dstRegions[:0], graphicsdriver.DstRegion{
		Region:     dstRegion,
		IndexCount: len(indices),
	})
	c.shader = shader
	c.uniforms = uniforms
	c.sameInternalAsPrev = sameInternal
	c.sameUserAsPrev = sameUser
	c.firstCaller = ""
	if debug.IsDebug {
		file, line, typ := debug.FirstCaller()
		switch typ {
		case debug.CallerTypeRegular:
			c.firstCaller = fmt.Sprintf("%s:%d", file, line)
		case debug.CallerTypeInternal:
			c.firstCaller = fmt.Sprintf("%s:%d (internal)", file, line)
		}
	}
	q.commands = append(q.commands, c)
	q.lastDraw = c
}

// sameRegionsAsLast reports whether a draw with shader and the given regions has the internal
// uniform block of the last command, which draws to the same destination from the same sources
// with shader: it does when the draw enqueued last, whose block is the last command's, had the
// same regions, as far as shader reads them. It may report false for equal blocks.
func (q *commandQueue) sameRegionsAsLast(shader *Shader, dstRegion image.Rectangle, srcRegions [graphics.ShaderSrcImageCount]image.Rectangle) bool {
	readsDst, readsSrc := shader.readsRegions()
	if readsDst && q.lastDstRegion != dstRegion {
		return false
	}
	if readsSrc && q.lastSrcRegions != srcRegions {
		return false
	}
	return true
}

// uniformVersions returns the versions of the blocks of c, the next draw command run in the flush
// in progress, whose Y flip is flipsY. A block keeps the version of the draw command run before
// it when it was equal to that command's at enqueueing and the flip, which only the internal
// block takes, is the same.
func (q *commandQueue) uniformVersions(c *drawTrianglesCommand, flipsY bool) graphicsdriver.UniformVersions {
	v := q.execVersions
	if v.Internal == 0 || !c.sameInternalAsPrev || flipsY != q.execFlipsY {
		v.Internal = graphicsdriver.NewUniformVersion()
	}
	if v.User == 0 || !c.sameUserAsPrev {
		v.User = graphicsdriver.NewUniformVersion()
	}
	q.execVersions = v
	q.execFlipsY = flipsY
	return v
}

// mergeIntoLast adds the vertices and indices appended last to last, the last command.
func (q *commandQueue) mergeIntoLast(last *drawTrianglesCommand, vertexFloats, indices int, dstRegion image.Rectangle) {
	last.numVertexFloats += vertexFloats
	if r := &last.dstRegions[len(last.dstRegions)-1]; r.Region == dstRegion {
		r.IndexCount += indices
		return
	}
	last.dstRegions = append(last.dstRegions, graphicsdriver.DstRegion{
		Region:     dstRegion,
		IndexCount: indices,
	})
}

// Enqueue enqueues a drawing command other than a draw-triangles command.
//
// For a draw-triangles command, use EnqueueDrawTrianglesCommand.
func (q *commandQueue) Enqueue(command command) {
	// TODO: If dst is the screen, reorder the command to be the last.
	q.commands = append(q.commands, command)
}

// Flush flushes the command queue.
func (q *commandQueue) Flush(graphicsDriver graphicsdriver.Graphics, mode graphicsdriver.FlushMode) error {
	// Disable asynchronous rendering when presenting, as vsync is on and asynchronous rendering
	// causes a rendering delay (#2822).
	sync := mode == graphicsdriver.FlushModePresent
	if !sync {
		for _, c := range q.commands {
			if c.NeedsSync() {
				sync = true
				break
			}
		}
	}

	// The flush's arguments are kept in q rather than captured by a closure, so that a flush
	// allocates nothing. q is not touched by anyone else until it is put back in the pool.
	q.flushDriver = graphicsDriver
	q.flushMode = mode
	q.flushSync = sync
	q.flushLogger = debug.SwitchFrameLogger()
	q.flushErr = nil
	if q.runFlushFunc == nil {
		q.runFlushFunc = q.runFlush
	}
	runOnRenderThread(q.runFlushFunc, sync)

	if sync {
		err := q.flushErr
		q.flushErr = nil
		theCommandQueueManager.putCommandQueue(q)
		return err
	}

	return nil
}

// runFlush runs the flush Flush set up, on the render thread.
func (q *commandQueue) runFlush() {
	driver, mode, sync, logger := q.flushDriver, q.flushMode, q.flushSync, q.flushLogger
	q.flushDriver, q.flushLogger = nil, nil
	defer logger.Flush()

	if err := q.flush(driver, mode, logger); err != nil {
		if sync {
			q.flushErr = err
			return
		}
		theCommandQueueManager.storeAsyncErr(err)
	} else if mode != graphicsdriver.FlushModeIntermediate {
		completedFrames.Add(1)
	}

	if !sync {
		theCommandQueueManager.putCommandQueue(q)
	}
}

// resetCommands releases commands and buffers after a flush, whether it succeeded or failed.
func (q *commandQueue) resetCommands(mode graphicsdriver.FlushMode) {
	// Release the commands explicitly (#1803).
	// Apparently, the part of a slice between len and cap-1 still holds references.
	// Then, resetting the length by [:0] doesn't release the references.
	for i, c := range q.commands {
		switch c := c.(type) {
		case *drawTrianglesCommand:
			q.drawTrianglesCommandPool.put(c)
		case *writePixelsCommand:
			// An image's pendingWrite may still point to c, but it is not used once the
			// queue's generation has passed.
			q.writePixelsCommandPool.put(c)
		case *callbackCommand:
			// Complete any unexecuted callback when a flush fails early and drops the queue so
			// ordering gates (e.g. Surface resize) cannot stick shut forever.
			if !c.done {
				c.done = true
				c.f()
			}
		}
		q.commands[i] = nil
	}
	q.commands = q.commands[:0]
	q.lastDraw = nil
	q.tmpNumVertexFloats = 0
	// No command refers to the vertices or indices any more, and the driver has copied them.
	if q.mgr != nil {
		vLimit, iLimit := q.mgr.bufferLimits()
		q.vertices = shrinkToLimit(q.vertices, vLimit)
		q.indices = shrinkToLimit(q.indices, iLimit)
		q.updateBufferBytes()
	} else {
		q.vertices = shrinkToRecentUseWindow(q.vertices, &q.verticesUse, BufferPeakWindow)
		q.indices = shrinkToRecentUseWindow(q.indices, &q.indicesUse, BufferPeakWindow)
	}

	// Return uploaded bytes on every flush (including intermediate flushes) so headless
	// ReadPixels loops and external replays do not leak buffers.
	for i, bs := range q.uploadedBytes {
		graphics.PutBytes(bs)
		q.uploadedBytes[i] = nil
	}
	q.uploadedBytes = keepSmall(q.uploadedBytes, 4*maxKeptWrites)

	if mode != graphicsdriver.FlushModeIntermediate {
		q.uint32sBuffer.reset()
		// The frame's upload bytes are back in the pool.
		graphics.AdvanceBytesPoolFrame()
	} else {
		q.uint32sBuffer.rewind()
	}
}

// flush must be called the render thread.
func (q *commandQueue) flush(graphicsDriver graphicsdriver.Graphics, mode graphicsdriver.FlushMode, logger debug.FrameLogger) (err error) {
	// Complete the frame even when no commands remain after an intermediate flush.
	if len(q.commands) == 0 && mode == graphicsdriver.FlushModeIntermediate {
		return nil
	}
	defer q.resetCommands(mode)

	es := q.indices
	vs := q.vertices
	logger.FrameLogf("Graphics commands:\n")

	// The first draw command compares with none run (see uniformVersions).
	q.execVersions = graphicsdriver.UniformVersions{}
	q.execFlipsY = false

	if err := graphicsDriver.Begin(); err != nil {
		return err
	}

	defer func() {
		// Call End even if an error causes, or the graphics driver's state might be stale (#2388).
		if graphicsErr := graphicsDriver.End(mode); graphicsErr != nil {
			err = errors.Join(err, graphicsErr)
		}
	}()

	flushes.Add(1)
	commandsRun.Add(int64(len(q.commands)))
	verticesSent.Add(int64(len(q.vertices) / graphics.VertexFloatCount))

	cs := q.commands
	for len(cs) > 0 {
		var nv int
		var ne int
		var nc int
		for _, c := range cs {
			if dtc, ok := c.(*drawTrianglesCommand); ok {
				if nc > 0 && mustUseDifferentVertexBuffer(nv+dtc.numVertices()) {
					break
				}
				nv += dtc.numVertices()
				ne += dtc.numIndices()
			}
			nc++
		}
		if 0 < ne {
			if err := graphicsDriver.SetVertices(vs[:nv], es[:ne]); err != nil {
				return err
			}
			es = es[ne:]
		}
		if 0 < nv {
			vs = vs[nv:]
		}
		var indexOffset int
		for _, c := range cs[:nc] {
			if err := c.Exec(q, graphicsDriver, indexOffset); err != nil {
				return err
			}
			if debug.IsDebug {
				str := c.String()
				for {
					head, tail, ok := strings.Cut(str, "\n")
					logger.FrameLogf("  %s\n", head)
					if !ok {
						break
					}
					str = tail
				}
			}
			// TODO: indexOffset should be reset if the command type is different
			// from the previous one. This fix is needed when another drawing command is
			// introduced than drawTrianglesCommand.
			if dtc, ok := c.(*drawTrianglesCommand); ok {
				indexOffset += dtc.numIndices()
			}
		}
		cs = cs[nc:]
	}

	return nil
}

type rectangleF32 struct {
	x      float32
	y      float32
	width  float32
	height float32
}

func imageRectangleToRectangleF32(r image.Rectangle) rectangleF32 {
	return rectangleF32{
		x:      float32(r.Min.X),
		y:      float32(r.Min.Y),
		width:  float32(r.Dx()),
		height: float32(r.Dy()),
	}
}

func (q *commandQueue) prependPreservedUniforms(uniforms []uint32, dst *Image, srcs [graphics.ShaderSrcImageCount]*Image, dstRegion image.Rectangle, srcRegions [graphics.ShaderSrcImageCount]image.Rectangle) []uint32 {
	origUniforms := uniforms
	uniforms = q.uint32sBuffer.alloc(len(origUniforms) + graphics.PreservedUniformDwordCount)
	copy(uniforms[graphics.PreservedUniformDwordCount:], origUniforms)
	return prependPreservedUniforms(uniforms, dst, srcs, dstRegion, srcRegions)
}

func prependPreservedUniforms(uniforms []uint32, dst *Image, srcs [graphics.ShaderSrcImageCount]*Image, dstRegion image.Rectangle, srcRegions [graphics.ShaderSrcImageCount]image.Rectangle) []uint32 {
	// The layout is documented at graphics.PreservedUniformDwordCount.
	_ = uniforms[graphics.PreservedUniformDwordCount-1]

	dw, dh := dst.InternalSize()
	uniforms[graphics.DstTextureSizeUniformDwordIndex] = math.Float32bits(float32(dw))
	uniforms[graphics.DstTextureSizeUniformDwordIndex+1] = math.Float32bits(float32(dh))

	for i, src := range srcs {
		idx := graphics.SourceTextureSizeUniformDwordIndex + 4*i
		if src != nil {
			w, h := src.InternalSize()
			uniforms[idx] = math.Float32bits(float32(w))
			uniforms[idx+1] = math.Float32bits(float32(h))
		} else {
			uniforms[idx] = 0
			uniforms[idx+1] = 0
		}
		uniforms[idx+2] = 0
		uniforms[idx+3] = 0
	}

	dr := imageRectangleToRectangleF32(dstRegion)
	uniforms[graphics.DstRegionOriginUniformDwordIndex] = math.Float32bits(dr.x)
	uniforms[graphics.DstRegionOriginUniformDwordIndex+1] = math.Float32bits(dr.y)
	uniforms[graphics.DstRegionSizeUniformDwordIndex] = math.Float32bits(dr.width)
	uniforms[graphics.DstRegionSizeUniformDwordIndex+1] = math.Float32bits(dr.height)
	uniforms[graphics.DstRegionSizeUniformDwordIndex+2] = 0
	uniforms[graphics.DstRegionSizeUniformDwordIndex+3] = 0

	for i, r := range srcRegions {
		sr := imageRectangleToRectangleF32(r)
		idx := graphics.SourceImageRegionOriginUniformDwordIndex + 4*i
		uniforms[idx] = math.Float32bits(sr.x)
		uniforms[idx+1] = math.Float32bits(sr.y)
		uniforms[idx+2] = 0
		uniforms[idx+3] = 0
		idx = graphics.SourceImageRegionSizeUniformDwordIndex + 4*i
		uniforms[idx] = math.Float32bits(sr.width)
		uniforms[idx+1] = math.Float32bits(sr.height)
		uniforms[idx+2] = 0
		uniforms[idx+3] = 0
	}

	// The projection matrix maps texture pixels to clip space, column-major: the top row to Y = -1.
	// drawTrianglesCommand.Exec flips it for a driver whose render target for dst has its rows go
	// down (see graphicsdriver.Graphics.FlipsY). Draws to one destination get the same matrix,
	// which merging them needs.
	p := uniforms[graphics.ProjectionMatrixUniformDwordIndex : graphics.ProjectionMatrixUniformDwordIndex+16]
	clear(p)
	p[0] = math.Float32bits(2 / float32(dw))
	p[5] = math.Float32bits(2 / float32(dh))
	p[10] = math.Float32bits(1)
	p[12] = math.Float32bits(-1)
	p[13] = math.Float32bits(-1)
	p[15] = math.Float32bits(1)

	return uniforms
}

// Confirm the concrete value of graphics.PreservedUniformDwordCount.
var _ [0]struct{} = [graphics.PreservedUniformDwordCount - 72]struct{}{}

type commandQueuePool struct {
	cache []*commandQueue
	m     sync.Mutex
}

func (c *commandQueuePool) get() *commandQueue {
	c.m.Lock()
	defer c.m.Unlock()

	if len(c.cache) == 0 {
		return &commandQueue{}
	}

	q := c.cache[len(c.cache)-1]
	c.cache[len(c.cache)-1] = nil
	c.cache = c.cache[:len(c.cache)-1]
	return q
}

func (c *commandQueuePool) put(queue *commandQueue) {
	c.m.Lock()
	defer c.m.Unlock()

	c.cache = append(c.cache, queue)
}

type commandQueueManager struct {
	pool    commandQueuePool
	current *commandQueue

	// peakM guards verticesUse and indicesUse, which track the recent peak across all queues
	// so new or shrunk queues seed their capacity in one allocation.
	peakM       sync.Mutex
	verticesUse recent.Peak
	indicesUse  recent.Peak

	// generation counts the flushes, so that an image can tell whether a command it enqueued is
	// still in the current queue.
	generation uint64

	// asyncErr holds the error reported by an asynchronous flush until the next flush drains it.
	asyncErr atomic.Pointer[error]

	// lastPoolSweep is the recent.Now tick when idle pooled queues were last trimmed.
	lastPoolSweep atomic.Int64
}

var theCommandQueueManager commandQueueManager

func (c *commandQueueManager) getQueue() *commandQueue {
	q := c.pool.get()
	q.mgr = c
	return q
}

func (c *commandQueueManager) observeBuffers(vertices, indices int) {
	c.peakM.Lock()
	c.verticesUse.ObserveNowWindow(vertices, BufferPeakWindow)
	c.indicesUse.ObserveNowWindow(indices, BufferPeakWindow)
	c.peakM.Unlock()
}

func (c *commandQueueManager) bufferLimits() (vLimit, iLimit int) {
	c.peakM.Lock()
	now := recent.Now()
	c.verticesUse.AdvanceToWindow(now, BufferPeakWindow)
	c.indicesUse.AdvanceToWindow(now, BufferPeakWindow)
	vLimit = max(c.verticesUse.Max(), minRetainedElements)
	iLimit = max(c.indicesUse.Max(), minRetainedElements)
	c.peakM.Unlock()
	return vLimit, iLimit
}

func (c *commandQueueManager) vertexSeed() int {
	c.peakM.Lock()
	c.verticesUse.AdvanceToWindow(recent.Now(), BufferPeakWindow)
	peak := c.verticesUse.Max()
	c.peakM.Unlock()
	if peak == 0 {
		return 0
	}
	return retainedFor(peak)
}

func (c *commandQueueManager) indexSeed() int {
	c.peakM.Lock()
	c.indicesUse.AdvanceToWindow(recent.Now(), BufferPeakWindow)
	peak := c.indicesUse.Max()
	c.peakM.Unlock()
	if peak == 0 {
		return 0
	}
	return retainedFor(peak)
}

func (q *commandQueue) trimBuffers(vLimit, iLimit int) {
	q.vertices = shrinkToLimit(q.vertices, vLimit)
	q.indices = shrinkToLimit(q.indices, iLimit)
	q.updateBufferBytes()
	q.uint32sBuffer.reset()
}

// Trim shrinks idle command-queue vertex, index, and uniform buffers to their current peaks.
// Must be called while command-queue enqueueing and flushing are serialized (e.g. under atlas backendsM).
func Trim() {
	theCommandQueueManager.trim()
}

// FinalizeIdleUniforms records and clears pending uniform-buffer usage on every idle queue at the
// current frame clock, without issuing a driver EndFrame. Must be called under atlas backendsM
// after Sync (when no queue is in flight) and before Trim advances the idle clock.
func FinalizeIdleUniforms() {
	theCommandQueueManager.finalizeIdleUniforms()
}

// UniformBufferBytes returns the total capacity in bytes of uniform staging buffers across the
// current and pooled command queues.
func UniformBufferBytes() int64 {
	return theCommandQueueManager.uniformBufferBytes()
}

// DrainAsyncErr returns and clears any error reported by an asynchronous flush.
func DrainAsyncErr() error {
	return theCommandQueueManager.drainAsyncErr()
}

func (c *commandQueueManager) finalizeIdleUniforms() {
	if c.current != nil && len(c.current.commands) == 0 {
		c.current.uint32sBuffer.reset()
	}
	c.pool.m.Lock()
	for _, q := range c.pool.cache {
		q.uint32sBuffer.reset()
	}
	c.pool.m.Unlock()
}

func (c *commandQueueManager) uniformBufferBytes() int64 {
	var total int64
	if c.current != nil {
		total += int64(cap(c.current.uint32sBuffer.buf)) * 4
	}
	c.pool.m.Lock()
	for _, q := range c.pool.cache {
		total += int64(cap(q.uint32sBuffer.buf)) * 4
	}
	c.pool.m.Unlock()
	return total
}

func (c *commandQueueManager) trim() {
	vLimit, iLimit := c.bufferLimits()
	if c.current != nil && len(c.current.commands) == 0 {
		c.current.trimBuffers(vLimit, iLimit)
	}
	c.pool.m.Lock()
	for _, q := range c.pool.cache {
		q.trimBuffers(vLimit, iLimit)
	}
	c.pool.m.Unlock()
	c.lastPoolSweep.Store(recent.Now())
}

// sweepPoolIfNeeded periodically trims idle queues parked in the LIFO pool so an oversized
// queue at the bottom of the pool does not retain memory indefinitely during small frames.
func (c *commandQueueManager) sweepPoolIfNeeded() {
	now := recent.Now()
	last := c.lastPoolSweep.Load()
	if now-last < recent.Window {
		return
	}
	if !c.lastPoolSweep.CompareAndSwap(last, now) {
		return
	}
	vLimit, iLimit := c.bufferLimits()
	c.pool.m.Lock()
	for _, q := range c.pool.cache {
		q.trimBuffers(vLimit, iLimit)
	}
	c.pool.m.Unlock()
}

func (c *commandQueueManager) storeAsyncErr(err error) {
	if err == nil {
		return
	}
	for {
		oldErr := c.asyncErr.Load()
		var newErr error
		if oldErr != nil {
			newErr = errors.Join(*oldErr, err)
		} else {
			newErr = err
		}
		if c.asyncErr.CompareAndSwap(oldErr, &newErr) {
			return
		}
	}
}

func (c *commandQueueManager) drainAsyncErr() error {
	if ep := c.asyncErr.Swap(nil); ep != nil {
		return *ep
	}
	return nil
}

// newWritePixelsCommand returns a write-pixels command from the current queue's pool. The queue
// puts it back once it has run.
func (c *commandQueueManager) newWritePixelsCommand() *writePixelsCommand {
	if c.current == nil {
		c.current = c.getQueue()
	}
	return c.current.writePixelsCommandPool.get()
}

func (c *commandQueueManager) enqueueCommand(command command) {
	if c.current == nil {
		c.current = c.getQueue()
	}
	c.current.Enqueue(command)
}

// put can be called from any goroutines.
func (c *commandQueueManager) putCommandQueue(commandQueue *commandQueue) {
	c.pool.put(commandQueue)
	c.sweepPoolIfNeeded()
}

func (c *commandQueueManager) enqueueDrawTrianglesCommand(dst *Image, srcs [graphics.ShaderSrcImageCount]*Image, vertices []float32, indices []uint32, blend graphicsdriver.Blend, dstRegion image.Rectangle, srcRegions [graphics.ShaderSrcImageCount]image.Rectangle, shader *Shader, uniforms []uint32) {
	if c.current == nil {
		c.current = c.getQueue()
	}
	c.current.EnqueueDrawTrianglesCommand(dst, srcs, vertices, indices, blend, dstRegion, srcRegions, shader, uniforms)
}

func (c *commandQueueManager) flush(graphicsDriver graphicsdriver.Graphics, mode graphicsdriver.FlushMode) error {
	if err := c.drainAsyncErr(); err != nil {
		return err
	}

	// Switch the command queue. Record prev's buffer sizes before switching so the next queue
	// seeds from the peak even when prev flushes asynchronously.
	prev := c.current
	if prev != nil {
		c.observeBuffers(len(prev.vertices), len(prev.indices))
	}
	c.current = c.getQueue()
	c.generation++

	if prev == nil {
		return nil
	}
	if err := prev.Flush(graphicsDriver, mode); err != nil {
		return err
	}
	return c.drainAsyncErr()
}

// uint32sBuffer is a reusable buffer to allocate []uint32.
type uint32sBuffer struct {
	buf []uint32

	// used is how many uint32s were allocated since the last reset, over all the arrays buf used.
	used int
	// use tracks used over recent frames.
	use recent.Peak
}

func (b *uint32sBuffer) alloc(n int) []uint32 {
	buf := b.buf
	if len(buf)+n > cap(buf) {
		buf = make([]uint32, 0, max(recent.NextPowerOf2(len(buf)+n), 16))
	}
	s := buf[len(buf) : len(buf)+n]
	b.buf = buf[:len(buf)+n]
	b.used += n
	return s
}

// free gives back s, the slice the last alloc returned, which must no longer be used. A slice
// that is not the last one allocated is left alone.
func (b *uint32sBuffer) free(s []uint32) {
	n := len(s)
	if n == 0 || n > len(b.buf) || &s[0] != &b.buf[len(b.buf)-n] {
		return
	}
	b.buf = b.buf[:len(b.buf)-n]
	b.used -= n
}

// rewind reuses the backing buffer after an intermediate flush without ending the frame's peak observation.
func (b *uint32sBuffer) rewind() {
	b.buf = b.buf[:0]
}

// reset frees all the allocated slices; it must be called only when none of them is used any more.
func (b *uint32sBuffer) reset() {
	b.use.ObserveNow(b.used)
	b.used = 0
	limit := max(b.use.Max(), minRetainedElements)
	if cap(b.buf) > recent.ShrinkFactor*limit {
		// Slices still held elsewhere keep the old array alive by themselves.
		b.buf = make([]uint32, 0, retainedFor(limit))
		return
	}
	b.buf = b.buf[:0]
}

// minRetainedElements is the room a reused slice may always keep. A slice larger than
// recent.ShrinkFactor times its recent peak gives its room back, down to a quarter more than the
// peak (see retainedFor).
const minRetainedElements = 4096

// BufferPeakWindow is how many ticks the command queue keeps its vertex and index buffer peaks,
// so a brief idle pause does not drop and regrow tens of megabytes of staging buffers.
const BufferPeakWindow = 10 * recent.Window

// shrinkToLimit returns s emptied, reallocating smaller if cap(s) exceeds ShrinkFactor*limit.
func shrinkToLimit[T any](s []T, limit int) []T {
	if cap(s) > recent.ShrinkFactor*limit {
		return make([]T, 0, retainedFor(limit))
	}
	return s[:0]
}

// shrinkToRecentUseWindow records the use of s in use with the given window, and returns s emptied.
// If s has far more room than recent uses needed, the returned slice is a smaller one.
func shrinkToRecentUseWindow[T any](s []T, use *recent.Peak, window int64) []T {
	use.ObserveNowWindow(len(s), window)
	limit := max(use.Max(), minRetainedElements)
	return shrinkToLimit(s, limit)
}

// retainedFor is the room a reused slice keeps once shrunk, for a recent peak of limit.
func retainedFor(limit int) int { return limit + limit/4 }

// The queue's counts, for DebugInfo: they only grow, but for bufferBytes,
// the size of the vertex and index buffers kept between flushes.
var flushes, commandsRun, verticesSent, bufferBytes atomic.Int64

// completedFrames counts final (non-intermediate) flushes whose driver End returned without error
// on the render thread.
var completedFrames atomic.Uint64

// CompletedFrames returns the number of final (non-intermediate) flushes whose driver End has
// completed without error on the render thread.
func CompletedFrames() uint64 {
	return completedFrames.Load()
}

// Counts reports how many flushes and commands have run and vertices been
// sent since the program started, and the bytes the queue keeps for
// vertices and indices.
func Counts() (flushCount, commands, vertices, buffers int64) {
	return flushes.Load(), commandsRun.Load(), verticesSent.Load(), bufferBytes.Load()
}
