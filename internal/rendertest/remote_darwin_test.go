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

package rendertest_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/host"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal"
	"github.com/ironpark/ggfx/remote"
	"github.com/ironpark/ggfx/vector"
)

// The child process of TestRemoteReplayMatchesNative draws through the remote driver and writes
// its batches to the file recordEnv names, for the driver info infoEnv gives in hex.
const (
	recordEnv = "GGFX_RENDERTEST_REMOTE_RECORD"
	infoEnv   = "GGFX_RENDERTEST_REMOTE_INFO"
)

const remoteW, remoteH = 203, 117 // not multiples of 16: the replay must retarget the screen

// remoteScene draws frame n of a scene to dst: fills, a scaled and filtered image, a sub-image, a
// color scale, a path's coverage, and a shader of the user's with uniforms.
func remoteScene(dst, src *ggfx.Image, sh *ggfx.Shader, n int) {
	if n == 0 {
		dst.Fill(color.RGBA{0x20, 0x40, 0x60, 0xff})
	}
	op := &ggfx.DrawImageOptions{Filter: ggfx.FilterLinear}
	op.GeoM.Scale(2.5, 3)
	op.GeoM.Translate(float64(3+7*n), 4)
	op.ColorScale.Scale(1, 0.75, 0.5, 1)
	dst.DrawImage(src, op)
	op = &ggfx.DrawImageOptions{}
	op.GeoM.Translate(150, float64(10+20*n))
	dst.DrawImage(src.SubImage(image.Rect(2, 1, 9, 6)).(*ggfx.Image), op)
	vector.FillCircle(dst, 120, 60, 25+float32(5*n), color.RGBA{0xc0, 0x10, 0x30, 0xc0}, true)
	vector.StrokeLine(dst, 5, 110, 198, float32(20+40*n), 3, color.RGBA{0, 0xff, 0, 0xff}, true)
	sop := &ggfx.DrawRectShaderOptions{Uniforms: map[string]any{"Tint": []float32{1, 0.5, 0.25, 1}}}
	sop.GeoM.Translate(float64(60+n), 70)
	dst.DrawRectShader(31, 23, sh, sop)
}

func remoteSceneSources() (*ggfx.Image, *ggfx.Shader, error) {
	src := ggfx.NewImage(13, 7)
	pix := make([]byte, 4*13*7)
	for i := range pix {
		pix[i] = byte(i * 37)
	}
	for i := 3; i < len(pix); i += 4 {
		pix[i] = 0xff
	}
	src.WritePixels(pix)
	sh, err := ggfx.NewShader([]byte(`
struct Uniforms {
	Tint: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;

fn fragment(v: Vertex) -> vec4f {
	return vec4f(v.position.x / 256.0, v.position.y / 128.0, 0.5, 1.0) * u.Tint;
}
`))
	return src, sh, err
}

// fileTransport writes each batch to a file, length first.
type fileTransport struct{ w io.Writer }

func (t fileTransport) Send(batch []byte, present bool) error {
	if err := binary.Write(t.w, binary.LittleEndian, uint32(len(batch))); err != nil {
		return err
	}
	_, err := t.w.Write(batch)
	return err
}

func (t fileTransport) ReadPixels(batch []byte, id uint32, regions []image.Rectangle, dsts [][]byte) error {
	return errors.New("rendertest: the scene reads no pixels")
}

// recordRemote is the child's TestMain: it draws two frames of the scene on a remote screen,
// writing the batches to path, and checks that it made no Metal device.
func recordRemote(path string) error {
	b, err := hex.DecodeString(os.Getenv(infoEnv))
	if err != nil {
		return err
	}
	var info remote.Info
	if err := info.UnmarshalBinary(b); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := remote.Install(info, fileTransport{w: f}); err != nil {
		return err
	}
	if err := host.Init(nil); err != nil {
		return err
	}
	defer host.Terminate()
	s, err := host.NewSurface(host.RemoteTarget{}, false)
	if err != nil {
		return err
	}
	src, sh, err := remoteSceneSources()
	if err != nil {
		return err
	}
	for n := range 2 {
		fr, err := host.BeginFrame()
		if err != nil {
			return err
		}
		remoteScene(fr.Screen(s, remoteW, remoteH), src, sh, n)
		if err := fr.End(); err != nil {
			return err
		}
		if _, err := host.Flush(true); err != nil {
			return err
		}
	}
	if metal.DeviceCreated() {
		return errors.New("rendertest: the remote app made a Metal device")
	}
	return nil
}

