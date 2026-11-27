// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"unsafe"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// D-P7: verify struct sizes stay compact after moving rarely-used fields behind pointers.
func TestStructSizes(t *testing.T) {
	elemSize := unsafe.Sizeof(element{})
	patchSize := unsafe.Sizeof(stylePatch{})
	svgPathSize := unsafe.Sizeof(SvgPath{})
	pathStyleSize := unsafe.Sizeof(PathStyle{})
	t.Logf("sizeof(element)=%d sizeof(stylePatch)=%d sizeof(SvgPath)=%d sizeof(PathStyle)=%d",
		elemSize, patchSize, svgPathSize, pathStyleSize)

	if elemSize > 256 {
		t.Errorf("sizeof(element) = %d; want <= 256", elemSize)
	}
	if patchSize > 200 {
		t.Errorf("sizeof(stylePatch) = %d; want <= 200", patchSize)
	}
}

// D-P6: verify currentColor gradient cache hits across multiple shapes in one draw.
func TestCurrentColorGradCacheHits(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><linearGradient id="g"><stop offset="0" stop-color="currentColor"/><stop offset="1" stop-color="#00f"/></linearGradient></defs>`)
	for i := range 50 {
		sb.WriteString(`<rect x="0" y="0" width="8" height="8" fill="url(#g)"/>`)
		_ = i
	}
	sb.WriteString(`</svg>`)
	icon, err := ReadIconStream(strings.NewReader(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
	rend := icon.NewRenderer()
	defer rend.Release()
	opts := &EvalOptions{CurrentColor: color.NRGBA{0xff, 0, 0, 0xff}}
	// Warm up painter caches and frame slices.
	rend.Draw(dst, rasterx.Identity, opts)

	allocs := testing.AllocsPerRun(50, func() {
		rend.Draw(dst, rasterx.Identity, opts)
	})
	if allocs > 5 {
		t.Errorf("currentColor gradient 50-rect draw did %.1f allocs/run; want <= 5", allocs)
	}
}

// D-P8: verify animated `d` keyframes are compiled once and cached across frames.
func TestAnimatedDPathMemoized(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
<path d="M0 0 L50 50 L100 0" fill="none" stroke="#000">
  <animate attributeName="d" values="M0 0 L50 50 L100 0;M0 100 L50 50 L100 100" dur="2s" repeatCount="indefinite"/>
</path>
</svg>`
	icon, err := ReadIconStream(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
	rend := icon.NewRenderer()
	defer rend.Release()
	opts := &EvalOptions{Animate: true, Time: 1.5}
	// Warm up cache on keyframe 2.
	rend.Draw(dst, rasterx.Identity, opts)

	allocs := testing.AllocsPerRun(50, func() {
		rend.Draw(dst, rasterx.Identity, opts)
	})
	if allocs > 2 {
		t.Errorf("animated d redraw did %.1f allocs/run; want <= 2", allocs)
	}
}

// Item 10: animated-d pathCache bounds both entry count and retained bytes,
// clearing on full so later keys remain cacheable.
func TestPathCacheByteAndEntryBound(t *testing.T) {
	sc := &scene{}
	for i := range maxPathCacheEntries + 50 {
		d := fmt.Sprintf("M0 0 L%d %d", i+1, i+2)
		if p := sc.compiledPath(d); len(p) == 0 {
			t.Fatalf("compiledPath(%q) returned empty", d)
		}
		if len(sc.pathCache) > maxPathCacheEntries {
			t.Fatalf("pathCache entries = %d > %d", len(sc.pathCache), maxPathCacheEntries)
		}
		if sc.pathCacheBytes > maxPathCacheBytes {
			t.Fatalf("pathCacheBytes = %d > %d", sc.pathCacheBytes, maxPathCacheBytes)
		}
	}
	// A key added after clearing must be present in the cache.
	lateKey := fmt.Sprintf("M0 0 L%d %d", maxPathCacheEntries+49, maxPathCacheEntries+50)
	if _, ok := sc.pathCache[lateKey]; !ok {
		t.Errorf("late key %q was not cached after clear-on-full", lateKey)
	}
}
