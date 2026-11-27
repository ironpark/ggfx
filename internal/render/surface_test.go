// Copyright 2026 The ggfx Authors
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
	"errors"
	"image"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

type countingScreenSurface struct {
	mu    sync.Mutex
	calls []image.Point
}

func (s *countingScreenSurface) NewScreenImage(width, height int) (graphicsdriver.Image, error) {
	s.mu.Lock()
	s.calls = append(s.calls, image.Pt(width, height))
	s.mu.Unlock()
	return driver.NewImage(width, height, graphicsdriver.PixelFormatRGBA8)
}

func (s *countingScreenSurface) FlipsY() bool { return false }

func (s *countingScreenSurface) Readable() bool { return true }

func (s *countingScreenSurface) Dispose() {}

func (s *countingScreenSurface) snapshotCalls() []image.Point {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]image.Point, len(s.calls))
	copy(out, s.calls)
	return out
}

// A screen image replaced by a resize or surface disposal is terminal: subsequent draws, writes,
// or reads on it (or using it as a source) are no-ops and never re-create a screen backend at the
// old size.
func TestDisposedScreenNeverReallocates(t *testing.T) {
	surf := &countingScreenSurface{}
	s := &Surface{surface: surf}

	oldScreen := s.Screen(64, 64)
	oldScreen.Fill(1, 0, 0, 1, image.Rect(0, 0, 64, 64))

	// Flush so the initial screen allocation runs on the render thread.
	if err := graphicscommand.FlushCommands(driver, graphicsdriver.FlushModeIntermediate); err != nil {
		t.Fatal(err)
	}
	if calls := surf.snapshotCalls(); len(calls) != 1 || calls[0] != image.Pt(64, 64) {
		t.Fatalf("after initial Screen(64, 64): NewScreenImage calls = %v, want [(64,64)]", calls)
	}

	// Resize replaces oldScreen and permanently disposes it.
	newScreen := s.Screen(128, 128)
	if oldScreen.CanBeSource() {
		t.Error("oldScreen.CanBeSource() = true after resize, want false")
	}

	// Exercise every mutating and reading operation on the stale screen image, plus using it as a source.
	src := NewImage(16, 16, atlas.ImageTypeRegular)
	defer src.Deallocate()
	src.Fill(0, 1, 0, 1, image.Rect(0, 0, 16, 16))

	var vs [4 * graphics.VertexFloatCount]float32
	graphics.QuadVerticesFromDstAndSrc(vs[:], 0, 0, 16, 16, 0, 0, 16, 16, 1, 1, 1, 1)
	dr := image.Rect(0, 0, 16, 16)
	sr := [graphics.ShaderSrcImageCount]image.Rectangle{dr}

	oldScreen.Fill(0, 0, 1, 1, dr)
	oldScreen.DrawTriangles([graphics.ShaderSrcImageCount]*Image{src}, vs[:], graphics.QuadIndices(), graphicsdriver.BlendCopy, dr, sr, atlas.NearestFilterShader(), nil, true)
	oldScreen.DrawQuads([]*Image{src}, vs[:], graphicsdriver.BlendCopy, dr, []image.Rectangle{dr}, atlas.NearestFilterShader())
	oldScreen.WritePixel(1, 1, [4]byte{1, 2, 3, 4})
	oldScreen.WritePixels(make([]byte, 4*4*4), image.Rect(0, 0, 4, 4))
	_ = oldScreen.ReadPixel(1, 1)
	oldScreen.ReadPixels(make([]byte, 4*4*4), image.Rect(0, 0, 4, 4))

	// Also try drawing from oldScreen into newScreen.
	newScreen.DrawTriangles([graphics.ShaderSrcImageCount]*Image{oldScreen}, vs[:], graphics.QuadIndices(), graphicsdriver.BlendCopy, dr, sr, atlas.NearestFilterShader(), nil, true)
	newScreen.DrawQuads([]*Image{oldScreen}, vs[:], graphicsdriver.BlendCopy, dr, []image.Rectangle{dr}, atlas.NearestFilterShader())
	newScreen.Fill(0, 1, 0, 1, image.Rect(0, 0, 128, 128))

	if err := graphicscommand.FlushCommands(driver, graphicsdriver.FlushModeIntermediate); err != nil {
		t.Fatal(err)
	}
	calls := surf.snapshotCalls()
	for i, pt := range calls {
		if i > 0 && pt == image.Pt(64, 64) {
			t.Fatalf("stale screen re-allocated at old size (64,64): NewScreenImage calls = %v", calls)
		}
	}
	if len(calls) != 2 || calls[1] != image.Pt(128, 128) {
		t.Fatalf("after resize and stale draws: NewScreenImage calls = %v, want [(64,64) (128,128)]", calls)
	}

	// Disposing the surface also makes newScreen terminal.
	s.Dispose()
	newScreen.Fill(1, 1, 1, 1, image.Rect(0, 0, 128, 128))
	if err := graphicscommand.FlushCommands(driver, graphicsdriver.FlushModeIntermediate); err != nil {
		t.Fatal(err)
	}
	if calls := surf.snapshotCalls(); len(calls) != 2 {
		t.Fatalf("after Surface.Dispose and draw to stale screen: NewScreenImage calls = %v, want len 2", calls)
	}
}

