// Copyright 2025 The Ebitengine Authors
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

package vector

import (
	"fmt"
	"image"
	"runtime"
	"slices"
	"sync"
	"weak"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/bridge"
	"github.com/ironpark/ggfx/internal/pool"
	"github.com/ironpark/ggfx/internal/render"
)

// FillRule is the rule whether an overlapped region is rendered or not.
//
// The number of overlaps is counted with a limited precision, so a region with too many
// overlapping triangles can be rendered incorrectly.
type FillRule int

const (
	// FillRuleNonZero means that triangles are rendered based on the non-zero rule.
	// If and only if the number of overlaps is not 0, the region is rendered.
	FillRuleNonZero FillRule = iota

	// FillRuleEvenOdd means that triangles are rendered based on the even-odd rule.
	// If and only if the number of overlaps is odd, the region is rendered.
	FillRuleEvenOdd
)

var (
	// theFillPathsStates is keyed by weak pointers not to keep the destination images alive.
	// When a destination image is collected before being used again, releaseFillPathsState removes the entry.
	theFillPathsStates     = map[weak.Pointer[ggfx.Image]]*fillDst{}
	theFillPathsStatesPool = pool.Pool[*fillPathsState]{
		New: func() *fillPathsState {
			return &fillPathsState{}
		},
	}
	theFillPathM sync.Mutex
)

// FillOptions is options to fill a path.
type FillOptions struct {
	// FillRule is the rule whether an overlapped region is rendered or not.
	// The default (zero) value is FillRuleNonZero.
	FillRule FillRule
}

// DrawPathOptions is options to draw a path.
type DrawPathOptions struct {
	// AntiAlias is whether the path is drawn with anti-aliasing.
	// The default (zero) value is false.
	AntiAlias bool

	// ColorScale is the color scale to apply to the path.
	// The default (zero) value is identity, which is (1, 1, 1, 1) (white).
	ColorScale ggfx.ColorScale

	// Blend is the blend mode to apply to the path.
	// The default (zero) value is ggfx.BlendSourceOver.
	Blend ggfx.Blend
}

// FillPath fills the specified path with the specified options.
func FillPath(dst *ggfx.Image, path *Path, fillOptions *FillOptions, drawPathOptions *DrawPathOptions) {
	fillPath(dst, path, nil, fillOptions, drawPathOptions)
}

// fillPath fills path, or its stroke if stroke is not nil. The stroke is built in the batch's own
// path rather than built elsewhere and copied.
func fillPath(dst *ggfx.Image, path *Path, stroke *AddStrokeOptions, fillOptions *FillOptions, drawPathOptions *DrawPathOptions) {
	if drawPathOptions == nil {
		drawPathOptions = &DrawPathOptions{}
	}
	if fillOptions == nil {
		fillOptions = &FillOptions{}
	}

	bounds := dst.Bounds()

	// Get the original image if dst is a sub-image to integrate the callbacks.
	dst = originalImage(dst)

	theFillPathM.Lock()
	defer theFillPathM.Unlock()

	key := weak.Make(dst)

	d, ok := theFillPathsStates[key]
	if !ok {
		d = &fillDst{}
		d.callback = d.fill
		theFillPathsStates[key] = d
		d.cleanup = runtime.AddCleanup(dst, releaseFillPathsState, key)
	}
	s := d.state
	if s != nil && (s.antialias != drawPathOptions.AntiAlias || s.blend != drawPathOptions.Blend || s.fillRule != fillOptions.FillRule) {
		// Filling draws to dst, which would run the callback, so the batch goes idle first. s is
		// kept for the next batch.
		removeUsageCallback(dst, d.token)
		d.state = nil
		s.fillPaths(dst)
		s.reset()
	}
	if d.state == nil {
		// The destination goes from idle to pending: this is the only time the callback is added.
		if s == nil {
			s = theFillPathsStatesPool.Get()
		}
		d.state = s
		d.token = addUsageCallback(dst, d.callback)
	}
	s.antialias = drawPathOptions.AntiAlias
	s.blend = drawPathOptions.Blend
	s.fillRule = fillOptions.FillRule
	s.addPath(path, stroke, bounds, drawPathOptions.ColorScale)
}

