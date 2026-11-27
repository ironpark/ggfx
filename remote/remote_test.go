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

package remote_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/host"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	rd "github.com/ironpark/ggfx/internal/graphicsdriver/remote"
	"github.com/ironpark/ggfx/internal/graphicsdriver/remote/fakedriver"
	"github.com/ironpark/ggfx/remote"
	"github.com/ironpark/ggfx/vector"
)

func init() {
	// host.Init runs on the main thread.
	runtime.LockOSThread()
}

// viewer is a transport that replays every batch at once on a fake driver, as a viewer would on
// its real one, with the checks a viewer makes.
type viewer struct {
	mu         sync.Mutex
	g          *fakedriver.Graphics
	r          *rd.Replayer
	target     *rd.Target
	batches    int
	present    int
	lastDamage []image.Rectangle
	onSend     func(present bool) error
	err        error
}

func (v *viewer) replay(batch []byte) (rd.Result, error) {
	h, err := rd.ParseHeader(batch)
	if err != nil {
		return rd.Result{}, err
	}
	if h.Screen != 0 && (v.target == nil || v.target.Width != h.Width || v.target.Height != h.Height) {
		v.target = &rd.Target{Image: v.g.NewTarget(h.Width, h.Height), Width: h.Width, Height: h.Height, InternalWidth: h.Width, InternalHeight: h.Height}
	}
	return v.r.Replay(v.g, batch, v.target)
}

