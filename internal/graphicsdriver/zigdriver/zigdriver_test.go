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

//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"bytes"
	"errors"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/ironpark/ggfx/internal/builtinshader"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/drivertest"
	"github.com/ironpark/ggfx/internal/shader"
)

var resolvedTestLibPath string

func TestMain(m *testing.M) {
	resolvedTestLibPath = findTestDriverLib()
	os.Exit(m.Run())
}

func findTestDriverLib() string {
	if env := os.Getenv("GGFX_DRIVER_LIB"); env != "" {
		return env
	}
	sub := "lib"
	if runtime.GOOS == "windows" {
		sub = "bin"
	}
	cand := filepath.Join("..", "..", "..", "zig", "zig-out", sub, libFileName())
	if abs, err := filepath.Abs(cand); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	return ""
}

func newTestGraphics(t *testing.T, opts Options) *Graphics {
	t.Helper()
	if opts.LibPath == "" {
		if resolvedTestLibPath == "" {
			if os.Getenv("GGFX_DRIVER_STRICT") == "1" {
				t.Fatalf("zig driver shared library not found (GGFX_DRIVER_STRICT=1)")
			}
			t.Skip("zig driver shared library not found (set GGFX_DRIVER_LIB or build zig/zig-out)")
		}
		opts.LibPath = resolvedTestLibPath
	}
	g, err := NewGraphics(opts)
	if err != nil {
		if errors.Is(err, ErrBackendUnavailable) && os.Getenv("GGFX_DRIVER_STRICT") != "1" {
			t.Skipf("zig driver backend unavailable: %v", err)
		}
		t.Fatalf("NewGraphics: %v", err)
	}
	return g
}

func TestDriverConformance(t *testing.T) {
	g := newTestGraphics(t, Options{Slots: 2})
	if !DeviceCreated() {
		t.Fatalf("DeviceCreated() = false after NewGraphics")
	}

	caps := g.Caps()
	drivertest.RunSuite(t, g, drivertest.Options{
		CheckDrawPixels: caps.Backend != BackendNull,
	})

	// Flush any deferred disposals and verify zero live resources before Close.
	if err := g.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatalf("End: %v", err)
	}

	st, err := g.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	if st.LiveImages != 0 || st.LiveShaders != 0 || st.LiveSurfaces != 0 {
		t.Fatalf("unexpected live handles after conformance suite: %+v", st)
	}

	if err := g.Close(); err != nil {
		t.Fatalf("Close (leak check): %v", err)
	}
}

func TestStateCacheBindsSkipped(t *testing.T) {
	g := newTestGraphics(t, Options{Slots: 3})
	defer func() {
		if err := g.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	prog, err := shader.CompileUser(builtinshader.ShaderSource(builtinshader.FilterNearest, builtinshader.AddressUnsafe))
	if err != nil {
		t.Fatal(err)
	}

	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}

	src, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Dispose()

	dst, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Dispose()

	sh, err := g.NewShader(prog.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Dispose()

	vs := make([]float32, 4*graphics.VertexFloatCount)
	graphics.QuadVerticesFromDstAndSrc(vs, 0, 0, 16, 16, 0, 0, 16, 16, 1, 1, 1, 1)
	is := graphics.QuadIndices()
	if err := g.SetVertices(vs, is); err != nil {
		t.Fatal(err)
	}

	uniforms := make([]uint32, InternalUniformDwords+uint32(prog.UniformDwordCount))
	uniforms[56] = 0x3f800000 // projection[0] = 1.0
	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
	srcs[0] = src
	regions := []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 16, 16), IndexCount: len(is)}}
	vers := graphicsdriver.UniformVersions{Internal: 1001, User: 2002}

	for range 5 {
		if err := g.DrawTriangles(dst, srcs, sh, regions, 0, graphicsdriver.BlendCopy, uniforms, vers); err != nil {
			t.Fatal(err)
		}
	}

	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}

	st, err := g.DebugStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Draws < 5 {
		t.Fatalf("Draws = %d, want >= 5", st.Draws)
	}
	if st.BindsSkipped == 0 {
		t.Fatalf("BindsSkipped = 0 after 5 identical draws, want > 0")
	}
}

