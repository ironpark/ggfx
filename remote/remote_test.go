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
	"image"
	"image/color"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/host"
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
	g       *fakedriver.Graphics
	r       *rd.Replayer
	target  *rd.Target
	batches int
	present int
	err     error
}

func (v *viewer) replay(batch []byte) error {
	h, err := rd.ParseHeader(batch)
	if err != nil {
		return err
	}
	if h.Screen != 0 && (v.target == nil || v.target.Width != h.Width || v.target.Height != h.Height) {
		v.target = &rd.Target{Image: v.g.NewTarget(h.Width, h.Height), Width: h.Width, Height: h.Height, InternalWidth: h.Width, InternalHeight: h.Height}
	}
	_, err = v.r.Replay(v.g, batch, v.target)
	return err
}

func (v *viewer) Send(batch []byte, present bool) error {
	v.batches++
	if present {
		v.present++
	}
	if err := v.replay(batch); err != nil {
		v.err = err
		return err
	}
	return nil
}

func (v *viewer) ReadPixels(batch []byte, id uint32, regions []image.Rectangle, dsts [][]byte) error {
	if err := v.replay(batch); err != nil {
		return err
	}
	return v.r.ReadPixels(id, regions, dsts, v.target)
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