// DrawQuads skips only quads whose source is a disposed screen image while still drawing every
// valid quad in order and never reallocating the disposed screen.
func TestDrawQuadsSkipsOnlyDisposedSources(t *testing.T) {
	surf := &countingScreenSurface{}
	s := &Surface{surface: surf}

	oldScreen := s.Screen(32, 32)
	oldScreen.Fill(0, 0, 1, 1, image.Rect(0, 0, 32, 32))
	if err := graphicscommand.FlushCommands(driver, graphicsdriver.FlushModeIntermediate); err != nil {
		t.Fatal(err)
	}

	// Replace oldScreen so it becomes permanently disposed.
	_ = s.Screen(64, 64)
	defer s.Dispose()

	icon1 := NewImage(8, 8, atlas.ImageTypeRegular)
	defer icon1.Deallocate()
	icon1.Fill(1, 0, 0, 1, image.Rect(0, 0, 8, 8))

	icon2 := NewImage(8, 8, atlas.ImageTypeRegular)
	defer icon2.Deallocate()
	icon2.Fill(0, 1, 0, 1, image.Rect(0, 0, 8, 8))

	dst := NewImage(32, 16, atlas.ImageTypeRegular)
	defer dst.Deallocate()

	const quad = 4 * graphics.VertexFloatCount
	vs := make([]float32, 3*quad)
	// Quad 0: icon1 at (0, 0)..(8, 8)
	graphics.QuadVerticesFromDstAndSrc(vs[0:quad], 0, 0, 8, 8, 0, 0, 8, 8, 1, 1, 1, 1)
	// Quad 1: disposed oldScreen at (0, 0)..(32, 16) — must be skipped.
	graphics.QuadVerticesFromDstAndSrc(vs[quad:2*quad], 0, 0, 32, 16, 0, 0, 32, 32, 1, 1, 1, 1)
	// Quad 2: icon2 at (4, 0)..(12, 8) — overlaps right half of icon1 to verify order.
	graphics.QuadVerticesFromDstAndSrc(vs[2*quad:3*quad], 4, 0, 12, 8, 0, 0, 8, 8, 1, 1, 1, 1)

	srcs := []*Image{icon1, oldScreen, icon2}
	regions := []image.Rectangle{
		image.Rect(0, 0, 8, 8),
		image.Rect(0, 0, 32, 32),
		image.Rect(0, 0, 8, 8),
	}
	dst.DrawQuads(srcs, vs, graphicsdriver.BlendSourceOver, image.Rect(0, 0, 32, 16), regions, atlas.NearestFilterShader())

	if got, want := dst.ReadPixel(1, 4), [4]byte{0xff, 0, 0, 0xff}; got != want {
		t.Errorf("icon1 pixel at (1,4) = %v, want %v", got, want)
	}
	if got, want := dst.ReadPixel(6, 4), [4]byte{0, 0xff, 0, 0xff}; got != want {
		t.Errorf("overlapping icon2 pixel at (6,4) = %v, want %v (order preserved)", got, want)
	}
	if got, want := dst.ReadPixel(10, 4), [4]byte{0, 0xff, 0, 0xff}; got != want {
		t.Errorf("icon2 pixel at (10,4) = %v, want %v", got, want)
	}
	if got, want := dst.ReadPixel(20, 4), [4]byte{0, 0, 0, 0}; got != want {
		t.Errorf("unwritten area at (20,4) = %v, want transparent %v", got, want)
	}
	var oldSizeAllocs int
	for _, pt := range surf.snapshotCalls() {
		if pt == image.Pt(32, 32) {
			oldSizeAllocs++
		}
	}
	if oldSizeAllocs != 1 {
		t.Fatalf("disposed 32x32 screen was re-allocated: NewScreenImage calls = %v", surf.snapshotCalls())
	}
}