func (v *viewer) Send(batch []byte, present bool) error {
	v.mu.Lock()
	hook := v.onSend
	v.mu.Unlock()
	if hook != nil {
		if err := hook(present); err != nil {
			return err
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.batches++
	if present {
		v.present++
	}
	res, err := v.replay(batch)
	if err != nil {
		v.err = err
		return err
	}
	if present {
		v.lastDamage = slices.Clone(res.Damage)
	}
	return nil
}

func (v *viewer) ReadPixels(batch []byte, id uint32, regions []image.Rectangle, dsts [][]byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, err := v.replay(batch); err != nil {
		return err
	}
	return v.r.ReadPixels(id, regions, dsts, v.target)
}

func (v *viewer) setOnSend(fn func(present bool) error) {
	v.mu.Lock()
	v.onSend = fn
	v.mu.Unlock()
}

func (v *viewer) snapshotLastDamage() []image.Rectangle {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.lastDamage)
}

var (
	theViewer = &viewer{g: fakedriver.New(), r: rd.NewReplayer()}
	surface   *host.Surface
)

func TestMain(m *testing.M) {
	info := remote.Info{FlipsY: true, A8: true, MaxImageSize: fakedriver.MaxImageSize}
	if err := remote.Install(info, theViewer); err != nil {
		panic(err)
	}
	if err := host.Init(nil); err != nil {
		panic(err)
	}
	s, err := host.NewSurface(host.RemoteTarget{}, false)
	if err != nil {
		panic(err)
	}
	surface = s
	code := m.Run()
	host.Terminate()
	os.Exit(code)
}

func frame(t *testing.T, present bool, f func(fr host.Frame)) {
	t.Helper()
	fr, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	f(fr)
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(present); err != nil {
		t.Fatal(err)
	}
	if theViewer.err != nil {
		t.Fatalf("the viewer refused a batch: %v", theViewer.err)
	}
}

// TestFrames draws frames through the remote driver as an app does, on its screen, and checks
// that the viewer takes every batch, all the checks of its replay passed.
func TestFrames(t *testing.T) {
	if got := host.CurrentDriver().Library; got != host.GraphicsLibraryRemote {
		t.Fatalf("the driver is %v", got)
	}
	src := ggfx.NewImage(13, 7)
	defer src.Deallocate()
	pix := make([]byte, 4*13*7)
	for i := range pix {
		pix[i] = byte(i)
	}
	src.WritePixels(pix)
	sh, err := ggfx.NewShader([]byte(`
struct Uniforms {
	Tint: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;

fn fragment(v: Vertex) -> vec4f {
	return v.color * u.Tint;
}
`))
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Deallocate()

	before := theViewer.present
	for n := range 3 {
		frame(t, true, func(f host.Frame) {
			fr := f.Screen(surface, 101+n, 77)
			fr.Fill(color.RGBA{0x20, 0x40, 0x60, 0xff})
			op := &ggfx.DrawImageOptions{Filter: ggfx.FilterLinear}
			op.GeoM.Scale(2.5, 3)
			fr.DrawImage(src, op)
			vector.FillCircle(fr, 50, 40, 20+float32(n), color.RGBA{0xff, 0, 0, 0xff}, true)
			fr.DrawRectShader(30, 20, sh, &ggfx.DrawRectShaderOptions{Uniforms: map[string]any{"Tint": []float32{1, 0.5, 0.25, 1}}})
		})
	}
	if got := theViewer.present - before; got != 3 {
		t.Errorf("%d frames presented, want 3", got)
	}
	if images, shaders := theViewer.r.Live(); images == 0 || shaders == 0 {
		t.Errorf("the viewer has %d images and %d shaders of the app's", images, shaders)
	}
}

// TestReadPixels reads pixels the app wrote back from the viewer, a round trip of the driver's.
func TestReadPixels(t *testing.T) {
	img := ggfx.NewImage(9, 5)
	defer img.Deallocate()
	pix := make([]byte, 4*9*5)
	for i := range pix {
		pix[i] = byte(255 - i%251)
	}
	got := make([]byte, len(pix))
	frame(t, false, func(host.Frame) {
		img.WritePixels(pix)
		img.ReadPixels(got)
	})
	if !bytes.Equal(got, pix) {
		t.Errorf("read back %v, want %v", got[:16], pix[:16])
	}
	if !slices.ContainsFunc(theViewer.g.Log, func(s string) bool { return strings.HasPrefix(s, "ReadPixels ") }) {
		t.Error("no read reached the viewer's driver")
	}
}

type captureTransport struct {
	batch []byte
}

func (c *captureTransport) Send(batch []byte, _ bool) error {
	c.batch = slices.Clone(batch)
	return nil
}

func (c *captureTransport) ReadPixels([]byte, uint32, []image.Rectangle, [][]byte) error {
	return nil
}

// TestReplayerLimits verifies that Limits set through the public remote.Replayer API are reported
// back and enforced during Replay.
func TestReplayerLimits(t *testing.T) {
	p := remote.NewReplayer()
	if got, want := p.Limits(), remote.DefaultLimits(); got != want {
		t.Fatalf("default Limits() = %+v, want %+v", got, want)
	}
	if remote.DefaultMaxImages <= 0 || remote.DefaultMaxPixels <= 0 || remote.DefaultMaxShaders <= 0 || remote.DefaultMaxShaderSourceBytes <= 0 || remote.DefaultMaxUploadBytes <= 0 || remote.DefaultMaxPipelines <= 0 {
		t.Fatalf("DefaultMax* constants must be positive: %+v", remote.DefaultLimits())
	}

	p.SetLimits(remote.Limits{MaxImages: 1})
	got := p.Limits()
	if got.MaxImages != 1 || got.MaxPixels != remote.DefaultMaxPixels || got.MaxShaders != remote.DefaultMaxShaders || got.MaxShaderSourceBytes != remote.DefaultMaxShaderSourceBytes || got.MaxUploadBytes != remote.DefaultMaxUploadBytes || got.MaxPipelines != remote.DefaultMaxPipelines {
		t.Fatalf("Limits() after SetLimits({MaxImages: 1}) = %+v", got)
	}

	// Encode a batch that creates two offscreen images.
	capT := &captureTransport{}
	g := rd.NewGraphics(remote.Info{MaxImageSize: fakedriver.MaxImageSize}, capT)
	if err := g.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := g.Begin(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8); err != nil {
		t.Fatal(err)
	}
	if _, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8); err != nil {
		t.Fatal(err)
	}
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		t.Fatal(err)
	}

	var replayErr error
	frame(t, false, func(host.Frame) {
		defer func() {
			if err := p.Dispose(); err != nil {
				t.Errorf("Dispose: %v", err)
			}
		}()
		_, replayErr = p.Replay(capT.batch, nil)
	})
	if replayErr == nil {
		t.Fatal("Replay succeeded with 2 images despite MaxImages=1, want error")
	}

	// Preflight ScreenSize checks both MaxPixels and the local driver's MaxImageSize.
	encodeScreenBatch := func(w, h, driverMax int) []byte {
		t.Helper()
		ct := &captureTransport{}
		dg := rd.NewGraphics(remote.Info{MaxImageSize: driverMax}, ct)
		if err := dg.Initialize(); err != nil {
			t.Fatal(err)
		}
		surf, err := dg.NewSurface(graphicsdriver.SurfaceDescriptor{Kind: graphicsdriver.SurfaceKindRemote})
		if err != nil {
			t.Fatal(err)
		}
		if err := dg.Begin(); err != nil {
			t.Fatal(err)
		}
		if _, err := surf.NewScreenImage(w, h); err != nil {
			t.Fatal(err)
		}
		if err := dg.End(graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
		return ct.batch
	}

	p2 := remote.NewReplayer()
	// No-screen batch returns 0, 0, nil.
	if w, h, err := p2.ScreenSize(capT.batch); err != nil || w != 0 || h != 0 {
		t.Fatalf("ScreenSize(no screen) = (%d, %d, %v), want (0, 0, nil)", w, h, err)
	}

	// 64x64 unmanaged screen target has physical backing size (64+1 rounded to 16)^2 = 80*80 = 6400
	// pixels: rejected when MaxPixels=4096, accepted when MaxPixels=6400.
	screen64 := encodeScreenBatch(64, 64, fakedriver.MaxImageSize)
	p2.SetLimits(remote.Limits{MaxPixels: 4096})
	if _, _, err := p2.ScreenSize(screen64); err == nil {
		t.Fatal("ScreenSize(64x64) succeeded despite MaxPixels=4096 (< 80*80 backing pixels), want error")
	}
	p2.SetLimits(remote.Limits{MaxPixels: 6400})
	if w, h, err := p2.ScreenSize(screen64); err != nil || w != 64 || h != 64 {
		t.Fatalf("ScreenSize(64x64) with MaxPixels=6400 = (%d, %d, %v), want (64, 64, nil)", w, h, err)
	}

	// Screen exceeding local driver MaxImageSize is rejected even with a large pixel budget.
	oversized := encodeScreenBatch(fakedriver.MaxImageSize+1, 16, fakedriver.MaxImageSize*2)
	p2.SetLimits(remote.DefaultLimits())
	if _, _, err := p2.ScreenSize(oversized); err == nil {
		t.Fatalf("ScreenSize(%dx16) succeeded despite local MaxImageSize=%d, want error", fakedriver.MaxImageSize+1, fakedriver.MaxImageSize)
	}
	frame(t, false, func(host.Frame) {
		if err := p2.Dispose(); err != nil {
			t.Errorf("p2.Dispose: %v", err)
		}
	})
}