func TestPayloadOverflowAutoFlush(t *testing.T) {
	g := newTestGraphics(t, Options{})
	defer func() {
		if err := g.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	// Cap payload arena to 512 bytes so multiple writes trigger intermediate flushes.
	for i := range g.ring.n {
		g.ring.slots[i].maxPayload = 512
	}

	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	img, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Dispose()

	// Write 4 stripes of 16x4 = 256 bytes each (1024 bytes total > 512 byte slot limit).
	want := make([]byte, 4*16*16)
	for i := range want {
		want[i] = byte((i*29 + 11) & 0xff)
	}
	for row := 0; row < 16; row += 4 {
		stripe := want[row*16*4 : (row+4)*16*4]
		if err := img.WritePixels([]graphicsdriver.PixelsArgs{
			{Pixels: stripe, Region: image.Rect(0, row, 16, row+4)},
		}); err != nil {
			t.Fatalf("WritePixels row=%d: %v", row, err)
		}
	}

	stMid, err := g.DebugStats()
	if err != nil {
		t.Fatal(err)
	}
	if stMid.Submits == 0 {
		t.Fatalf("expected at least one automatic intermediate submit, got Submits=0")
	}

	got := make([]byte, len(want))
	if err := img.ReadPixels([]graphicsdriver.PixelsArgs{
		{Pixels: got, Region: image.Rect(0, 0, 16, 16)},
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("pixel readback mismatch after auto-flushed writes")
	}

	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}
}

func TestSurfaceLifecycleNullBackend(t *testing.T) {
	g := newTestGraphics(t, Options{})
	defer func() {
		if err := g.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()
	if g.Caps().Backend != BackendNull {
		t.Skip("surface handle smoke test uses synthetic handles on null backend only")
	}

	surf, err := g.NewSurface(graphicsdriver.SurfaceDescriptor{
		Kind:   graphicsdriver.SurfaceKindNSView,
		NSView: graphicsdriver.NSView(0x1234),
	})
	if err != nil {
		t.Fatalf("NewSurface: %v", err)
	}
	s := surf.(*Surface)
	s.SetWindowState(true, false, false)
	_ = s.Kept()
	_ = s.Readable()
	_ = s.FlipsY()
	s.SetDamage([]image.Rectangle{image.Rect(0, 0, 10, 10)})

	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	screen, err := s.NewScreenImage(32, 24)
	if err != nil {
		t.Fatal(err)
	}

	// Screen images reject WritePixels and ReadPixels.
	if err := screen.WritePixels([]graphicsdriver.PixelsArgs{
		{Pixels: make([]byte, 4), Region: image.Rect(0, 0, 1, 1)},
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("WritePixels on screen image = %v, want ErrInvalidArgument", err)
	}
	if err := screen.ReadPixels([]graphicsdriver.PixelsArgs{
		{Pixels: make([]byte, 4), Region: image.Rect(0, 0, 1, 1)},
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ReadPixels on screen image = %v, want ErrInvalidArgument", err)
	}

	if err := g.End(graphicsdriver.FlushModePresent); err != nil {
		t.Fatal(err)
	}

	// Dispose screen image before surface, then flush.
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	screen.Dispose()
	s.Dispose()
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}

	st, err := g.DebugStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.LiveSurfaces != 0 || st.LiveImages != 0 {
		t.Fatalf("live resources remained after surface disposal: %+v", st)
	}
}

func TestErrorPaths(t *testing.T) {
	t.Run("MissingLibrary", func(t *testing.T) {
		_, err := NewGraphics(Options{LibPath: filepath.Join(t.TempDir(), "does_not_exist_driver.lib")})
		if !errors.Is(err, ErrBackendUnavailable) {
			t.Fatalf("NewGraphics(missing) = %v, want ErrBackendUnavailable", err)
		}
	})

	g := newTestGraphics(t, Options{})
	defer func() {
		_ = g.Close()
	}()

	t.Run("DeviceCreateABIMismatch", func(t *testing.T) {
		badOpts := DeviceOptions{
			StructSize: uint32(unsafe.Sizeof(DeviceOptions{})),
			ABIVersion: DriverABIVersion + 99,
			Backend:    BackendAuto,
		}
		var dev uintptr
		var caps DriverCaps
		var errBuf [256]byte
		st := g.lib.deviceCreate(&badOpts, &dev, &caps, &errBuf[0], uintptr(len(errBuf)))
		err := statusToError("device_create", st, errBuf[:])
		if !errors.Is(err, ErrABIMismatch) {
			t.Fatalf("deviceCreate with wrong ABI = %v, want ErrABIMismatch", err)
		}
	})

	t.Run("MalformedBatch", func(t *testing.T) {
		badBatch := Batch{
			StructSize: 12, // wrong struct_size
		}
		var res SubmitResult
		var errBuf [256]byte
		st := g.lib.submit(g.dev, &badBatch, FlushIntermediate, &res, &errBuf[0], uintptr(len(errBuf)))
		err := statusToError("submit", st, errBuf[:])
		if !errors.Is(err, ErrMalformedBatch) {
			t.Fatalf("submit malformed struct_size = %v, want ErrMalformedBatch", err)
		}

		// Unknown opcode in valid-sized batch.
		var bogusCmd [2]uint64
		*(*CmdHeader)(unsafe.Pointer(&bogusCmd[0])) = CmdHeader{Op: 250, Size: 16}
		badOpBatch := Batch{
			StructSize: uint32(unsafe.Sizeof(Batch{})),
			CmdCount:   1,
			Cmds:       uintptr(unsafe.Pointer(&bogusCmd[0])),
			CmdsLen:    16,
		}
		errBuf[0] = 0
		st = g.lib.submit(g.dev, &badOpBatch, FlushIntermediate, &res, &errBuf[0], uintptr(len(errBuf)))
		err = statusToError("submit", st, errBuf[:])
		if !errors.Is(err, ErrMalformedBatch) {
			t.Fatalf("submit unknown opcode = %v, want ErrMalformedBatch", err)
		}
	})

	t.Run("MainThreadRunnerAndFinishForcedFrame", func(t *testing.T) {
		g.SetMainThreadRunner(func(fn func()) { fn() })
		if err := g.FinishForcedFrame(); err != nil {
			t.Fatalf("FinishForcedFrame: %v", err)
		}
		g.SetMainThreadRunner(nil)
	})

	t.Run("InvalidUniformLength", func(t *testing.T) {
		prog, err := shader.CompileUser(builtinshader.ShaderSource(builtinshader.FilterNearest, builtinshader.AddressUnsafe))
		if err != nil {
			t.Fatal(err)
		}
		if err := g.Begin(); err != nil {
			t.Fatal(err)
		}
		img, err := g.NewImage(8, 8, graphicsdriver.PixelFormatRGBA8)
		if err != nil {
			t.Fatal(err)
		}
		defer img.Dispose()
		sh, err := g.NewShader(prog.Descriptor())
		if err != nil {
			t.Fatal(err)
		}
		defer sh.Dispose()

		var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
		regions := []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 8, 8), IndexCount: 6}}
		err = g.DrawTriangles(img, srcs, sh, regions, 0, graphicsdriver.BlendCopy, make([]uint32, 10), graphicsdriver.UniformVersions{Internal: 1})
		if !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("DrawTriangles bad uniforms len = %v, want ErrInvalidArgument", err)
		}
		if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ClosedDevice", func(t *testing.T) {
		if err := g.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if err := g.Begin(); !errors.Is(err, ErrClosed) {
			t.Fatalf("Begin after Close = %v, want ErrClosed", err)
		}
		if _, err := g.DebugStats(); !errors.Is(err, ErrClosed) {
			t.Fatalf("DebugStats after Close = %v, want ErrClosed", err)
		}
		// Double close is a no-op.
		if err := g.Close(); err != nil {
			t.Fatalf("second Close = %v, want nil", err)
		}
	})
}

