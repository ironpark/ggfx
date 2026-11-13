// Copyright 2024 The Ebitengine Authors
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
	"strings"

	"github.com/ironpark/ggfx/internal/recent"
	"github.com/ironpark/ggfx/internal/render"
)

type Point struct {
	X, Y float32
}

func (p Point) String() string {
	return fmt.Sprintf("(%f, %f)", p.X, p.Y)
}

func IsPointCloseToSegment(p, p0, p1 Point, allow float32) bool {
	return isPointCloseToSegment(point{
		x: p.X,
		y: p.Y,
	}, point{
		x: p0.X,
		y: p0.Y,
	}, point{
		x: p1.X,
		y: p1.Y,
	}, allow)
}

func CurrentPosition(path *Path) (Point, bool) {
	p, ok := path.currentPosition()
	if !ok {
		return Point{}, false
	}
	return Point{X: p.x, Y: p.y}, true
}

func SubPathCount(path *Path) int {
	return len(path.subPaths)
}

// PathOperationsString returns a string representation of the operations in the path.
func PathOperationsString(path *Path) string {
	var sb strings.Builder
	for _, subPath := range path.subPaths {
		sb.WriteString(fmt.Sprintf("MoveTo(%v, %v)\n", subPath.start.x, subPath.start.y))
		for _, o := range subPath.ops {
			switch o.typ {
			case opTypeLineTo:
				sb.WriteString(fmt.Sprintf("LineTo(%v, %v)\n", o.p1.x, o.p1.y))
			case opTypeQuadTo:
				sb.WriteString(fmt.Sprintf("QuadTo(%v, %v, %v, %v)\n", o.p1.x, o.p1.y, o.p2.x, o.p2.y))
			}
		}
		if subPath.closed {
			sb.WriteString("Close()\n")
		}
	}
	return sb.String()
}

func FillPathsStateCount() int {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()
	return len(theFillPathsStates)
}

// CallbackTokenCount returns the number of destinations with a usage callback, which are the ones
// with pending paths.
func CallbackTokenCount() int {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()
	var n int
	for _, d := range theFillPathsStates {
		if d.state != nil {
			n++
		}
	}
	return n
}

func CircleVertexCount(radius float32) int {
	return circleVertexCount(radius)
}

// SetBatchingDisabled makes the paths drawn one by one, as clipped paths are, if disabled is true.
func SetBatchingDisabled(disabled bool) {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()
	disableBatchingForTesting = disabled
}

// DrawCallCount returns the number of draw calls fillPaths has made.
func DrawCallCount() int {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()
	return theDrawCallCountForTesting
}

// AtlasRegion is a path's region in the stencil atlas.
type AtlasRegion struct {
	ImageIndex int
	// Region is the path's region on its atlas image. StencilBuffers are the stencil buffers in
	// it; there are two for antialiasing.
	Region         image.Rectangle
	StencilBuffers []image.Rectangle
	Contained      bool
}

// setPaths lays out paths in the atlas at time now, in frames, and returns whether each path lies
// in its rendering bounds, and the bounds.
func (a *atlas) setPaths(now int64, dstBounds image.Rectangle, paths []*Path, bounds []image.Rectangle, antialias bool) (rbs []image.Rectangle, contained []bool) {
	rbs, contained = setRenderingBounds(nil, nil, dstBounds, paths, bounds)
	a.layout(now, dstBounds, rbs, antialias, nil)
	return rbs, contained
}

// AtlasRegions lays out paths in a fresh atlas and returns their regions, in the order of paths.
// A path without a region has a zero AtlasRegion.
func AtlasRegions(dstBounds image.Rectangle, paths []*Path, bounds []image.Rectangle, antialias bool) []AtlasRegion {
	var a atlas
	_, contained := a.setPaths(0, dstBounds, paths, bounds, antialias)
	defer a.dispose()
	regions := make([]AtlasRegion, len(paths))
	for i := range paths {
		ar := a.atlasRegions[a.pathIndexToAtlasRegionIndex[i]]
		if ar.imageBounds.Empty() {
			continue
		}
		r := AtlasRegion{
			ImageIndex: ar.imageIndex,
			Region:     ar.imageBounds,
			Contained:  contained[i],
		}
		n := 1
		if antialias {
			n = 2
		}
		for k := range n {
			img := a.atlasImages[ar.imageIndex]
			b := stencilBuffer(ar.imageBounds, antialias, k)
			if !b.In(img.Bounds()) {
				panic(fmt.Sprintf("vector: a stencil buffer %v is out of its atlas image %v", b, img.Bounds()))
			}
			r.StencilBuffers = append(r.StencilBuffers, b)
		}
		regions[i] = r
	}
	return regions
}