// fill is the usage callback of a destination with pending paths. It fills them.
func (d *fillDst) fill(dst *ggfx.Image) {
	if originalImage(dst) != dst {
		panic("vector: dst must be the original image")
	}

	theFillPathM.Lock()
	defer theFillPathM.Unlock()

	s := d.state
	if s == nil {
		panic("vector: fillPathsState must exist here")
	}
	// Remove the callback not to call this twice.
	removeUsageCallback(dst, d.token)
	s.fillPaths(dst)
	s.reset()
	// The entry and its cleanup stay for the image's next batch; the state goes back to the pool.
	d.state = nil
	theFillPathsStatesPool.Put(s)
}

// releaseFillPathsState discards the state for a destination image that was collected before being used again.
func releaseFillPathsState(key weak.Pointer[ggfx.Image]) {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()

	// The destination image is already collected, and its usage callbacks are gone with it.
	d, ok := theFillPathsStates[key]
	if !ok {
		return
	}
	delete(theFillPathsStates, key)
	if s := d.state; s != nil {
		d.state = nil
		s.reset()
		theFillPathsStatesPool.Put(s)
	}
}

// fillDst is the entry of a destination image paths were filled to: the batch of paths pending
// for it, if any, with the token of the usage callback that fills them, and the cleanup that
// removes the entry once the image is collected. The entry stays while the image lives, so that
// its later batches register no cleanup again.
type fillDst struct {
	state *fillPathsState
	// token is the usage callback's while state is not nil.
	token int64
	// callback is fill, bound once so that adding it allocates nothing. It refers to d only,
	// not to the destination, which the image's callbacks would otherwise keep alive.
	callback func(dst *ggfx.Image)
	cleanup  runtime.Cleanup
}

// imageHooks are what vector needs of ggfx.Image that ggfx does not export.
var imageHooks = bridge.Image[*ggfx.Image]()

func originalImage(img *ggfx.Image) *ggfx.Image {
	return imageHooks.Original(img)
}

func addUsageCallback(img *ggfx.Image, fn func(img *ggfx.Image)) int64 {
	return imageHooks.AddUsageCallback(img, fn)
}

func removeUsageCallback(img *ggfx.Image, token int64) {
	imageHooks.RemoveUsageCallback(img, token)
}

type offsetAndColor struct {
	offsetX    float32
	offsetY    float32
	colorR     float32
	colorG     float32
	colorB     float32
	colorA     float32
	imageIndex int
}

var (
	offsetAndColorsNonAA = []offsetAndColor{
		{
			offsetX: 0,
			offsetY: 0,
			colorR:  1,
			colorG:  0,
			colorB:  0,
			colorA:  0,
		},
	}

	// https://learn.microsoft.com/en-us/windows/win32/api/d3d11/ne-d3d11-d3d11_standard_multisample_quality_levels
	offsetAndColorsAA = []offsetAndColor{
		{
			offsetX:    1.0 / 16.0,
			offsetY:    -3.0 / 16.0,
			colorR:     1,
			colorG:     0,
			colorB:     0,
			colorA:     0,
			imageIndex: 0,
		},
		{
			offsetX:    -1.0 / 16.0,
			offsetY:    3.0 / 16.0,
			colorR:     0,
			colorG:     1,
			colorB:     0,
			colorA:     0,
			imageIndex: 0,
		},
		{
			offsetX:    5.0 / 16.0,
			offsetY:    1.0 / 16.0,
			colorR:     0,
			colorG:     0,
			colorB:     1,
			colorA:     0,
			imageIndex: 0,
		},
		{
			offsetX:    -3.0 / 16.0,
			offsetY:    -5.0 / 16.0,
			colorR:     0,
			colorG:     0,
			colorB:     0,
			colorA:     1,
			imageIndex: 0,
		},
		{
			offsetX:    -5.0 / 16.0,
			offsetY:    5.0 / 16.0,
			colorR:     1,
			colorG:     0,
			colorB:     0,
			colorA:     0,
			imageIndex: 1,
		},
		{
			offsetX:    -7.0 / 16.0,
			offsetY:    -1.0 / 16.0,
			colorR:     0,
			colorG:     1,
			colorB:     0,
			colorA:     0,
			imageIndex: 1,
		},
		{
			offsetX:    3.0 / 16.0,
			offsetY:    7.0 / 16.0,
			colorR:     0,
			colorG:     0,
			colorB:     1,
			colorA:     0,
			imageIndex: 1,
		},
		{
			offsetX:    7.0 / 16.0,
			offsetY:    -7.0 / 16.0,
			colorR:     0,
			colorG:     0,
			colorB:     0,
			colorA:     1,
			imageIndex: 1,
		},
	}
)