// TestShaderDisposeAndNewInSameBatch verifies that disposing a shader and creating
// a new shader within the same unsubmitted batch never collides on shader IDs:
// disposed IDs remain in pendingFreeShaderIDs until the batch carrying OP_DISPOSE_SHADER
// has been submitted, and are reused on subsequent batches.
func TestShaderDisposeAndNewInSameBatch(t *testing.T) {
	g := newTestGraphics(t, Options{Slots: 2})
	defer func() {
		if err := g.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	prog, err := shader.CompileUser(builtinshader.ShaderSource(builtinshader.FilterNearest, builtinshader.AddressUnsafe))
	if err != nil {
		t.Fatal(err)
	}

	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	src, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	s1, err := g.NewShader(prog.Descriptor())
	if err != nil {
		t.Fatalf("NewShader s1: %v", err)
	}
	id1 := s1.(*Shader).id

	// Queue disposal of s1 in this batch, then synchronously create s2 before End().
	s1.Dispose()
	s2, err := g.NewShader(prog.Descriptor())
	if err != nil {
		t.Fatalf("NewShader s2 in same batch after Dispose: %v", err)
	}
	id2 := s2.(*Shader).id
	if id2 == id1 {
		t.Fatalf("shader ID %d reused before pending OP_DISPOSE_SHADER was submitted", id1)
	}

	// Draw using s2 within the same batch to verify s2 is valid.
	vs := make([]float32, 4*graphics.VertexFloatCount)
	graphics.QuadVerticesFromDstAndSrc(vs, 0, 0, 16, 16, 0, 0, 16, 16, 1, 1, 1, 1)
	is := graphics.QuadIndices()
	if err := g.SetVertices(vs, is); err != nil {
		t.Fatal(err)
	}
	uniforms := make([]uint32, InternalUniformDwords+uint32(prog.UniformDwordCount))
	uniforms[56] = 0x3f800000
	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
	srcs[0] = src
	regions := []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 16, 16), IndexCount: len(is)}}
	if err := g.DrawTriangles(dst, srcs, s2, regions, 0, graphicsdriver.BlendCopy, uniforms, graphicsdriver.UniformVersions{Internal: 1}); err != nil {
		t.Fatalf("DrawTriangles with s2: %v", err)
	}
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatalf("End batch 1: %v", err)
	}

	// After submission, s1 has been disposed on the native side while s2 remains live.
	st, err := g.DebugStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.LiveShaders != 1 {
		t.Fatalf("LiveShaders after batch 1 = %d, want 1", st.LiveShaders)
	}

	// On the next batch, id1 should now be released for reuse.
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	s3, err := g.NewShader(prog.Descriptor())
	if err != nil {
		t.Fatalf("NewShader s3: %v", err)
	}
	if got := s3.(*Shader).id; got != id1 {
		t.Fatalf("s3 ID = %d, want reused id1=%d after batch submission", got, id1)
	}
	s2.Dispose()
	s3.Dispose()
	src.Dispose()
	dst.Dispose()
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatalf("End batch 2: %v", err)
	}

	st, err = g.DebugStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.LiveShaders != 0 || st.LiveImages != 0 {
		t.Fatalf("unexpected live handles after cleanup: %+v", st)
	}
}