// PathRenderingBounds returns the bounds in which a path is rendered, as the atlas computes them.
func PathRenderingBounds(dstBounds image.Rectangle, path *Path, bounds image.Rectangle) image.Rectangle {
	var a atlas
	rbs, _ := a.setPaths(0, dstBounds, []*Path{path}, []image.Rectangle{bounds}, false)
	defer a.dispose()
	return rbs[0]
}

// AtlasRecentWindow is how many frames a stencil atlas's recent peak covers at least.
const AtlasRecentWindow = recent.Window

// StencilCacheMaxBytes bounds the size of the stencil cache's pages.
const StencilCacheMaxBytes = stencilCacheMaxBytes

// StencilAtlas is a stencil atlas with its own clock, in frames, that the test advances.
type StencilAtlas struct {
	a   atlas
	now int64
}

func NewStencilAtlas() *StencilAtlas {
	return &StencilAtlas{}
}

// Advance makes n frames go by.
func (s *StencilAtlas) Advance(n int64) { s.now += n }

func (s *StencilAtlas) SetPaths(dstBounds image.Rectangle, paths []*Path, bounds []image.Rectangle, antialias bool) {
	s.a.setPaths(s.now, dstBounds, paths, bounds, antialias)
}

// NextFrame ends the frame the test runs in and begins another, which advances the shared
// stencil atlas's clock.
func NextFrame() error {
	if err := render.EndFrame(); err != nil {
		return err
	}
	if err := render.Flush(false); err != nil {
		return err
	}
	return render.BeginFrame()
}

// ImageSizes returns the sizes of the atlas images kept, with a zero size for one not allocated.
func (s *StencilAtlas) ImageSizes() []image.Point {
	var sizes []image.Point
	for _, img := range s.a.images {
		var p image.Point
		if img != nil {
			p = img.Bounds().Size()
		}
		sizes = append(sizes, p)
	}
	return sizes
}

// ActiveImageCount returns how many atlas images the current paths are on.
func (s *StencilAtlas) ActiveImageCount() int { return len(s.a.atlasImages) }

// Allocations returns how many atlas images were allocated.
func (s *StencilAtlas) Allocations() int { return s.a.allocations }

func (s *StencilAtlas) Bytes() int64 { return s.a.bytes.Load() }

func (s *StencilAtlas) Dispose() { s.a.dispose() }

func RoundUpAtlasSize(size int) int {
	return roundUpAtlasSize(size)
}

// SetStencilCacheDisabled makes every path's stencil buffers drawn anew on the transient atlas if
// disabled is true, as before the stencil cache.
func SetStencilCacheDisabled(disabled bool) {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()
	disableStencilCacheForTesting = disabled
}

// ResetStencilCache empties the stencil cache and deallocates its pages.
func ResetStencilCache() {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()
	theStencilCache.dispose()
}

// RenderTargetSwitchCount returns how many times fillPaths has changed the render target, from the
// destination back to it.
func RenderTargetSwitchCount() int {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()
	return theRenderTargetSwitchCountForTesting
}

// StencilCachePageSizes returns the sizes of the stencil cache's pages.
func StencilCachePageSizes() []image.Point {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()
	var sizes []image.Point
	for _, pg := range theStencilCache.pages {
		sizes = append(sizes, pg.size)
	}
	return sizes
}

// SetStencilCacheRedrawn makes the cached stencil buffers cleared and drawn again where they are if
// redrawn is true, which must render the same pixels as reusing them.
func SetStencilCacheRedrawn(redrawn bool) {
	theFillPathM.Lock()
	defer theFillPathM.Unlock()
	redrawStencilCacheForTesting = redrawn
}