// theAtlas manages the atlas for stencil buffer images.
// theAtlas is a singleton to avoid unnecessary texture allocations.
//
// theAtlas methods are used only at fillPathsState.fillPaths, and should be protected by theFillPathM.
var theAtlas atlas

type fillPathsState struct {
	paths  []*Path
	colors []ggfx.ColorScale
	bounds []image.Rectangle

	// slots are where the stencil buffers of the paths are, and hasQuads whether a path whose
	// buffers are drawn has a curve, for fillPaths.
	slots    []stencilSlot
	hasQuads []bool

	antialias bool
	blend     ggfx.Blend
	fillRule  FillRule
}

func (f *fillPathsState) reset() {
	for _, p := range f.paths {
		p.Reset()
	}
	f.paths = f.paths[:0]
	f.bounds = f.bounds[:0]
	f.colors = slices.Delete(f.colors, 0, len(f.colors))
}

// addPath adds a copy of path, or its stroke if stroke is not nil.
func (f *fillPathsState) addPath(path *Path, stroke *AddStrokeOptions, bounds image.Rectangle, clr ggfx.ColorScale) {
	if path == nil && stroke == nil {
		return
	}

	f.paths = slices.Grow(f.paths, 1)[:len(f.paths)+1]
	if f.paths[len(f.paths)-1] == nil {
		f.paths[len(f.paths)-1] = &Path{}
	}
	dst := f.paths[len(f.paths)-1]
	f.bounds = append(f.bounds, bounds)
	f.colors = append(f.colors, clr)
	if stroke != nil {
		// dst is empty, as reset leaves it.
		dst.AddStroke(path, stroke)
		return
	}
	dst.addSubPaths(len(path.subPaths))
	for i, subPath := range path.subPaths {
		dst.subPaths[i].start = subPath.start
		dst.subPaths[i].closed = subPath.closed
		dst.subPaths[i].invalid = subPath.invalid
		dst.subPaths[i].ops = slices.Grow(dst.subPaths[i].ops, len(subPath.ops))[:len(subPath.ops)]
		copy(dst.subPaths[i].ops, subPath.ops)
	}
}