// TestSubmitAllocs verifies that steady-state frame submission (both empty and
// Begin/SetVertices/DrawTriangles/End) performs zero Go heap allocations.
func TestSubmitAllocs(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	g := newTestGraphics(t, Options{Slots: 2})
	defer func() {
		if err := g.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	prog, err := shader.CompileUser(builtinshader.ShaderSource(builtinshader.FilterNearest, builtinshader.AddressUnsafe))
	if err != nil {
		t.Fatal(err)
	}

	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	src, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Dispose()
	dst, err := g.NewImage(32, 32, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Dispose()
	sh, err := g.NewShader(prog.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Dispose()
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}

	vs := make([]float32, 4*graphics.VertexFloatCount)
	graphics.QuadVerticesFromDstAndSrc(vs, 0, 0, 16, 16, 0, 0, 16, 16, 1, 1, 1, 1)
	is := graphics.QuadIndices()
	uniforms := make([]uint32, InternalUniformDwords+uint32(prog.UniformDwordCount))
	uniforms[56] = 0x3f800000
	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
	srcs[0] = src
	regions := []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 32, 32), IndexCount: len(is)}}
	vers := graphicsdriver.UniformVersions{Internal: 1, User: 1}

	drawFrame := func() {
		if err := g.Begin(); err != nil {
			t.Fatal(err)
		}
		if err := g.SetVertices(vs, is); err != nil {
			t.Fatal(err)
		}
		if err := g.DrawTriangles(dst, srcs, sh, regions, 0, graphicsdriver.BlendSourceOver, uniforms, vers); err != nil {
			t.Fatal(err)
		}
		if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
	}

	// Warm up every ring slot and pooled syscall argument buffer before measuring.
	for range 4 {
		drawFrame()
	}

	if allocs := testing.AllocsPerRun(100, drawFrame); allocs != 0 {
		t.Fatalf("steady-state Begin/SetVertices/DrawTriangles/End allocs/run = %.2f, want 0", allocs)
	}

	emptyFrame := func() {
		if err := g.Begin(); err != nil {
			t.Fatal(err)
		}
		if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
	}
	if allocs := testing.AllocsPerRun(100, emptyFrame); allocs != 0 {
		t.Fatalf("steady-state empty Begin/End allocs/run = %.2f, want 0", allocs)
	}
}

