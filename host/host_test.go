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

package host_test

import (
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/host"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	etesting "github.com/ironpark/ggfx/internal/testing"
	"github.com/ironpark/ggfx/vector"
)

const subprocessEnv = "GGFX_HOST_TEST_SUBPROCESS"

// noBrowserDOM is set on js/wasm when running outside a browser (e.g. Node.js via go_js_wasm_exec)
// where no WebGL canvas can be created.
var noBrowserDOM bool

func init() {
	runtime.LockOSThread()
}

func requireDriver(t *testing.T) {
	t.Helper()
	if noBrowserDOM {
		t.Skip("WebGL tests require a browser DOM")
	}
}

func setupGLContextIfNeeded() error {
	if host.CurrentDriver().Library == host.GraphicsLibraryOpenGL && runtime.GOOS != "js" {
		c, err := etesting.NewGLContext()
		if err != nil {
			return fmt.Errorf("host_test: OpenGL context: %w", err)
		}
		host.SetGLContext(c)
	}
	return nil
}

func TestMain(m *testing.M) {
	switch os.Getenv(subprocessEnv) {
	case "singlethread":
		if err := host.Init(&host.Options{SingleThread: true}); err != nil {
			panic(err)
		}
		ok, err := newCanvasSurface()
		if err != nil {
			panic(err)
		}
		noBrowserDOM = !ok
		if err := setupGLContextIfNeeded(); err != nil {
			panic(err)
		}
		code := m.Run()
		host.Terminate()
		os.Exit(code)
	case "lifecycle":
		// Subprocess drives Init/Terminate itself.
		os.Exit(m.Run())
	default:
		if err := host.Init(nil); err != nil {
			panic(err)
		}
		ok, err := newCanvasSurface()
		if err != nil {
			panic(err)
		}
		noBrowserDOM = !ok
		if err := setupGLContextIfNeeded(); err != nil {
			panic(err)
		}
		code := m.Run()
		host.Terminate()
		os.Exit(code)
	}
}

func runWithTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("timed out after %v (deadlock?)", d)
	}
}

func runSubprocess(t *testing.T, mode, testName string) {
	t.Helper()
	if runtime.GOOS == "js" {
		t.Skip("subprocess test is not supported on js/wasm")
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), subprocessEnv+"="+mode)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess %s (%s) failed: %v\n%s", mode, testName, err, string(out))
	}
}

// TestReadPixelsBetweenFrames verifies C-Q1: calling At / RGBA64At / ReadPixels after Frame.End
// (both before and after host.Flush) on the frame-driving goroutine completes synchronously with
// the right pixels instead of deadlocking waiting for another BeginFrame, and concurrent uncached
// reads overlap frames safely.
func TestReadPixelsBetweenFrames(t *testing.T) {
	requireDriver(t)
	img := ggfx.NewImage(16, 16)
	defer img.Deallocate()

	red := color.RGBA{R: 0xff, G: 0x00, B: 0x00, A: 0xff}
	green := color.RGBA{R: 0x00, G: 0xff, B: 0x00, A: 0xff}
	blue := color.RGBA{R: 0x00, G: 0x00, B: 0xff, A: 0xff}

	// 1 & 2. Run the entire BeginFrame -> Fill -> End -> At/ReadPixels -> Flush -> deferred Fill -> At
	// sequence on a single worker goroutine under a timeout so the frame-driving goroutine itself
	// performs every read.
	runWithTimeout(t, 10*time.Second, func() {
		f, err := host.BeginFrame()
		if err != nil {
			t.Errorf("BeginFrame: %v", err)
			return
		}
		img.Fill(red)
		if err := f.End(); err != nil {
			t.Errorf("End: %v", err)
			return
		}

		if got := img.At(4, 4).(color.RGBA); got != red {
			t.Errorf("At after Frame.End: got %v, want %v", got, red)
		}
		pix := make([]byte, 4*16*16)
		img.ReadPixels(pix)
		got := color.RGBA{R: pix[0], G: pix[1], B: pix[2], A: pix[3]}
		if got != red {
			t.Errorf("ReadPixels after Frame.End: got %v, want %v", got, red)
		}

		if _, err := host.Flush(false); err != nil {
			t.Errorf("Flush: %v", err)
			return
		}
		img.Fill(green)
		if got := img.At(8, 8).(color.RGBA); got != green {
			t.Errorf("At after deferred Fill between frames: got %v, want %v", got, green)
		}
	})

	// 3. Concurrent BeginFrame/Fill/End/Flush on one goroutine while another reads uncached pixels
	// (every frame invalidates the pixel cache tile via Fill so real GPU readbacks overlap frames).
	f, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	img.Fill(blue)
	if err := f.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(false); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	readReq := make(chan struct{}, 1)
	readRes := make(chan color.RGBA, 1)
	stop := make(chan struct{})
	var coldReads atomic.Int64
	var loggedUnexpected atomic.Bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			case <-readReq:
				got := img.At(2, 2).(color.RGBA)
				coldReads.Add(1)
				select {
				case readRes <- got:
				case <-stop:
					return
				}
			default:
				got := img.At(2, 2).(color.RGBA)
				if got != blue && got != red && !loggedUnexpected.Swap(true) {
					t.Errorf("concurrent At: got %v, want %v or %v", got, blue, red)
				}
			}
		}
	}()

	const overlapIters = 10
	for iter := range overlapIters {
		f, err := host.BeginFrame()
		if err != nil {
			close(stop)
			wg.Wait()
			t.Fatal(err)
		}
		want := red
		if iter%2 == 1 {
			want = blue
		}
		img.Fill(want)
		if err := f.End(); err != nil {
			close(stop)
			wg.Wait()
			t.Fatal(err)
		}
		// Trigger a cold read right after Fill invalidated the pixel cache and Frame.End ended
		// the frame, overlapping with Flush on half the iterations and waiting after End on others.
		readReq <- struct{}{}
		var got color.RGBA
		if iter%2 == 0 {
			got = <-readRes
		}
		if _, err := host.Flush(false); err != nil {
			close(stop)
			wg.Wait()
			t.Fatal(err)
		}
		if iter%2 == 1 {
			got = <-readRes
		}
		if got != want {
			t.Errorf("iter %d: concurrent At after Fill(%v): got %v, want %v", iter, want, got, want)
		}
	}
	close(stop)
	wg.Wait()
	if n := coldReads.Load(); n != overlapIters {
		t.Errorf("concurrent reader completed %d post-invalidation reads, want %d", n, overlapIters)
	}
}

