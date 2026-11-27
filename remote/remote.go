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

// Package remote draws an app's frames in another process: the viewer, which owns the window and
// the GPU. The app's renderer runs a driver that encodes its calls instead of making them, and
// ships a batch of them at the end of every frame; the viewer replays each batch on its own
// driver. The app opens no GPU device at all, nothing is read back, and a frame's batch grows with
// what it draws, not with the size of the window.
//
// The app installs the driver before host.Init and draws on a surface made for
// host.RemoteTarget, the viewer's window, as it would on a window of its own:
//
//	var info remote.Info // what the viewer's LocalInfo was, as the viewer sent it
//	if err := info.UnmarshalBinary(fromViewer); err != nil { ... }
//	if err := remote.Install(info, transport); err != nil { ... }
//	if err := host.Init(nil); err != nil { ... }
//	surface, err := host.NewSurface(host.RemoteTarget{}, false)
//
// The viewer keeps a Replayer for each app, and an image of its own for the app's screen, which
// it shows in its window. Inside a frame of its own:
//
//	w, h, err := replayer.ScreenSize(batch)
//	// make screen an unmanaged image of w by h, unless it is one already
//	result, err := replayer.Replay(batch, screen)
//
// Each [host.Flush] automatically advances every live Replayer's retirement epoch by one host
// frame (see [Replayer.Retire]). Viewers that end or present frames without calling [host.Flush]
// must call [Replayer.Retire] explicitly after each frame so disposed resources and staged uploads
// are released from the Replayer's [Limits] budget.
//
// The screen image is the viewer's: it keeps the last frame when the app restarts, and a new app
// draws over it. How the batches travel is the Transport's business; the wire format is
// documented with the driver, in internal/graphicsdriver/remote, and versioned: Info carries the
// version, and each side refuses the other's with a *VersionError when they differ.
package remote

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sync"
	"weak"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/bridge"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	rd "github.com/ironpark/ggfx/internal/graphicsdriver/remote"
	"github.com/ironpark/ggfx/internal/render"
)

// Version is the version of the wire format.
const Version = rd.Version

// Default resource limits for a [Replayer].
const (
	DefaultMaxImages            = rd.DefaultMaxImages
	DefaultMaxPixels            = rd.DefaultMaxPixels
	DefaultMaxShaders           = rd.DefaultMaxShaders
	DefaultMaxShaderSourceBytes = rd.DefaultMaxShaderSourceBytes
	DefaultMaxUploadBytes       = rd.DefaultMaxUploadBytes
	DefaultMaxPipelines         = rd.DefaultMaxPipelines
)

// Limits bounds the logical budget of resources one [Replayer] may let an app make the viewer hold
// or stage across a two-epoch retirement window. Any zero or negative field uses the corresponding
// DefaultMax* value. Exceeding a limit fails [Replayer.Replay] (or [Replayer.ScreenSize] for the
// screen target) with an error.
//
// These limits are a logical budget, not a measured physical GPU or OS memory cap:
//
//   - MaxImages: maximum number of live image IDs tracked by the Replayer (both offscreen images
//     and logical screen image IDs) plus disposed offscreen images awaiting retirement.
//   - MaxPixels: maximum backing pixel area of the active screen target (counted once, via
//     unmanaged backing size with edge padding rounded up to 16 in [Replayer.ScreenSize] preflight
//     and using the target's actual internal backing size in [Replayer.Replay]) plus live and
//     pending-retirement offscreen images.
//   - MaxShaders and MaxShaderSourceBytes: live shaders plus disposed shaders awaiting retirement.
//     MaxShaderSourceBytes (default 1 MiB) is the total WGSL source bytes across all live and
//     pending-retirement shaders combined, not a per-shader cap; there is no compile-time deadline
//     on shader compilation.
//   - MaxPipelines: maximum number of distinct (shader, blend, destination pixel format)
//     combinations drawn with, as a logical bound on pipeline objects the app can cause the driver
//     to build (not measured driver pipeline memory), released with the shader after retirement.
//   - MaxUploadBytes: maximum aggregate payload bytes of explicit WritePixels calls and synthetic
//     NewImage clears across the active two-epoch retirement window (excluding backend staging
//     alignment/padding, vertex/index buffer uploads, and uniform uploads).
//
// Disposed resources and upload payloads remain charged for a two-flush retirement delay.
// [host.Flush] advances the retirement epoch automatically for every live Replayer; callers not
// driving frames through [host.Flush] must call [Replayer.Retire] once after each ended or
// presented host frame. This two-flush window approximates but does not prove backend GPU
// reclamation (for example, D3D12 frees a frame slot's deferred objects only when that slot comes
// round again, and Metal holds unfinished command-buffer generations until the GPU signals
// completion). Until Retire is called, disposed resources remain charged so a host that never
// calls Retire fails closed; [Replayer.Dispose] releases all charges immediately.
//
// In addition to these limits, a Replayer may lazily allocate at most one internal 16×16 RGBA8
// placeholder texture to bind for shader source slots left unbound by a draw. This fixed
// per-Replayer overhead is not counted against MaxImages, MaxPixels, or MaxUploadBytes and is
// freed on [Replayer.Dispose].
type Limits = rd.Limits