// TestReplayerRetire verifies both explicit (*Replayer).Retire and automatic retirement at
// host.Flush across the two-epoch retirement window.
func TestReplayerRetire(t *testing.T) {
	encodeCreateDispose := func(g graphicsdriver.Graphics, ct *captureTransport) []byte {
		t.Helper()
		if err := g.Begin(); err != nil {
			t.Fatal(err)
		}
		img, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8)
		if err != nil {
			t.Fatal(err)
		}
		img.Dispose()
		if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
		return ct.batch
	}

	ct := &captureTransport{}
	dg := rd.NewGraphics(remote.Info{MaxImageSize: fakedriver.MaxImageSize}, ct)
	if err := dg.Initialize(); err != nil {
		t.Fatal(err)
	}
	b1 := encodeCreateDispose(dg, ct)
	b2 := encodeCreateDispose(dg, ct)
	b3 := encodeCreateDispose(dg, ct)
	b4 := encodeCreateDispose(dg, ct)

	p := remote.NewReplayer()
	p.SetLimits(remote.Limits{MaxImages: 1})
	defer func() {
		frame(t, false, func(host.Frame) {
			if err := p.Dispose(); err != nil {
				t.Errorf("Dispose: %v", err)
			}
		})
	}()

	// Within one frame (no Flush/Retire yet): first create+dispose succeeds and pins its quota;
	// a second one in the same epoch fails closed, and after two explicit Retire calls it succeeds.
	frame(t, false, func(host.Frame) {
		if _, err := p.Replay(b1, nil); err != nil {
			t.Fatalf("b1 Replay: %v", err)
		}
		if _, err := p.Replay(b2, nil); err == nil {
			t.Fatalf("b2 Replay in same epoch succeeded despite MaxImages=1, want error")
		}
		p.Retire()
		p.Retire()
		if _, err := p.Replay(b2, nil); err != nil {
			t.Fatalf("b2 Replay after 2 explicit Retire calls: %v", err)
		}
	})
	// The frame() helper above called host.Flush once (epoch 1). One more empty host.Flush
	// advances epoch 2 automatically so b3 succeeds without an explicit Retire call.
	frame(t, false, func(host.Frame) {})
	frame(t, false, func(host.Frame) {
		if _, err := p.Replay(b3, nil); err != nil {
			t.Fatalf("b3 Replay after 2 automatic host.Flush retirements: %v", err)
		}
	})
	// Only 1 automatic Flush happened since b3, so b4 still fails until the second Flush.
	var b4Err error
	frame(t, false, func(host.Frame) {
		_, b4Err = p.Replay(b4, nil)
	})
	if b4Err == nil {
		t.Fatalf("b4 Replay after only 1 Flush succeeded despite 2-epoch retirement window, want error")
	}
	frame(t, false, func(host.Frame) {
		if _, err := p.Replay(b4, nil); err != nil {
			t.Fatalf("b4 Replay after 2nd automatic Flush: %v", err)
		}
	})

	// R2: calling Dispose outside a frame returns an error and must NOT unregister p from
	// automatic retirement. Reusing p after a successful Dispose must also keep automatic
	// retirement active.
	b5 := encodeCreateDispose(dg, ct)
	b6 := encodeCreateDispose(dg, ct)
	b7 := encodeCreateDispose(dg, ct)
	b8 := encodeCreateDispose(dg, ct)
	if err := p.Dispose(); err == nil {
		t.Fatalf("outside-frame Dispose succeeded, want error")
	}
	// Two empty frames retire b4's quota; then b5 succeeds, and two more frames retire b5.
	frame(t, false, func(host.Frame) {})
	frame(t, false, func(host.Frame) {})
	frame(t, false, func(host.Frame) {
		if _, err := p.Replay(b5, nil); err != nil {
			t.Fatalf("b5 Replay after failed Dispose + 2 flushes: %v", err)
		}
	})
	frame(t, false, func(host.Frame) {})
	frame(t, false, func(host.Frame) {
		if _, err := p.Replay(b6, nil); err != nil {
			t.Fatalf("b6 Replay after failed Dispose + 2 automatic flushes: %v", err)
		}
	})
	// Successful Dispose followed by reuse must keep automatic retirement active.
	frame(t, false, func(host.Frame) {
		if err := p.Dispose(); err != nil {
			t.Fatalf("in-frame Dispose: %v", err)
		}
	})
	frame(t, false, func(host.Frame) {
		if _, err := p.Replay(b7, nil); err != nil {
			t.Fatalf("b7 Replay after reuse: %v", err)
		}
	})
	frame(t, false, func(host.Frame) {})
	frame(t, false, func(host.Frame) {
		if _, err := p.Replay(b8, nil); err != nil {
			t.Fatalf("b8 Replay after reuse + 2 automatic flushes: %v", err)
		}
	})
}

