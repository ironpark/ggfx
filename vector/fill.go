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
	_ "unsafe"
	"weak"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/pool"
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
	// theCallbackTokens and theFillPathsStates are keyed by weak pointers not to keep the destination images alive.
	// When a destination image is collected before being used again, releaseFillPathsState removes the entries.
	theCallbackTokens      = map[weak.Pointer[ggfx.Image]]int64{}
	theFillPathsStates     = map[weak.Pointer[ggfx.Image]]*fillPathsState{}
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

	// Remove the previous registered callbacks.
	if token, ok := theCallbackTokens[key]; ok {
		removeUsageCallback(dst, token)
	}
	delete(theCallbackTokens, key)

	s, ok := theFillPathsStates[key]
	if !ok {
		s = theFillPathsStatesPool.Get()
		theFillPathsStates[key] = s
		s.cleanup = runtime.AddCleanup(dst, releaseFillPathsState, key)
	}
	if s.antialias != drawPathOptions.AntiAlias || s.blend != drawPathOptions.Blend || s.fillRule != fillOptions.FillRule {
		s.fillPaths(dst)
		s.reset()
	}
	s.antialias = drawPathOptions.AntiAlias
	s.blend = drawPathOptions.Blend
	s.fillRule = fillOptions.FillRule
	s.addPath(path, bounds, drawPathOptions.ColorScale)

	// Use an independent callback function to avoid unexpected captures.
	theCallbackTokens[key] = addUsageCallback(dst, fillPathCallback)
}

func fillPathCallback(dst *ggfx.Image) {
	if originalImage(dst) != dst {
		panic("vector: dst must be the original image")
	}

	theFillPathM.Lock()
	defer theFillPathM.Unlock()

	key := weak.Make(dst)

	// Remove the callback not to call this twice.
	if token, ok := theCallbackTokens[key]; ok {
		removeUsageCallback(dst, token)
	}
	delete(theCallbackTokens, key)

	s, ok := theFillPathsStates[key]
	if !ok {
		panic("vector: fillPathsState must exist here")
	}
	s.fillPaths(dst)
	s.reset()
	delete(theFillPathsStates, key)
	s.cleanup.Stop()
	s.cleanup = runtime.Cleanup{}
	theFillPathsStatesPool.Put(s)
}

// releaseFillPathsState discards the state for a destination image that was collected before being used again.
func releaseFillPathsState(key weak.Pointer[ggfx.Image]) {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()

	// The destination image is already collected, and its usage callbacks are gone with it.
	delete(theCallbackTokens, key)

	s, ok := theFillPathsStates[key]
	if !ok {
		return
	}
	delete(theFillPathsStates, key)
	s.cleanup = runtime.Cleanup{}
	s.reset()
	theFillPathsStatesPool.Put(s)
}

//go:linkname originalImage github.com/ironpark/ggfx.originalImage
func originalImage(img *ggfx.Image) *ggfx.Image

//go:linkname addUsageCallback github.com/ironpark/ggfx.addUsageCallback
func addUsageCallback(img *ggfx.Image, fn func(img *ggfx.Image)) int64

//go:linkname removeUsageCallback github.com/ironpark/ggfx.removeUsageCallback
func removeUsageCallback(img *ggfx.Image, token int64)

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

	antialias bool
	blend     ggfx.Blend
	fillRule  FillRule

	// cleanup removes the entries for the destination image from theCallbackTokens and theFillPathsStates
	// when the image is collected.
	cleanup runtime.Cleanup
}

func (f *fillPathsState) reset() {
	for _, p := range f.paths {
		p.Reset()
	}
	f.paths = f.paths[:0]
	f.bounds = f.bounds[:0]
	f.colors = slices.Delete(f.colors, 0, len(f.colors))
}

