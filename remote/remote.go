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
//	w, h, err := remote.ScreenSize(batch)
//	// make screen an unmanaged image of w by h, unless it is one already
//	result, err := replayer.Replay(batch, screen)
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

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/bridge"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	rd "github.com/ironpark/ggfx/internal/graphicsdriver/remote"
	"github.com/ironpark/ggfx/internal/render"
)

// Version is the version of the wire format.
const Version = rd.Version

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

// Install makes host.Init use the remote driver, which answers its queries as info says and ships
// its batches through t. It is called before host.Init. The process then opens no GPU device: the
// platform's driver is linked but never made.
func Install(info Info, t Transport) error {
	if info.MaxImageSize <= 0 {
		return fmt.Errorf("remote: invalid max image size %d", info.MaxImageSize)
	}
	if t == nil {
		return errors.New("remote: no transport")
	}
	return render.SetDriver(rd.NewGraphics(info, t), render.GraphicsLibraryRemote)
}

// LocalInfo returns what this process's driver answers, for the viewer to send an app. It is
// called after host.Init.
func LocalInfo() (Info, error) {
	g := render.GraphicsDriver()
	if g == nil {
		return Info{}, errors.New("remote: LocalInfo is called before host.Init")
	}
	return Info{
		ColorSpace:   uint8(g.ColorSpace()),
		FlipsY:       g.FlipsY(),
		A8:           g.SupportsPixelFormat(graphicsdriver.PixelFormatA8),
		MaxImageSize: g.MaxImageSize(),
	}, nil
}

// ScreenSize returns the size of the app's screen as of the end of batch, or 0, 0 when the app
// has none: the size the image given to Replay must be.
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
	r *rd.Replayer
}

// NewReplayer returns the replayer of a new app.
func NewReplayer() *Replayer {
	return &Replayer{r: rd.NewReplayer()}
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

// Dispose frees every image and shader of the app's. It is called inside a frame.
func (p *Replayer) Dispose() error {
	return render.DrawExternally(nil, func(g graphicsdriver.Graphics, _ *atlas.ExternalTarget) error {
		p.r.Dispose()
		return nil
	})
}

// Live returns how many images and shaders of the app's are alive on the viewer's side.
func (p *Replayer) Live() (images, shaders int) {
	return p.r.Live()
}

func (p *Replayer) run(batch []byte, screen *ggfx.Image, f func(graphicsdriver.Graphics, *rd.Target) error) error {
	w, h, err := ScreenSize(batch)
	if err != nil {
		return err
	}
	if w == 0 {
		return render.DrawExternally(nil, func(g graphicsdriver.Graphics, _ *atlas.ExternalTarget) error {
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
		return f(g, &rd.Target{Image: t.Image, Width: w, Height: h, InternalWidth: t.InternalWidth, InternalHeight: t.InternalHeight})
	})
}