// TestNonPOTImageInternalBounds verifies that WritePixels and ReadPixels on a non-POT
// image allow accessing the internal padded region [0..InternalImageSize] while still
// rejecting regions outside internal bounds.
func TestNonPOTImageInternalBounds(t *testing.T) {
	g := newTestGraphics(t, Options{Slots: 2})
	defer func() {
		if err := g.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	const logicalW, logicalH = 5, 5
	iw := graphics.InternalImageSize(logicalW)
	ih := graphics.InternalImageSize(logicalH)
	if iw <= logicalW || ih <= logicalH {
		t.Fatalf("expected InternalImageSize(%d) > %d, got %dx%d", logicalW, logicalW, iw, ih)
	}

	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	img, err := g.NewImage(logicalW, logicalH, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		t.Fatalf("NewImage(%d,%d): %v", logicalW, logicalH, err)
	}
	defer img.Dispose()

	// Region outside logical 5x5 but inside internal 16x16 padding area.
	padRect := image.Rect(logicalW, logicalH, iw, ih)
	want := make([]byte, 4*padRect.Dx()*padRect.Dy())
	for i := range want {
		want[i] = byte((i*37 + 13) & 0xff)
	}
	if err := img.WritePixels([]graphicsdriver.PixelsArgs{
		{Pixels: want, Region: padRect},
	}); err != nil {
		t.Fatalf("WritePixels padding region %v: %v", padRect, err)
	}

	got := make([]byte, len(want))
	if err := img.ReadPixels([]graphicsdriver.PixelsArgs{
		{Pixels: got, Region: padRect},
	}); err != nil {
		t.Fatalf("ReadPixels padding region %v: %v", padRect, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadPixels padding region mismatch")
	}

	// Region exceeding internal bounds must still be rejected.
	oobRect := image.Rect(0, 0, iw+1, ih)
	if err := img.WritePixels([]graphicsdriver.PixelsArgs{
		{Pixels: make([]byte, 4*oobRect.Dx()*oobRect.Dy()), Region: oobRect},
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("WritePixels out of internal bounds = %v, want ErrInvalidArgument", err)
	}
	if err := img.ReadPixels([]graphicsdriver.PixelsArgs{
		{Pixels: make([]byte, 4*oobRect.Dx()*oobRect.Dy()), Region: oobRect},
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ReadPixels out of internal bounds = %v, want ErrInvalidArgument", err)
	}

	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}
}

// TestSurfaceMethodsConcurrentClose verifies under -race that non-render-thread
// calls (Surface.SetWindowState, Surface.Kept, SetMainThreadRunner) running
// concurrently with Graphics.Close do not race on g.dev or use-after-free.
func TestSurfaceMethodsConcurrentClose(t *testing.T) {
	g := newTestGraphics(t, Options{Slots: 2})
	s := &Surface{
		g:    g,
		id:   999,
		caps: SurfaceCaps{Flags: SurfaceCapFlagKept},
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for i := range 200 {
				s.SetWindowState(i%2 == 0, id%2 == 0, false)
				_ = s.Kept()
				if i%16 == 0 {
					g.SetMainThreadRunner(nil)
				}
			}
		}(worker)
	}

	close(start)
	runtime.Gosched()
	if err := g.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wg.Wait()
}