func (f *fillPathsState) addPath(path *Path, bounds image.Rectangle, clr ggfx.ColorScale) {
	if path == nil {
		return
	}

	f.paths = slices.Grow(f.paths, 1)[:len(f.paths)+1]
	if f.paths[len(f.paths)-1] == nil {
		f.paths[len(f.paths)-1] = &Path{}
	}
	dst := f.paths[len(f.paths)-1]
	dst.addSubPaths(len(path.subPaths))
	for i, subPath := range path.subPaths {
		dst.subPaths[i].start = subPath.start
		dst.subPaths[i].closed = subPath.closed
		dst.subPaths[i].invalid = subPath.invalid
		dst.subPaths[i].ops = slices.Grow(dst.subPaths[i].ops, len(subPath.ops))[:len(subPath.ops)]
		copy(dst.subPaths[i].ops, subPath.ops)
	}
	f.bounds = append(f.bounds, bounds)
	f.colors = append(f.colors, clr)
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

	theAtlas.setPaths(dst.Bounds(), f.paths, f.bounds, f.antialias)

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

	// First, render the polygons roughly, and second, the bezier curves, to the stencil buffers.
	// The blending is additive, so the order of the triangles does not matter.
	//
	// A contained path's points are in its stencil buffer, and a sample offset moves them less than
	// half a pixel, so its triangles cover no pixel center of another stencil buffer. The bezier
	// triangles' control points may be farther out, but the bezier shader adds zero outside the
	// curve, which is in the path's bounds. So the contained paths on an atlas image are rendered to
	// the whole atlas image in one call, as if each were clipped by its stencil buffer. The other
	// paths are clipped by their stencil buffers' bounds one by one.
	stencilOp := &ggfx.DrawTrianglesShaderOptions{}
	stencilOp.Blend = ggfx.BlendLighter
	for _, bezier := range []bool{false, true} {
		shader := fillShader
		if bezier {
			shader = bezierShader
		}

		for imageIndex, atlasImage := range theAtlas.atlasImages {
			vs, is = vs[:0], is[:0]
			for i, path := range f.paths {
				if path == nil || !f.batchable(i) || theAtlas.atlasImageIndexAt(i) != imageIndex {
					continue
				}
				for _, oac := range offsetAndColors {
					_, b, ok := theAtlas.stencilBufferRegionAt(i, f.antialias, oac.imageIndex)
					if !ok {
						continue
					}
					dx, dy := stencilOffset(dst, i, b)
					vs, is = appendStencilTriangles(vs, is, path, oac, dx, dy, bezier)
				}
			}
			f.drawTrianglesShader(atlasImage, vs, is, shader, stencilOp)
		}

		for i, path := range f.paths {
			if path == nil || f.batchable(i) {
				continue
			}
			for _, oac := range offsetAndColors {
				stencilBufferImage := theAtlas.stencilBufferImageAt(i, f.antialias, oac.imageIndex)
				if stencilBufferImage == nil {
					continue
				}
				dx, dy := stencilOffset(dst, i, stencilBufferImage.Bounds())
				vs, is = appendStencilTriangles(vs[:0], is[:0], path, oac, dx, dy, bezier)
				f.drawTrianglesShader(stencilBufferImage, vs, is, shader, stencilOp)
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

	// The paths drawn to dst itself are drawn in one call while they share an atlas image. The cover
	// shaders read the stencil buffers at absolute positions, so the whole atlas image works as the
	// source. A path drawn to a sub-image of dst ends the batch, which keeps the paths in order.
	vs, is = vs[:0], is[:0]
	for i, path := range f.paths {
		if path == nil {
			continue
		}
		atlasImage, srcRegion, ok := theAtlas.stencilBufferRegionAt(i, f.antialias, 0)
		if !ok {
			continue
		}
		var offsetX, offsetY float32
		if f.antialias {
			_, srcRegion1, _ := theAtlas.stencilBufferRegionAt(i, f.antialias, 1)
			offsetX = float32(srcRegion1.Min.X - srcRegion.Min.X)
			offsetY = float32(srcRegion1.Min.Y - srcRegion.Min.Y)
		}

		if f.bounds[i] == dst.Bounds() && !disableBatchingForTesting {
			if coverOp.Images[0] != atlasImage {
				f.drawTrianglesShader(dst, vs, is, coverShader, coverOp)
				vs, is = vs[:0], is[:0]
				coverOp.Images[0] = atlasImage
			}
			vs, is = f.appendCoverQuad(vs, is, dst, i, srcRegion, offsetX, offsetY)
			continue
		}

		f.drawTrianglesShader(dst, vs, is, coverShader, coverOp)
		coverOp.Images[0] = nil

		op := &ggfx.DrawTrianglesShaderOptions{}
		op.Blend = f.blend
		op.Images[0] = atlasImage.SubImage(srcRegion).(*ggfx.Image)
		vs, is = f.appendCoverQuad(vs[:0], is[:0], dst, i, srcRegion, offsetX, offsetY)
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
}

// theVertices and theIndices are the buffers fillPaths reuses. A batch holds the triangles of all the
// paths, so the buffers are shared by all the states rather than grown again for each state taken
// from the pool. They are protected by theFillPathM.
var (
	theVertices []ggfx.Vertex
	theIndices  []uint32
)

// disableBatchingForTesting makes fillPaths draw every path one by one, as it does a clipped path.
var disableBatchingForTesting bool

// theDrawCallCountForTesting counts the draw calls fillPaths makes. It is protected by theFillPathM.
var theDrawCallCountForTesting int

// batchable reports whether path i can be rendered to the whole atlas image with other paths.
func (f *fillPathsState) batchable(i int) bool {
	return theAtlas.isContained(i) && !disableBatchingForTesting
}

// drawTrianglesShader draws the triangles to dst, if any.
func (f *fillPathsState) drawTrianglesShader(dst *ggfx.Image, vs []ggfx.Vertex, is []uint32, shader *ggfx.Shader, op *ggfx.DrawTrianglesShaderOptions) {
	if len(is) == 0 {
		return
	}
	theDrawCallCountForTesting++
	dst.DrawTrianglesShader(vs, is, shader, op)
}

// stencilOffset returns the translation from the destination's coordinates of path i to its stencil
// buffer, whose bounds on the atlas image are b.
func stencilOffset(dst *ggfx.Image, i int, b image.Rectangle) (float32, float32) {
	pp := theAtlas.pathRenderingPositionAt(i)
	dx := float32(-pp.X + b.Min.X - max(0, dst.Bounds().Min.X-pp.X))
	dy := float32(-pp.Y + b.Min.Y - max(0, dst.Bounds().Min.Y-pp.Y))
	return dx, dy
}

// appendStencilTriangles appends the triangles of path for the sample oac, translated by (dx, dy).
// If bezier is false, they are the triangles that fill the polygons of path roughly. If bezier is
// true, they are the triangles of the bezier curves for the Loop-Blinn algorithm.
func appendStencilTriangles(vs []ggfx.Vertex, is []uint32, path *Path, oac offsetAndColor, dx, dy float32, bezier bool) ([]ggfx.Vertex, []uint32) {
	vertex := func(p point) ggfx.Vertex {
		return ggfx.Vertex{
			DstX:   p.x + oac.offsetX + dx,
			DstY:   p.y + oac.offsetY + dy,
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

		// Add an origin point. Any position works in theory.
		// Use the sub-path's start point. Using one of the sub-path's points can reduce triangles.
		// Also, this point should be close to the other points and then triangle overlaps are reduced.
		// TODO: Use a better position like the center of the sub-path.
		originIdx := uint32(len(vs))
		vs = append(vs, vertex(cur))

		for _, op := range subPath.ops {
			var next point
			switch op.typ {
			case opTypeLineTo:
				next = op.p1
			case opTypeQuadTo:
				next = op.p2
			}
			idx := uint32(len(vs))
			vs = append(vs, vertex(cur), vertex(next))
			is = append(is, idx, originIdx, idx+1)
			cur = next
		}
		// If the sub-path is not closed, add a supplementary line.
		if !subPath.closed {
			idx := uint32(len(vs))
			vs = append(vs, vertex(cur), vertex(subPath.start))
			is = append(is, idx, originIdx, idx+1)
		}
	}
	return vs, is
}

// appendCoverQuad appends the quad that renders the stencil buffer of path i, at srcRegion on its
// atlas image, to dst with the path's color. (offsetX, offsetY) is the offset to the second stencil
// buffer for antialiasing.
func (f *fillPathsState) appendCoverQuad(vs []ggfx.Vertex, is []uint32, dst *ggfx.Image, i int, srcRegion image.Rectangle, offsetX, offsetY float32) ([]ggfx.Vertex, []uint32) {
	pp := theAtlas.pathRenderingPositionAt(i)
	dstOffsetX := max(0, dst.Bounds().Min.X-pp.X)
	dstOffsetY := max(0, dst.Bounds().Min.Y-pp.Y)
	x0 := float32(pp.X + dstOffsetX)
	y0 := float32(pp.Y + dstOffsetY)
	x1 := float32(pp.X + srcRegion.Dx() + dstOffsetX)
	y1 := float32(pp.Y + srcRegion.Dy() + dstOffsetY)
	clr := f.colors[i]
	vertex := func(dx, dy float32, sx, sy int) ggfx.Vertex {
		return ggfx.Vertex{
			DstX:    dx,
			DstY:    dy,
			SrcX:    float32(sx),
			SrcY:    float32(sy),
			ColorR:  clr.R(),
			ColorG:  clr.G(),
			ColorB:  clr.B(),
			ColorA:  clr.A(),
			Custom0: offsetX,
			Custom1: offsetY,
		}
	}
	idx := uint32(len(vs))
	vs = append(vs,
		vertex(x0, y0, srcRegion.Min.X, srcRegion.Min.Y),
		vertex(x1, y0, srcRegion.Max.X, srcRegion.Min.Y),
		vertex(x0, y1, srcRegion.Min.X, srcRegion.Max.Y),
		vertex(x1, y1, srcRegion.Max.X, srcRegion.Max.Y))
	is = append(is, idx, idx+1, idx+2, idx+1, idx+2, idx+3)
	return vs, is
}