// fillPaths fills the specified path with the specified color.
//
// fillPaths callers must be protected by theFillPathM.
func (f *fillPathsState) fillPaths(dst *ggfx.Image) {
	if len(f.paths) != len(f.colors) {
		panic("vector: the number of paths and colors must be the same")
	}

	vs := theVertices[:0]
	is := theIndices[:0]
	defer func() {
		theVertices = vs
		theIndices = is
	}()

	theRenderTarget = dst
	f.placeStencilBuffers(dst.Bounds())

	offsetAndColors := offsetAndColorsNonAA
	if f.antialias {
		offsetAndColors = offsetAndColorsAA
	}

	fillShader, err := ensureStencilBufferShaders()
	if err != nil {
		panic(fmt.Sprintf("vector: failed to create stencil buffer shader: %v", err))
	}
	bezierShader, err := ensureStencilBufferBezierShader()
	if err != nil {
		panic(fmt.Sprintf("vector: failed to create stencil buffer bezier shader: %v", err))
	}

	// First, render the polygons roughly, and second, the bezier curves, to the stencil buffers
	// not drawn yet. The blending is additive, so the order of the triangles does not matter.
	//
	// A contained path's points are in its stencil buffer, and a sample offset moves them less than
	// half a pixel, so its triangles cover no pixel center of another stencil buffer. The bezier
	// triangles' control points may be farther out, but the bezier shader adds zero outside the
	// curve, which is in the path's bounds. So the contained paths on an image are rendered to the
	// whole image in one call, as if each were clipped by its stencil buffer, and leave the buffers
	// cached there before as they were. The other paths are clipped by their stencil buffers' bounds
	// one by one.
	stencilOp := &ggfx.DrawTrianglesShaderOptions{}
	stencilOp.Blend = ggfx.BlendLighter
	stencilBuffers := 1
	if f.antialias {
		stencilBuffers = 2
	}
	images := theStencilImages[:0]
	f.hasQuads = slices.Grow(f.hasQuads[:0], len(f.paths))[:len(f.paths)]
	for i, path := range f.paths {
		s := &f.slots[i]
		f.hasQuads[i] = s.draw && path.hasQuadTo()
		if s.draw && f.batchable(i) && !slices.Contains(images, s.img) {
			images = append(images, s.img)
		}
	}
	defer func() {
		clear(images)
		theStencilImages = images[:0]
	}()
	for _, bezier := range []bool{false, true} {
		shader := fillShader
		if bezier {
			shader = bezierShader
		}

		for _, img := range images {
			vs, is = vs[:0], is[:0]
			for i, path := range f.paths {
				s := &f.slots[i]
				if !s.draw || s.img != img || !f.batchable(i) || bezier && !f.hasQuads[i] {
					continue
				}
				pp := theRenderingBounds[i].Min
				for _, oac := range offsetAndColors {
					vs, is = appendStencilTriangles(vs, is, path, oac, pp, s.buffer(f.antialias, oac.imageIndex).Min, bezier)
				}
			}
			f.drawTrianglesShader(img, vs, is, shader, stencilOp)
		}

		// The samples of a stencil buffer are in its channels, so they are drawn in one call.
		for i, path := range f.paths {
			s := &f.slots[i]
			if !s.draw || f.batchable(i) || bezier && !f.hasQuads[i] {
				continue
			}
			pp := theRenderingBounds[i].Min
			for k := range stencilBuffers {
				b := s.buffer(f.antialias, k)
				vs, is = vs[:0], is[:0]
				for _, oac := range offsetAndColors {
					if oac.imageIndex == k {
						vs, is = appendStencilTriangles(vs, is, path, oac, pp, b.Min, bezier)
					}
				}
				if len(is) == 0 {
					continue
				}
				stencilBufferImage := s.img.RecyclableSubImage(b)
				f.drawTrianglesShader(stencilBufferImage, vs, is, shader, stencilOp)
				stencilBufferImage.Recycle()
			}
		}
	}

	// Render the stencil buffers with the specified colors.
	var coverShader *ggfx.Shader
	switch f.fillRule {
	case FillRuleNonZero:
		coverShader, err = ensureStencilBufferNonZeroShader(f.antialias)
		if err != nil {
			panic(fmt.Sprintf("vector: failed to create stencil buffer non-zero shader: %v", err))
		}
	case FillRuleEvenOdd:
		coverShader, err = ensureStencilBufferEvenOddShader(f.antialias)
		if err != nil {
			panic(fmt.Sprintf("vector: failed to create stencil buffer even-odd shader: %v", err))
		}
	}
	coverOp := &ggfx.DrawTrianglesShaderOptions{}
	coverOp.Blend = f.blend

	// The paths are drawn in one call while their stencil images fit in the call's sources. The
	// cover shaders read the stencil buffers at absolute positions on the source a vertex names, so
	// the whole images work as the sources: the cache's pages and the atlas's images are unmanaged,
	// so a position on an image is the position on its texture. A path drawn to a sub-image of dst
	// has its quad cut to the sub-image's bounds: the edges are on whole pixels, so the cut quad
	// covers the pixels the sub-image's scissor would let the whole quad cover, with the same
	// values, whatever the blend.
	var sources int
	vs, is = vs[:0], is[:0]
	for i := range f.paths {
		s := &f.slots[i]
		atlasImage := s.img
		if atlasImage == nil {
			continue
		}
		srcRegion := s.buffer(f.antialias, 0)
		var offsetX, offsetY float32
		if f.antialias {
			srcRegion1 := s.buffer(f.antialias, 1)
			offsetX = float32(srcRegion1.Min.X - srcRegion.Min.X)
			offsetY = float32(srcRegion1.Min.Y - srcRegion.Min.Y)
		}

		if !disableBatchingForTesting {
			source := slices.Index(coverOp.Images[:sources], atlasImage)
			if source < 0 {
				if sources == len(coverOp.Images) {
					f.drawTrianglesShader(dst, vs, is, coverShader, coverOp)
					vs, is = vs[:0], is[:0]
					coverOp.Images = [len(coverOp.Images)]*ggfx.Image{}
					sources = 0
				}
				source = sources
				coverOp.Images[source] = atlasImage
				sources++
			}
			vs, is = f.appendCoverQuad(vs, is, dst, i, srcRegion, offsetX, offsetY, source, f.bounds[i])
			continue
		}

		// The reference the batch is tested against: the path alone, cut by a sub-image's scissor.
		op := &ggfx.DrawTrianglesShaderOptions{}
		op.Blend = f.blend
		op.Images[0] = atlasImage.SubImage(srcRegion).(*ggfx.Image)
		vs, is = f.appendCoverQuad(vs[:0], is[:0], dst, i, srcRegion, offsetX, offsetY, 0, dst.Bounds())
		dst2 := dst
		if dst.Bounds() != f.bounds[i] {
			dst2 = dst.RecyclableSubImage(f.bounds[i])
		}
		f.drawTrianglesShader(dst2, vs, is, coverShader, op)
		if dst2 != dst {
			dst2.Recycle()
		}
		vs, is = vs[:0], is[:0]
	}
	f.drawTrianglesShader(dst, vs, is, coverShader, coverOp)
	clear(f.slots)
	noteRenderTarget(dst)
	theRenderTarget = nil
}