// DefaultLimits returns the default resource limits for a [Replayer].
func DefaultLimits() Limits {
	return rd.DefaultLimits()
}

// Info is what the viewer's driver answers to the queries the app's driver is asked. Its binary
// form, which MarshalBinary and UnmarshalBinary make and read, carries the version of the wire
// format.
type Info = rd.Info

// VersionError reports that the two sides speak different versions of the wire format.
type VersionError = rd.VersionError

// Transport carries the app's batches to the viewer. See the methods for what each must do.
type Transport = rd.Transport

// Result is what a replayed batch did.
type Result = rd.Result

var (
	liveReplayersM sync.Mutex
	liveReplayers  = map[weak.Pointer[Replayer]]struct{}{}
)

func init() {
	render.AddPostFlushHook(retireLiveReplayers)
}

func registerReplayer(p *Replayer) {
	wp := weak.Make(p)
	liveReplayersM.Lock()
	liveReplayers[wp] = struct{}{}
	liveReplayersM.Unlock()
}

func retireLiveReplayers() {
	liveReplayersM.Lock()
	if len(liveReplayers) == 0 {
		liveReplayersM.Unlock()
		return
	}
	var alive []*Replayer
	for wp := range liveReplayers {
		if p := wp.Value(); p != nil {
			alive = append(alive, p)
		} else {
			delete(liveReplayers, wp)
		}
	}
	liveReplayersM.Unlock()

	for _, p := range alive {
		p.syncRetire()
	}
}

// Install makes host.Init use the remote driver, which answers its queries as info says and ships
// its batches through t. It is called before host.Init. The process then opens no GPU device: the
// platform's driver is linked but never made.
func Install(info Info, t Transport) error {
	if info.MaxImageSize <= 0 || info.MaxImageSize > math.MaxInt32 {
		return fmt.Errorf("remote: invalid max image size %d", info.MaxImageSize)
	}
	if t == nil {
		return errors.New("remote: no transport")
	}
	return render.SetDriver(rd.NewGraphics(info, t), render.GraphicsLibraryRemote)
}

// LocalInfo returns what this process's driver answers, for the viewer to send an app. It is
// called after host.Init, and on OpenGL after the window system set its context
// (host.SetGLContext); the driver is initialized then if no frame has run yet.
func LocalInfo() (Info, error) {
	g := render.GraphicsDriver()
	if g == nil {
		return Info{}, errors.New("remote: LocalInfo is called before host.Init")
	}
	maxImageSize, a8, err := render.DriverLimits()
	if err != nil {
		return Info{}, err
	}
	return Info{
		ColorSpace:   uint8(g.ColorSpace()),
		FlipsY:       g.FlipsY(),
		A8:           a8,
		MaxImageSize: maxImageSize,
	}, nil
}

// ScreenSize parses batch's header and returns the size of the app's screen as of the end of
// batch, or 0, 0 when the app has none.
//
// ScreenSize performs wire-format validation only and does not check the device's maximum image
// size or a [Replayer]'s pixel budget. A viewer replaying untrusted apps must use
// [Replayer.ScreenSize] before allocating a screen target.
func ScreenSize(batch []byte) (width, height int, err error) {
	h, err := rd.ParseHeader(batch)
	if err != nil {
		return 0, 0, err
	}
	return h.Width, h.Height, nil
}

// Replayer replays one app's batches on the viewer's driver. The app's images and shaders live on
// the viewer's side until Dispose, which the viewer calls when the app goes away.
type Replayer struct {
	mu               sync.Mutex
	r                *rd.Replayer
	lastRetiredFrame uint64
}

// NewReplayer returns the replayer of a new app with [DefaultLimits].
func NewReplayer() *Replayer {
	p := &Replayer{
		r:                rd.NewReplayer(),
		lastRetiredFrame: render.CompletedFrames(),
	}
	registerReplayer(p)
	return p
}

// syncRetireLocked advances p's retirement epoch once for each confirmed completed host frame
// since the last observation (capped at 2 epochs, which clears the retirement ring). p.mu must be
// held (or the caller must hold exclusive access while blocked on the render thread).
func (p *Replayer) syncRetireLocked() {
	cur := render.CompletedFrames()
	if cur <= p.lastRetiredFrame {
		return
	}
	diff := cur - p.lastRetiredFrame
	p.lastRetiredFrame = cur
	if diff > 2 {
		diff = 2
	}
	for range diff {
		p.r.Retire()
	}
}

func (p *Replayer) syncRetire() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.syncRetireLocked()
}

// SetLimits sets the resource limits enforced on subsequent calls to [Replayer.Replay]. Any zero
// or negative field uses the corresponding DefaultMax* value; exceeding a limit fails the replay
// with an error.
func (p *Replayer) SetLimits(limits Limits) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.r.SetLimits(limits)
}