func readBatches(path string) ([][]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var batches [][]byte
	for len(b) > 0 {
		if len(b) < 4 {
			return nil, errors.New("rendertest: a batch cut short")
		}
		n := binary.LittleEndian.Uint32(b)
		if int(n) > len(b)-4 {
			return nil, errors.New("rendertest: a batch cut short")
		}
		// A copy, aligned as a viewer's buffer is.
		batches = append(batches, bytes.Clone(b[4:4+n]))
		b = b[4+n:]
	}
	return batches, nil
}

// TestRemoteReplayMatchesNative draws a scene natively and through the remote driver in a child
// process with no GPU, replays the child's batches here, and checks that the two are the same,
// pixel for pixel.
func TestRemoteReplayMatchesNative(t *testing.T) {
	if !metal.DeviceCreated() {
		t.Fatal("this process draws with Metal, but made no Metal device")
	}
	info, err := remote.LocalInfo()
	if err != nil {
		t.Fatal(err)
	}
	ib, err := info.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "batches")
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), recordEnv+"="+path, infoEnv+"="+hex.EncodeToString(ib))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the remote app: %v\n%s", err, out)
	}
	batches, err := readBatches(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 {
		t.Fatalf("got %d batches, want one a frame", len(batches))
	}

	src, sh, err := remoteSceneSources()
	if err != nil {
		t.Fatal(err)
	}
	defer src.Deallocate()
	defer sh.Deallocate()
	native := ggfx.NewImageWithOptions(image.Rect(0, 0, remoteW, remoteH), &ggfx.NewImageOptions{Unmanaged: true})
	defer native.Deallocate()
	screen := ggfx.NewImageWithOptions(image.Rect(0, 0, remoteW, remoteH), &ggfx.NewImageOptions{Unmanaged: true})
	defer screen.Deallocate()
	r := remote.NewReplayer()
	for n, batch := range batches {
		frame(t, func() {
			remoteScene(native, src, sh, n)
			w, h, err := remote.ScreenSize(batch)
			if err != nil || w != remoteW || h != remoteH {
				t.Fatalf("ScreenSize: %d, %d, %v", w, h, err)
			}
			res, err := r.Replay(batch, screen)
			if err != nil {
				t.Fatal(err)
			}
			if !res.Presented {
				t.Error("a frame's batch presented nothing")
			}
		})
	}

	want := make([]byte, 4*remoteW*remoteH)
	got := make([]byte, len(want))
	frame(t, func() {
		native.ReadPixels(want)
		screen.ReadPixels(got)
		if err := r.Dispose(); err != nil {
			t.Error(err)
		}
	})
	if images, shaders := r.Live(); images != 0 || shaders != 0 {
		t.Errorf("%d images and %d shaders of the app's left after Dispose", images, shaders)
	}
	if bytes.Equal(got, want) {
		return
	}
	var diff int
	first := image.Point{-1, -1}
	for i := 0; i < len(got); i += 4 {
		if !bytes.Equal(got[i:i+4], want[i:i+4]) {
			if diff == 0 {
				first = image.Pt(i/4%remoteW, i/4/remoteW)
			}
			diff++
		}
	}
	t.Errorf("the replay differs from the native draw in %d pixels, the first at %v: %v, want %v",
		diff, first, got[4*(first.Y*remoteW+first.X):][:4], want[4*(first.Y*remoteW+first.X):][:4])
}