// placeStencilBuffers places the stencil buffers of the paths: in the stencil cache, and on
// theAtlas for the paths the cache cannot hold, or for all of them while testing without the cache.
func (f *fillPathsState) placeStencilBuffers(dstBounds image.Rectangle) {
	now := render.Tick()
	n := len(f.paths)
	theRenderingBounds, theContained = setRenderingBounds(theRenderingBounds, theContained, dstBounds, f.paths, f.bounds)
	f.slots = slices.Grow(f.slots[:0], n)[:n]
	clear(f.slots)
	if disableStencilCacheForTesting {
		for i := range f.slots {
			f.slots[i].transient = true
		}
	} else {
		theStencilCache.place(now, f.paths, theRenderingBounds, f.antialias, f.slots)
	}
	theAtlas.layout(now, dstBounds, theRenderingBounds, f.antialias, f.slots)
	for i := range f.slots {
		s := &f.slots[i]
		if !s.transient {
			continue
		}
		img, b, ok := theAtlas.regionAt(i)
		if !ok {
			continue
		}
		s.img, s.region, s.draw = img, b, true
	}
}

// theVertices and theIndices are the buffers fillPaths reuses. A batch holds the triangles of all the
// paths, so the buffers are shared by all the states rather than grown again for each state taken
// from the pool. They are protected by theFillPathM.
var (
	theVertices []ggfx.Vertex
	theIndices  []uint32
	// theStencilImages are the images a flush draws stencil buffers to.
	theStencilImages []*ggfx.Image
	// theRenderingBounds are the rendering bounds of the paths of a flush, and theContained whether
	// each path lies in them.
	theRenderingBounds []image.Rectangle
	theContained       []bool
)