// Limits returns the resource limits currently enforced by p.
func (p *Replayer) Limits() Limits {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.r.Limits()
}

// Retire advances p's retirement epoch by one host flush, crediting charges for resources disposed
// and upload payloads staged since the call before last (a two-flush delay). This two-flush window
// approximates but does not prove backend GPU reclamation.
//
// [host.Flush] calls Retire automatically on every live Replayer after each flushed frame. Callers
// that drive frames without [host.Flush] must call Retire explicitly once after each ended or
// presented host frame; otherwise disposed resources remain charged against [Limits] and
// subsequent replays eventually fail closed.
func (p *Replayer) Retire() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.r.Retire()
}

// ScreenSize parses batch's header and validates its screen size against the local graphics
// driver's maximum image size and p's remaining live pixel budget (together with p's live and
// pending-retirement offscreen images, using the target's physical backing size). Callers should
// call this before allocating a screen image for [Replayer.Replay]. It returns 0, 0, nil when the
// batch names no screen.
//
// Like [LocalInfo], ScreenSize must be called after host.Init (and on desktop OpenGL after
// host.SetGLContext, on the renderer's thread when host.Options.SingleThread is set); if no frame
// has run yet, it initializes the graphics driver.
func (p *Replayer) ScreenSize(batch []byte) (width, height int, err error) {
	maxImageSize, _, err := render.DriverLimits()
	if err != nil {
		return 0, 0, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.syncRetireLocked()
	return p.r.ScreenSize(batch, maxImageSize)
}

// Replay runs batch, after every draw the viewer has made so far, and waits for it. screen is the
// image the app's screen is: an unmanaged image of ScreenSize(batch), or nil when that is 0, 0. A
// batch that fails a check, being malformed, cut short or not of this version, returns an error;
// the viewer should then drop the app. Replay is called inside a frame.
func (p *Replayer) Replay(batch []byte, screen *ggfx.Image) (Result, error) {
	var res Result
	err := p.run(batch, screen, func(g graphicsdriver.Graphics, target *rd.Target) error {
		var err error
		res, err = p.r.Replay(g, batch, target)
		return err
	})
	return res, err
}

// ReadPixels runs batch, as Replay does, and then reads regions of the app's image id into dsts,
// as the app's driver asked with it.
func (p *Replayer) ReadPixels(batch []byte, screen *ggfx.Image, id uint32, regions []image.Rectangle, dsts [][]byte) error {
	return p.run(batch, screen, func(g graphicsdriver.Graphics, target *rd.Target) error {
		if _, err := p.r.Replay(g, batch, target); err != nil {
			return err
		}
		return p.r.ReadPixels(id, regions, dsts, target)
	})
}

// Dispose frees every image and shader of the app's and releases all limit charges. It is called
// inside a frame.
func (p *Replayer) Dispose() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return render.DrawExternally(nil, func(g graphicsdriver.Graphics, _ *atlas.ExternalTarget) error {
		p.r.Dispose()
		return nil
	})
}

// Live returns how many images and shaders of the app's are alive on the viewer's side.
func (p *Replayer) Live() (images, shaders int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.syncRetireLocked()
	return p.r.Live()
}

func (p *Replayer) run(batch []byte, screen *ggfx.Image, f func(graphicsdriver.Graphics, *rd.Target) error) error {
	maxImageSize, _, err := render.DriverLimits()
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.syncRetireLocked()

	w, h, err := p.r.ScreenSize(batch, maxImageSize)
	if err != nil {
		return err
	}
	if w == 0 {
		return render.DrawExternally(nil, func(g graphicsdriver.Graphics, _ *atlas.ExternalTarget) error {
			// Consume any frame that finished on the render thread just before this callback.
			p.syncRetireLocked()
			return f(g, nil)
		})
	}
	if screen == nil {
		return fmt.Errorf("remote: the batch draws to a screen of %dx%d, and no image is given for it", w, h)
	}
	hooks := bridge.Image[*ggfx.Image]()
	if hooks.Original(screen) != screen || screen.Bounds() != image.Rect(0, 0, w, h) {
		return fmt.Errorf("remote: the screen must be an image of %v, not a sub-image or an image of %v", image.Rect(0, 0, w, h), screen.Bounds())
	}
	// Fills of the viewer's own to the image run first, as before any draw to it.
	if pending := hooks.Pending(screen); pending != nil {
		pending.FlushPending(screen)
	}
	return render.DrawExternally(hooks.Render(screen), func(g graphicsdriver.Graphics, t *atlas.ExternalTarget) error {
		if t.Image == nil || t.Origin != (image.Point{}) {
			return errors.New("remote: the screen image is not an image of its own")
		}
		p.syncRetireLocked()
		return f(g, &rd.Target{Image: t.Image, Width: w, Height: h, InternalWidth: t.InternalWidth, InternalHeight: t.InternalHeight})
	})
}