// staleKeptSurface mimics Metal's KeptSurface behavior where Kept() continues returning true
// from the previous presentation until the old screen image's queued Dispose actually executes
// on the render thread (or stays true whenever the driver considers a previous frame presented).
type staleKeptSurface struct {
	mu          sync.Mutex
	driverKept  bool
	keptCalls   int
	lastCreated image.Point
}

func (s *staleKeptSurface) NewScreenImage(width, height int) (graphicsdriver.Image, error) {
	img, err := driver.NewImage(width, height, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.lastCreated = image.Pt(width, height)
	s.mu.Unlock()
	return img, nil
}

func (s *staleKeptSurface) FlipsY() bool   { return false }
func (s *staleKeptSurface) Readable() bool { return true }
func (s *staleKeptSurface) Dispose()       {}

func (s *staleKeptSurface) Kept() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keptCalls++
	return s.driverKept
}

func (s *staleKeptSurface) setDriverKept(v bool) {
	s.mu.Lock()
	s.driverKept = v
	s.mu.Unlock()
}

func (s *staleKeptSurface) snapshotKeptCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keptCalls
}

// failOnceBeginDriver wraps a driver and fails its next Begin() call once.
type failOnceBeginDriver struct {
	graphicsdriver.Graphics
	failNext atomic.Bool
}

func (g *failOnceBeginDriver) Begin() error {
	if g.failNext.Swap(false) {
		return errors.New("injected Begin failure")
	}
	return g.Graphics.Begin()
}