func fillBusyFrame(dst, src *ggfx.Image) {
	var p vector.Path
	p.MoveTo(10, 10)
	p.LineTo(110, 20)
	p.LineTo(90, 110)
	p.LineTo(20, 90)
	p.Close()
	vector.FillPath(dst, &p, nil, &vector.DrawPathOptions{AntiAlias: true})

	vs := make([]ggfx.Vertex, 2000)
	is := make([]uint32, 3000)
	for i := range 1000 {
		base := uint32(i % 1998)
		is[3*i], is[3*i+1], is[3*i+2] = base, base+1, base+2
	}
	for range 20 {
		dst.DrawTriangles(vs, is, src, nil)
	}
}

// TestTrimReleasesCaches verifies C-P6: host.Trim releases idle caches (including vector stencil
// caches via the registered frame sweeper and oversized command-queue buffers) without needing
// hundreds of idle Flush calls.
func TestTrimReleasesCaches(t *testing.T) {
	requireDriver(t)
	dst := ggfx.NewImage(128, 128)
	defer dst.Deallocate()
	src := ggfx.NewImage(64, 64)
	defer src.Deallocate()
	src.Fill(color.White)

	f, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	fillBusyFrame(dst, src)
	if err := f.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(false); err != nil {
		t.Fatal(err)
	}

	if got := vector.StencilBytes(); got == 0 {
		t.Fatalf("expected non-zero vector.StencilBytes() after FillPath, got 0")
	}
	var before ggfx.DebugInfo
	ggfx.ReadDebugInfo(&before)

	if err := host.Trim(); err != nil {
		t.Fatalf("host.Trim: %v", err)
	}

	if got := vector.StencilBytes(); got != 0 {
		t.Errorf("vector.StencilBytes() after host.Trim() = %d, want 0", got)
	}
	var after ggfx.DebugInfo
	ggfx.ReadDebugInfo(&after)
	if after.QueueBufferBytes >= before.QueueBufferBytes/2 {
		t.Errorf("QueueBufferBytes after host.Trim() = %d (was %d), want < %d", after.QueueBufferBytes, before.QueueBufferBytes, before.QueueBufferBytes/2)
	}

	// Verify drawing still works normally after Trim.
	f, err = host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	dst.Fill(color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff})
	if err := f.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(false); err != nil {
		t.Fatal(err)
	}
	if got, want := dst.At(1, 1).(color.RGBA), (color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff}); got != want {
		t.Errorf("pixel after Trim + redraw = %v, want %v", got, want)
	}
}