// TestReplayerRetireWaitsForConfirmedEnd verifies R3: retirement epochs advance only after the
// driver's final End actually completes without error on the render thread—never while an async
// End is still in flight or when End fails.
func TestReplayerRetireWaitsForConfirmedEnd(t *testing.T) {
	encodeCreateDispose := func(g graphicsdriver.Graphics, ct *captureTransport) []byte {
		t.Helper()
		if err := g.Begin(); err != nil {
			t.Fatal(err)
		}
		img, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8)
		if err != nil {
			t.Fatal(err)
		}
		img.Dispose()
		if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
		return ct.batch
	}
	encodeScreen64 := func(driverMax int) []byte {
		t.Helper()
		ct := &captureTransport{}
		dg := rd.NewGraphics(remote.Info{MaxImageSize: driverMax}, ct)
		if err := dg.Initialize(); err != nil {
			t.Fatal(err)
		}
		surf, err := dg.NewSurface(graphicsdriver.SurfaceDescriptor{Kind: graphicsdriver.SurfaceKindRemote})
		if err != nil {
			t.Fatal(err)
		}
		if err := dg.Begin(); err != nil {
			t.Fatal(err)
		}
		if _, err := surf.NewScreenImage(64, 64); err != nil {
			t.Fatal(err)
		}
		if err := dg.End(graphicsdriver.FlushModeEndFrame); err != nil {
			t.Fatal(err)
		}
		return ct.batch
	}

	ct := &captureTransport{}
	dg := rd.NewGraphics(remote.Info{MaxImageSize: fakedriver.MaxImageSize}, ct)
	if err := dg.Initialize(); err != nil {
		t.Fatal(err)
	}
	b1 := encodeCreateDispose(dg, ct)
	b2 := encodeCreateDispose(dg, ct)
	screen64 := encodeScreen64(fakedriver.MaxImageSize)

	p := remote.NewReplayer()
	// 16x16 offscreen = 256 pixels, 64x64 unmanaged screen = 80x80 = 6400 pixels.
	// With MaxPixels = 6400, screen64 fits ONLY once b1's 256 disposed pixels have retired.
	p.SetLimits(remote.Limits{MaxImages: 1, MaxPixels: 6400})
	defer func() {
		theViewer.setOnSend(nil)
		frame(t, false, func(host.Frame) {
			if err := p.Dispose(); err != nil {
				t.Errorf("Dispose: %v", err)
			}
		})
	}()

	// Allocate scratch before installing the blocking Send hook so the async frame below only
	// enqueues drawTrianglesCommand (NeedsSync() == false), letting Flush(false) return while
	// driver End is still blocked in Send.
	scratch := ggfx.NewImage(8, 8)
	defer scratch.Deallocate()
	frame(t, true, func(host.Frame) {
		scratch.Fill(color.Black)
	})

	// Replay b1 and complete 1 host frame (epoch 1).
	frame(t, true, func(host.Frame) {
		if _, err := p.Replay(b1, nil); err != nil {
			t.Fatalf("b1 Replay: %v", err)
		}
	})
	// Still 1 epoch away from releasing b1's 256 disposed pixels.
	if _, _, err := p.ScreenSize(screen64); err == nil {
		t.Fatalf("ScreenSize succeeded after only 1 completed frame, want pixel limit error")
	}

	// Start a 2nd frame whose driver End blocks in Send. Flush(false) returns asynchronously
	// BEFORE End finishes; ScreenSize must NOT see the retired quota yet.
	sendEntered := make(chan struct{})
	releaseSend := make(chan struct{})
	failErr := errors.New("injected transport End failure")
	theViewer.setOnSend(func(bool) error {
		close(sendEntered)
		<-releaseSend
		return failErr
	})

	fr, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	scratch.Fill(color.White)
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(false); err != nil {
		t.Fatalf("async Flush(false): %v", err)
	}
	<-sendEntered

	// While driver End is blocked on the render thread, quota must NOT have come back.
	if _, _, err := p.ScreenSize(screen64); err == nil {
		close(releaseSend)
		t.Fatalf("ScreenSize succeeded while async driver End was still blocked, want pixel limit error")
	}

	// Unblock Send so driver End FAILS. Synchronize via host.Trim (which uses FlushModeIntermediate
	// + Sync and drains the async error without incrementing completedFrames), then drain the
	// latched error on BeginFrame and verify the failed End did NOT retire b1's quota.
	theViewer.setOnSend(nil)
	close(releaseSend)
	if err := host.Trim(); err == nil {
		t.Fatalf("expected Trim to surface injected async End failure, got nil")
	}
	if _, err := host.BeginFrame(); err == nil {
		t.Fatalf("expected BeginFrame to drain latched Trim error, got nil")
	}
	if _, _, err := p.ScreenSize(screen64); err == nil {
		t.Fatalf("ScreenSize succeeded after a failing driver End, want pixel limit error")
	}

	// Now complete a real 2nd successful frame: quota MUST be released and b2 must succeed.
	frame(t, true, func(host.Frame) {
		scratch.Fill(color.Black)
	})
	if w, h, err := p.ScreenSize(screen64); err != nil || w != 64 || h != 64 {
		t.Fatalf("ScreenSize after 2nd confirmed completed frame = (%d, %d, %v), want (64, 64, nil)", w, h, err)
	}
	frame(t, true, func(host.Frame) {
		if _, err := p.Replay(b2, nil); err != nil {
			t.Fatalf("b2 Replay after 2 confirmed completed frames: %v", err)
		}
	})
}