// TestKeptAfterDeferredResize verifies Z1 / C-X10: after a size change, Surface.Screen keeps
// reporting kept == false (without consulting KeptSurface.Kept) until the render-thread callback
// queued right after the old screen's Dispose has run (or been dropped on flush error), after
// which Screen consults the driver's Kept() directly.
func TestKeptAfterDeferredResize(t *testing.T) {
	// TestMain runs tests inside an open frame; balance EndFrame/BeginFrame across steps.
	stepFrame := func(doFlush, present bool) {
		t.Helper()
		if err := EndFrame(); err != nil {
			t.Fatalf("EndFrame: %v", err)
		}
		if doFlush {
			if err := Flush(present); err != nil {
				t.Fatalf("Flush(%v): %v", present, err)
			}
		}
		if err := BeginFrame(); err != nil {
			t.Fatalf("BeginFrame: %v", err)
		}
	}

	surf := &staleKeptSurface{}
	s := &Surface{surface: surf}
	defer s.Dispose()

	// 1. Steady state (no resize in flight): initial Screen(64,64) creates screen without touching
	// the resize gate; subsequent Screen(64,64) consults driver Kept() directly for both false/true.
	scr := s.Screen(64, 64)
	if s.Kept() {
		t.Fatalf("initial Screen(64,64): Kept() = true, want false")
	}
	if s.resizeGen != 0 {
		t.Fatalf("steady state touched resizeGen: got %d, want 0", s.resizeGen)
	}
	scr.Fill(1, 0, 0, 1, image.Rect(0, 0, 64, 64))
	stepFrame(true, true)

	surf.setDriverKept(false)
	_ = s.Screen(64, 64)
	if s.Kept() {
		t.Fatalf("steady-state Screen(64,64) with driver Kept=false: got true, want false")
	}
	surf.setDriverKept(true)
	_ = s.Screen(64, 64)
	if !s.Kept() {
		t.Fatalf("steady-state Screen(64,64) with driver Kept=true: got false, want true")
	}
	if s.resizeGen != 0 {
		t.Fatalf("steady state touched resizeGen: got %d, want 0", s.resizeGen)
	}
	calls := surf.snapshotKeptCalls()
	if calls != 2 {
		t.Fatalf("expected 2 steady-state driver Kept() calls, got %d", calls)
	}

	// 2. Resize A->B, frame draws NOTHING, and dispose has NOT run yet (no flush yet):
	// driver still answers Kept()==true from old presentation A; next Screen(B) must report false
	// without consulting driver Kept().
	_ = s.Screen(128, 128)
	if s.Kept() {
		t.Fatalf("resize Screen(128,128): Kept() = true, want false")
	}
	stepFrame(false, false)

	_ = s.Screen(128, 128)
	if s.Kept() {
		t.Fatalf("Screen(128,128) before queued dispose ran: Kept() = true, want false")
	}
	if got := surf.snapshotKeptCalls(); got != calls {
		t.Fatalf("driver Kept() consulted before queued dispose ran: calls = %d, want %d", got, calls)
	}

	// 3. Once the flush executes the queued dispose + callback on the render thread, Screen(B)
	// returns the driver's Kept() answer directly (both false and true).
	stepFrame(true, true)

	surf.setDriverKept(false)
	_ = s.Screen(128, 128)
	if s.Kept() {
		t.Fatalf("Screen(128,128) after dispose with driver Kept=false: got true, want false")
	}
	surf.setDriverKept(true)
	_ = s.Screen(128, 128)
	if !s.Kept() {
		t.Fatalf("Screen(128,128) after dispose with driver Kept=true: got false, want true")
	}
	calls += 2
	if got := surf.snapshotKeptCalls(); got != calls {
		t.Fatalf("expected driver Kept() consulted twice after dispose ran: calls = %d, want %d", got, calls)
	}

	// 4. Resize away and back (B -> C -> B) before dispose runs: latest generation keeps the gate
	// shut until gen C->B's callback executes.
	_ = s.Screen(96, 96)
	if s.Kept() {
		t.Fatalf("B->C Screen(96,96): Kept() = true, want false")
	}
	stepFrame(false, false)

	_ = s.Screen(128, 128)
	if s.Kept() {
		t.Fatalf("C->B Screen(128,128): Kept() = true, want false")
	}
	stepFrame(false, false)

	_ = s.Screen(128, 128)
	if s.Kept() {
		t.Fatalf("B->C->B before dispose ran: Kept() = true, want false")
	}
	if got := surf.snapshotKeptCalls(); got != calls {
		t.Fatalf("driver Kept() consulted during B->C->B before dispose ran: calls = %d, want %d", got, calls)
	}
	stepFrame(true, true)
	_ = s.Screen(128, 128)
	if !s.Kept() {
		t.Fatalf("B->C->B after dispose ran with driver Kept=true: got false, want true")
	}
	calls++

	// 5. Case 1: resize -> draw -> Flush(false) -> Flush(true) -> next Screen returns driver's answer.
	scr = s.Screen(160, 160)
	scr.Fill(0, 1, 0, 1, image.Rect(0, 0, 160, 160))
	stepFrame(true, false)
	stepFrame(true, true)
	surf.setDriverKept(true)
	_ = s.Screen(160, 160)
	if !s.Kept() {
		t.Fatalf("case 1 (resize + draw + Flush(false) + Flush(true)): Kept() = false, want true")
	}
	calls++

	// 6. Case 3: one-shot Begin() failure on the flush carrying the dispose must still release
	// the gate on queue drop so subsequent successful frames consult the driver's Kept().
	realDriver := driver
	failDrv := &failOnceBeginDriver{Graphics: realDriver}
	failDrv.failNext.Store(true)
	driver = failDrv
	defer func() { driver = realDriver }()

	_ = s.Screen(192, 192)
	if err := EndFrame(); err != nil {
		t.Fatalf("EndFrame before failing Begin: %v", err)
	}
	if err := Flush(true); err == nil {
		t.Fatalf("expected injected Begin failure from Flush, got nil")
	}
	driver = realDriver
	if err := BeginFrame(); err != nil {
		t.Fatalf("BeginFrame after failing Begin: %v", err)
	}
	surf.setDriverKept(true)
	_ = s.Screen(192, 192)
	if !s.Kept() {
		t.Fatalf("case 3 (after one-shot Begin failure): gate stuck shut, Kept() = false, want true")
	}
	calls++
	if got := surf.snapshotKeptCalls(); got != calls {
		t.Fatalf("case 3 driver Kept() calls = %d, want %d", got, calls)
	}
}