// TestTrimAfterEndWithoutFlush verifies that calling host.Trim directly after Frame.End (without
// an intervening host.Flush) drains the pending queue BEFORE advancing the idle clock so the
// busy frame's vertex/index, uniform, and upload-pool buffers are all released.
func TestTrimAfterEndWithoutFlush(t *testing.T) {
	requireDriver(t)
	dst := ggfx.NewImage(128, 128)
	defer dst.Deallocate()
	src := ggfx.NewImage(64, 64)
	defer src.Deallocate()
	src.Fill(color.White)

	upload := ggfx.NewImage(1024, 1024)
	defer upload.Deallocate()
	uploadPixels := make([]byte, 4*1024*1024)

	shader, err := ggfx.NewShader([]byte(`
struct Uniforms {
	data: array<vec4f, 128>,
}
@group(1) @binding(0) var<uniform> u: Uniforms;

fn fragment(v: Vertex) -> vec4f {
	return u.data[0];
}
`))
	if err != nil {
		t.Fatal(err)
	}
	defer shader.Deallocate()

	f, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	fillBusyFrame(dst, src)
	upload.WritePixels(uploadPixels)
	var uniformData [128 * 4]float32
	for i := range 256 {
		uniformData[0] = float32(i) / 255
		dst.DrawRectShader(16, 16, shader, &ggfx.DrawRectShaderOptions{
			Uniforms: map[string]any{"data": uniformData[:]},
		})
	}
	if err := f.End(); err != nil {
		t.Fatal(err)
	}

	var before ggfx.DebugInfo
	ggfx.ReadDebugInfo(&before)
	if before.QueueBufferBytes == 0 {
		t.Fatalf("expected non-zero QueueBufferBytes after busy Frame.End")
	}
	uniformBefore := graphicscommand.UniformBufferBytes()
	if uniformBefore < 512*1024 {
		t.Fatalf("expected UniformBufferBytes >= 512 KiB after varying-uniform draws, got %d", uniformBefore)
	}

	// No host.Flush before Trim.
	if err := host.Trim(); err != nil {
		t.Fatalf("host.Trim after End without Flush: %v", err)
	}

	if got := vector.StencilBytes(); got != 0 {
		t.Errorf("vector.StencilBytes() after End->Trim = %d, want 0", got)
	}
	var after ggfx.DebugInfo
	ggfx.ReadDebugInfo(&after)
	if after.QueueBufferBytes >= before.QueueBufferBytes/2 {
		t.Errorf("QueueBufferBytes after End->Trim = %d (was %d), want < %d", after.QueueBufferBytes, before.QueueBufferBytes, before.QueueBufferBytes/2)
	}
	if uniformAfter := graphicscommand.UniformBufferBytes(); uniformAfter >= uniformBefore/2 {
		t.Errorf("UniformBufferBytes after End->Trim = %d (was %d), want < %d", uniformAfter, uniformBefore, uniformBefore/2)
	}
	if poolAfter := graphics.BytesPoolBytes(); poolAfter != 0 {
		t.Errorf("BytesPoolBytes after End->Trim = %d, want 0", poolAfter)
	}
}

// TestTrimConcurrentWithReadPixels verifies that host.Trim serializes command-queue buffer
// trimming with concurrent between-frame uncached reads under -race.
func TestTrimConcurrentWithReadPixels(t *testing.T) {
	requireDriver(t)
	// Ensure the driver is initialized.
	f, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	img := ggfx.NewImage(32, 32)
	defer img.Deallocate()
	c1 := color.RGBA{R: 0xaa, G: 0x11, B: 0x22, A: 0xff}
	c2 := color.RGBA{R: 0x22, G: 0xbb, B: 0x44, A: 0xff}
	img.Fill(c1)
	if err := f.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(false); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	started := make(chan struct{})
	stop := make(chan struct{})
	var reads atomic.Int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		close(started)
		buf := make([]byte, 4*32*32)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				want := c1
				if i%2 == 1 {
					want = c2
				}
				img.Fill(want)
				if got := img.At(5, 5).(color.RGBA); got != want {
					t.Errorf("concurrent At during Trim: got %v, want %v", got, want)
					return
				}
				img.ReadPixels(buf)
				got := color.RGBA{R: buf[0], G: buf[1], B: buf[2], A: buf[3]}
				if got != want {
					t.Errorf("concurrent ReadPixels during Trim: got %v, want %v", got, want)
					return
				}
				reads.Add(1)
			}
		}
	}()

	<-started
	for range 15 {
		if err := host.Trim(); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("host.Trim: %v", err)
		}
		runtime.Gosched()
	}
	close(stop)
	wg.Wait()
	if reads.Load() == 0 {
		t.Errorf("expected concurrent uncached reads during Trim")
	}
}