// disableBatchingForTesting makes fillPaths draw every path one by one, as it does a clipped path.
var disableBatchingForTesting bool

// theDrawCallCountForTesting counts the draw calls fillPaths makes. It is protected by theFillPathM.
var theDrawCallCountForTesting int

// batchable reports whether path i can be rendered to the whole atlas image with other paths.
func (f *fillPathsState) batchable(i int) bool {
	return theContained[i] && !disableBatchingForTesting
}

// drawTrianglesShader draws the triangles to dst, if any.
func (f *fillPathsState) drawTrianglesShader(dst *ggfx.Image, vs []ggfx.Vertex, is []uint32, shader *ggfx.Shader, op *ggfx.DrawTrianglesShaderOptions) {
	if len(is) == 0 {
		return
	}
	theDrawCallCountForTesting++
	noteRenderTarget(dst)
	dst.DrawTrianglesShader(vs, is, shader, op)
}

// theRenderTarget is the image fillPaths last drew to, and theRenderTargetSwitchCountForTesting
// counts how many times it changed, from the destination, which the draw making fillPaths run is
// to, back to it. theRenderTarget is nil out of fillPaths, so as not to keep an image alive. They
// are protected by theFillPathM.
var (
	theRenderTarget                      *ggfx.Image
	theRenderTargetSwitchCountForTesting int
)

// clearStencilImage clears img, a stencil image or a sub-image of one. Every clear and draw of
// fillPaths goes through it or drawTrianglesShader, which note the render target.
func clearStencilImage(img *ggfx.Image) {
	noteRenderTarget(img)
	img.Clear()
}

// noteRenderTarget notes a draw of fillPaths to img, or to the image it is a sub-image of, for
// testing.
func noteRenderTarget(img *ggfx.Image) {
	if theRenderTarget == nil {
		return
	}
	if img = originalImage(img); img != theRenderTarget {
		theRenderTargetSwitchCountForTesting++
		theRenderTarget = img
	}
}

// hasQuadTo reports whether p has a quadratic curve, without which the bezier pass draws nothing.
func (p *Path) hasQuadTo() bool {
	for i := range p.subPaths {
		for _, op := range p.subPaths[i].ops {
			if op.typ == opTypeQuadTo {
				return true
			}
		}
	}
	return false
}

// appendStencilTriangles appends the triangles of path for the sample oac, moved from the path's
// rendering position pp to the stencil buffer's position origin. If bezier is false, they are the
// triangles that fill the polygons of path roughly. If bezier is true, they are the triangles of
// the bezier curves for the Loop-Blinn algorithm.
//
// A vertex is computed from the point's offset from pp, the stencil cache's key, so the vertices
// of the paths with the same key are the same wherever the paths are.
func appendStencilTriangles(vs []ggfx.Vertex, is []uint32, path *Path, oac offsetAndColor, pp, origin image.Point, bezier bool) ([]ggfx.Vertex, []uint32) {
	px, py := float32(pp.X), float32(pp.Y)
	ox, oy := float32(origin.X), float32(origin.Y)
	vertex := func(p point) ggfx.Vertex {
		return ggfx.Vertex{
			DstX:   (p.x - px) + oac.offsetX + ox,
			DstY:   (p.y - py) + oac.offsetY + oy,
			ColorR: oac.colorR,
			ColorG: oac.colorG,
			ColorB: oac.colorB,
			ColorA: oac.colorA,
		}
	}

	for i := range path.subPaths {
		subPath := &path.subPaths[i]
		if !subPath.isValid() {
			continue
		}

		cur := subPath.start
		if bezier {
			for _, op := range subPath.ops {
				switch op.typ {
				case opTypeLineTo:
					cur = op.p1
				case opTypeQuadTo:
					idx := uint32(len(vs))
					v0, v1, v2 := vertex(cur), vertex(op.p1), vertex(op.p2)
					// u and v for the Loop-Blinn algorithm.
					v1.Custom0 = 0.5
					v2.Custom0, v2.Custom1 = 1, 1
					vs = append(vs, v0, v1, v2)
					is = append(is, idx, idx+1, idx+2)
					cur = op.p2
				}
			}
			continue
		}

		// A fan of triangles from an origin point to each segment. Any origin works in theory. The
		// sub-path's start point is close to the other points, which keeps the triangles' overlaps
		// small, and it is one of the points, so the triangles of the segments touching it, the
		// first one and the closing one, have no area and are left out. Each point is one vertex,
		// shared by the triangles of the segments it ends and starts.
		// TODO: Use a better position like the center of the sub-path.
		if len(subPath.ops) < 2 {
			continue
		}
		originIdx := uint32(len(vs))
		vs = append(vs, vertex(cur))
		for k := range subPath.ops {
			vs = append(vs, vertex(subPath.endAtOp(k)))
			if k > 0 {
				// The winding is the same as the segment's, (cur, origin, next), which the
				// stencil shader counts by.
				idx := originIdx + uint32(k)
				is = append(is, idx, originIdx, idx+1)
			}
		}
	}
	return vs, is
}