// TestTrimPreservesPendingDamage verifies R4: calling host.Trim between Frame.End and host.Flush(true)
// does not end the logical frame or drop surface damage set for the pending present, while still
// releasing idle caches and command-queue buffers.
func TestTrimPreservesPendingDamage(t *testing.T) {
	// Prime the screen at 128x128 so the second frame is a kept/partial-damage present.
	frame(t, true, func(f host.Frame) {
		scr := f.Screen(surface, 128, 128)
		scr.Fill(color.RGBA{0x10, 0x20, 0x30, 0xff})
	})

	dst := ggfx.NewImage(128, 128)
	defer dst.Deallocate()
	src := ggfx.NewImage(32, 32)
	defer src.Deallocate()
	src.Fill(color.White)

	wantDamage := image.Rect(12, 24, 60, 80)

	fr, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	scr := fr.Screen(surface, 128, 128)
	surface.SetDamage([]image.Rectangle{wantDamage})
	scr.SubImage(wantDamage).(*ggfx.Image).Fill(color.RGBA{0xff, 0, 0, 0xff})

	// Also allocate stencil cache and large command-queue buffers to confirm Trim still trims.
	var path vector.Path
	path.MoveTo(4, 4)
	path.LineTo(120, 10)
	path.LineTo(90, 110)
	path.Close()
	vector.FillPath(dst, &path, nil, &vector.DrawPathOptions{AntiAlias: true})

	vs := make([]ggfx.Vertex, 2000)
	is := make([]uint32, 3000)
	for i := range 1000 {
		base := uint32(i % 1998)
		is[3*i], is[3*i+1], is[3*i+2] = base, base+1, base+2
	}
	for range 20 {
		dst.DrawTriangles(vs, is, src, nil)
	}
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}

	var before ggfx.DebugInfo
	ggfx.ReadDebugInfo(&before)

	// Trim between Frame.End and host.Flush(true).
	if err := host.Trim(); err != nil {
		t.Fatalf("host.Trim: %v", err)
	}
	if got := vector.StencilBytes(); got != 0 {
		t.Errorf("vector.StencilBytes() after Trim = %d, want 0", got)
	}
	var after ggfx.DebugInfo
	ggfx.ReadDebugInfo(&after)
	if after.QueueBufferBytes >= before.QueueBufferBytes/2 {
		t.Errorf("QueueBufferBytes after Trim = %d (was %d), want < %d", after.QueueBufferBytes, before.QueueBufferBytes, before.QueueBufferBytes/2)
	}

	if _, err := host.Flush(true); err != nil {
		t.Fatalf("Flush(true) after Trim: %v", err)
	}
	if theViewer.err != nil {
		t.Fatalf("viewer error: %v", theViewer.err)
	}
	gotDamage := theViewer.snapshotLastDamage()
	if len(gotDamage) != 1 || gotDamage[0] != wantDamage {
		t.Fatalf("presented damage after Frame.End -> Trim -> Flush(true) = %v, want [%v]", gotDamage, wantDamage)
	}
}