// TestSingleThreadReadPixels verifies C-Q1 in SingleThread mode on the locked renderer OS thread
// (runs on both Metal and OpenGL when GGFX_GRAPHICS_LIBRARY=opengl is set).
func TestSingleThreadReadPixels(t *testing.T) {
	if os.Getenv(subprocessEnv) != "" {
		t.Skip("already inside subprocess")
	}
	runSubprocess(t, "singlethread", "TestSingleThreadReadPixelsSubprocess")
}

func TestSingleThreadReadPixelsSubprocess(t *testing.T) {
	if os.Getenv(subprocessEnv) != "singlethread" {
		t.Skip("subprocess helper")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	img := ggfx.NewImage(16, 16)
	defer img.Deallocate()
	red := color.RGBA{R: 0xff, G: 0x00, B: 0x00, A: 0xff}
	green := color.RGBA{R: 0x00, G: 0xff, B: 0x00, A: 0xff}

	f, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	img.Fill(red)
	if err := f.End(); err != nil {
		t.Fatal(err)
	}
	if got := img.At(3, 3).(color.RGBA); got != red {
		t.Fatalf("SingleThread At after End: got %v, want %v", got, red)
	}
	pix := make([]byte, 4*16*16)
	img.ReadPixels(pix)
	if got := (color.RGBA{R: pix[0], G: pix[1], B: pix[2], A: pix[3]}); got != red {
		t.Fatalf("SingleThread ReadPixels after End: got %v, want %v", got, red)
	}

	if _, err := host.Flush(false); err != nil {
		t.Fatal(err)
	}
	img.Fill(green)
	if got := img.At(7, 7).(color.RGBA); got != green {
		t.Fatalf("SingleThread At after deferred Fill: got %v, want %v", got, green)
	}
	if err := host.Trim(); err != nil {
		t.Fatalf("SingleThread Trim: %v", err)
	}
}

// TestReadPixelsLifecycle verifies that reads before Init and after Terminate panic immediately
// without latching errors, and that a read after Init but before the first frame (or before
// SetGLContext on desktop OpenGL) fails with a clear error without initializing the driver.
func TestReadPixelsLifecycle(t *testing.T) {
	if os.Getenv(subprocessEnv) != "" {
		t.Skip("already inside subprocess")
	}
	runSubprocess(t, "lifecycle", "TestReadPixelsLifecycleSubprocess")
}

func TestReadPixelsLifecycleSubprocess(t *testing.T) {
	if os.Getenv(subprocessEnv) != "lifecycle" {
		t.Skip("subprocess helper")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	expectPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			r := recover()
			if r == nil {
				t.Fatalf("%s: expected panic, got none", name)
			}
			msg := fmt.Sprint(r)
			if !strings.Contains(msg, "before Init or after Terminate") {
				t.Fatalf("%s: unexpected panic %q", name, msg)
			}
		}()
		fn()
	}

	img := ggfx.NewImage(8, 8)
	buf := make([]byte, 4*8*8)

	// 1. Before Init: must panic immediately and NOT latch an error.
	expectPanic("At before Init", func() { _ = img.At(0, 0) })
	expectPanic("ReadPixels before Init", func() { img.ReadPixels(buf) })

	if err := host.Init(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := newCanvasSurface(); err != nil {
		t.Fatal(err)
	}

	// 2. After Init, before SetGLContext (on OpenGL) and before any frame: must fail cleanly
	// without initializing the driver or touching GL.
	if got := img.At(0, 0).(color.RGBA); got != (color.RGBA{}) {
		t.Fatalf("At before driver initialized: got %v, want zero", got)
	}

	// Now set GL context (if OpenGL). The first BeginFrame must surface the recorded error from
	// the uninitialized read (draining it), and the next BeginFrame must succeed normally.
	if err := setupGLContextIfNeeded(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.BeginFrame(); err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("BeginFrame after uninitialized read: got err %v, want 'not initialized'", err)
	}

	// Subsequent frame succeeds cleanly (proving the earlier before-Init panic did not poison it
	// and the uninitialized read did not bind a bad GL context).
	f, err := host.BeginFrame()
	if err != nil {
		t.Fatalf("second BeginFrame: %v", err)
	}
	want := color.RGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}
	img.Fill(want)
	if err := f.End(); err != nil {
		t.Fatal(err)
	}
	if got := img.At(1, 1).(color.RGBA); got != want {
		t.Fatalf("At after valid frame: got %v, want %v", got, want)
	}
	if _, err := host.Flush(false); err != nil {
		t.Fatal(err)
	}

	// 3. After Terminate: must panic immediately.
	host.Terminate()
	expectPanic("At after Terminate", func() { _ = img.At(0, 0) })
	expectPanic("ReadPixels after Terminate", func() { img.ReadPixels(buf) })
}