// appendCoverQuad appends the quad that renders the stencil buffer of path i, at srcRegion on its
// stencil image, to dst with the path's color, cut to clip. (offsetX, offsetY) is the offset to the
// second stencil buffer for antialiasing, and source the index of the stencil image in the draw's
// images.
func (f *fillPathsState) appendCoverQuad(vs []ggfx.Vertex, is []uint32, dst *ggfx.Image, i int, srcRegion image.Rectangle, offsetX, offsetY float32, source int, clip image.Rectangle) ([]ggfx.Vertex, []uint32) {
	pp := theRenderingBounds[i].Min
	dstOffsetX := max(0, dst.Bounds().Min.X-pp.X)
	dstOffsetY := max(0, dst.Bounds().Min.Y-pp.Y)
	d := image.Rectangle{Min: pp.Add(image.Pt(dstOffsetX, dstOffsetY))}
	d.Max = d.Min.Add(srcRegion.Size())
	c := d.Intersect(clip)
	if c.Empty() {
		return vs, is
	}
	// The source moves with the cut edges, so that every pixel samples what it did.
	s := image.Rectangle{Min: srcRegion.Min.Add(c.Min.Sub(d.Min)), Max: srcRegion.Max.Sub(d.Max.Sub(c.Max))}
	clr := f.colors[i]
	vertex := func(dx, dy int, sx, sy int) ggfx.Vertex {
		return ggfx.Vertex{
			DstX:    float32(dx),
			DstY:    float32(dy),
			SrcX:    float32(sx),
			SrcY:    float32(sy),
			ColorR:  clr.R(),
			ColorG:  clr.G(),
			ColorB:  clr.B(),
			ColorA:  clr.A(),
			Custom0: offsetX,
			Custom1: offsetY,
			Custom2: float32(source),
		}
	}
	idx := uint32(len(vs))
	vs = append(vs,
		vertex(c.Min.X, c.Min.Y, s.Min.X, s.Min.Y),
		vertex(c.Max.X, c.Min.Y, s.Max.X, s.Min.Y),
		vertex(c.Min.X, c.Max.Y, s.Min.X, s.Max.Y),
		vertex(c.Max.X, c.Max.Y, s.Max.X, s.Max.Y))
	is = append(is, idx, idx+1, idx+2, idx+1, idx+2, idx+3)
	return vs, is
}

// StencilBytes reports the size in bytes of the GPU images FillPath and the other fills keep as
// stencil buffers. They are sized to what the fills of the last 60 to 120 frames needed.
func StencilBytes() int64 {
	return theAtlas.bytes.Load() + theStencilCache.bytes.Load()
}
