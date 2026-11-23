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

// Package drivertest provides a reusable conformance suite for graphicsdriver.Graphics
// implementations (Go backends, fakedriver, remote, and future native/Zig backends).
package drivertest

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"
	"testing"

	"github.com/ironpark/ggfx/internal/builtinshader"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/shader"
)

// Options configures a conformance run.
type Options struct {
	// Run executes fn on the driver's render thread within an active Begin/End batch.
	// If nil, RunSuite uses DefaultRunner(g).
	Run func(fn func(graphicsdriver.Graphics) error) error

	// CheckDrawPixels enables pixel readback verification after DrawTriangles.
	// Set to true for real GPU drivers and false for non-rasterizing fakes like fakedriver.
	CheckDrawPixels bool
}

// DefaultRunner returns a runner that invokes Begin, fn, and End(FlushModeEndFrame) directly on g.
func DefaultRunner(g graphicsdriver.Graphics) func(func(graphicsdriver.Graphics) error) error {
	return func(fn func(graphicsdriver.Graphics) error) error {
		if err := g.Begin(); err != nil {
			return err
		}
		err := fn(g)
		return errors.Join(err, g.End(graphicsdriver.FlushModeEndFrame))
	}
}

// allocVersions returns versions for one draw's uniform blocks. They come from
// the process-wide allocator, so the suite can share a live driver with
// graphicscommand without either mistaking the other's blocks for its own.
func allocVersions() graphicsdriver.UniformVersions {
	return graphicsdriver.UniformVersions{
		Internal: graphicsdriver.NewUniformVersion(),
		User:     graphicsdriver.NewUniformVersion(),
	}
}

// RunSuite verifies the core graphicsdriver.Graphics contract on g.
// Callbacks passed to opts.Run may execute on a dedicated render thread, so
// failures inside them are returned as errors rather than calling t.Fatal.
func RunSuite(t *testing.T, g graphicsdriver.Graphics, opts Options) {
	run := opts.Run
	if run == nil {
		run = DefaultRunner(g)
	}

	t.Run("Capabilities", func(t *testing.T) {
		if err := run(func(g graphicsdriver.Graphics) error {
			var errs []error
			if !g.SupportsPixelFormat(graphicsdriver.PixelFormatRGBA8) {
				errs = append(errs, errors.New("SupportsPixelFormat(RGBA8) = false, want true"))
			}
			if maxSize := g.MaxImageSize(); maxSize < 256 {
				errs = append(errs, fmt.Errorf("MaxImageSize() = %d, want >= 256", maxSize))
			}
			return errors.Join(errs...)
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("RGBA8WriteReadRoundTrip", func(t *testing.T) {
		if err := run(func(g graphicsdriver.Graphics) error {
			const w, h = 19, 13
			img, err := g.NewImage(w, h, graphicsdriver.PixelFormatRGBA8)
			if err != nil {
				return err
			}
			defer img.Dispose()

			src := make([]byte, 4*w*h)
			for i := range src {
				src[i] = byte((i*17 + 23) & 0xff)
			}
			if err := img.WritePixels([]graphicsdriver.PixelsArgs{
				{Pixels: src, Region: image.Rect(0, 0, w, h)},
			}); err != nil {
				return err
			}

			got := make([]byte, len(src))
			if err := img.ReadPixels([]graphicsdriver.PixelsArgs{
				{Pixels: got, Region: image.Rect(0, 0, w, h)},
			}); err != nil {
				return err
			}
			if !bytes.Equal(got, src) {
				return errors.New("full RGBA8 round-trip mismatch")
			}

			// Sub-region overwrite and readback.
			sub := image.Rect(3, 2, 11, 9)
			patch := make([]byte, 4*sub.Dx()*sub.Dy())
			for i := range patch {
				patch[i] = byte((i*31 + 7) & 0xff)
			}
			if err := img.WritePixels([]graphicsdriver.PixelsArgs{
				{Pixels: patch, Region: sub},
			}); err != nil {
				return err
			}
			gotSub := make([]byte, len(patch))
			if err := img.ReadPixels([]graphicsdriver.PixelsArgs{
				{Pixels: gotSub, Region: sub},
			}); err != nil {
				return err
			}
			if !bytes.Equal(gotSub, patch) {
				return errors.New("sub-region RGBA8 round-trip mismatch")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("A8WriteReadRoundTrip", func(t *testing.T) {
		if err := run(func(g graphicsdriver.Graphics) error {
			if !g.SupportsPixelFormat(graphicsdriver.PixelFormatA8) {
				return nil
			}
			const w, h = 17, 11
			img, err := g.NewImage(w, h, graphicsdriver.PixelFormatA8)
			if err != nil {
				return err
			}
			defer img.Dispose()

			src := make([]byte, w*h)
			for i := range src {
				src[i] = byte((i*13 + 5) & 0xff)
			}
			if err := img.WritePixels([]graphicsdriver.PixelsArgs{
				{Pixels: src, Region: image.Rect(0, 0, w, h)},
			}); err != nil {
				return err
			}
			got := make([]byte, len(src))
			if err := img.ReadPixels([]graphicsdriver.PixelsArgs{
				{Pixels: got, Region: image.Rect(0, 0, w, h)},
			}); err != nil {
				return err
			}
			if !bytes.Equal(got, src) {
				return errors.New("A8 round-trip mismatch")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("DrawTrianglesCopyAndScissor", func(t *testing.T) {
		prog, err := shader.CompileUser(builtinshader.ShaderSource(builtinshader.FilterNearest, builtinshader.AddressUnsafe))
		if err != nil {
			t.Fatal(err)
		}
		if err := run(func(g graphicsdriver.Graphics) error {
			const w, h = 16, 16
			src, err := g.NewImage(w, h, graphicsdriver.PixelFormatRGBA8)
			if err != nil {
				return err
			}
			defer src.Dispose()
			dst, err := g.NewImage(w, h, graphicsdriver.PixelFormatRGBA8)
			if err != nil {
				return err
			}
			defer dst.Dispose()

			sh, err := g.NewShader(prog.Descriptor())
			if err != nil {
				return err
			}
			defer sh.Dispose()

			// Fill src with solid red (200, 40, 20, 255) and dst with zeros.
			srcPix := make([]byte, 4*w*h)
			for i := 0; i < len(srcPix); i += 4 {
				srcPix[i], srcPix[i+1], srcPix[i+2], srcPix[i+3] = 200, 40, 20, 255
			}
			dstPix := make([]byte, 4*w*h)
			if err := src.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: srcPix, Region: image.Rect(0, 0, w, h)}}); err != nil {
				return err
			}
			if err := dst.WritePixels([]graphicsdriver.PixelsArgs{{Pixels: dstPix, Region: image.Rect(0, 0, w, h)}}); err != nil {
				return err
			}

			vs := make([]float32, 4*graphics.VertexFloatCount)
			graphics.QuadVerticesFromDstAndSrc(vs, 0, 0, w, h, 0, 0, w, h, 1, 1, 1, 1)
			is := graphics.QuadIndices()
			if err := g.SetVertices(vs, is); err != nil {
				return err
			}

			uniforms := MakeInternalUniforms(prog, w, h, g.FlipsY())
			clip := image.Rect(4, 4, 12, 12)
			var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
			srcs[0] = src
			versions := allocVersions()
			if err := g.DrawTriangles(dst, srcs, sh, []graphicsdriver.DstRegion{{Region: clip, IndexCount: len(is)}}, 0, graphicsdriver.BlendCopy, uniforms, versions); err != nil {
				return err
			}
			// Repeat draw with identical uniform versions to verify driver version caching.
			if err := g.DrawTriangles(dst, srcs, sh, []graphicsdriver.DstRegion{{Region: clip, IndexCount: len(is)}}, 0, graphicsdriver.BlendCopy, uniforms, versions); err != nil {
				return err
			}

			if !opts.CheckDrawPixels {
				return nil
			}
			got := make([]byte, 4*w*h)
			if err := dst.ReadPixels([]graphicsdriver.PixelsArgs{{Pixels: got, Region: image.Rect(0, 0, w, h)}}); err != nil {
				return err
			}
			for y := range h {
				for x := range w {
					off := 4 * (y*w + x)
					inClip := image.Pt(x, y).In(clip)
					var want [4]byte
					if inClip {
						want = [4]byte{200, 40, 20, 255}
					}
					gotPx := [4]byte(got[off : off+4])
					if gotPx != want {
						return fmt.Errorf("pixel (%d,%d) inClip=%v: got %v, want %v", x, y, inClip, gotPx, want)
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

// MakeInternalUniforms returns the uniform block for drawing p into a w x h
// destination: the texture sizes and the projection matrix, with the
// program's own uniforms left zero.
func MakeInternalUniforms(p *shader.Program, w, h int, flipsY bool) []uint32 {
	iw, ih := float32(graphics.InternalImageSize(w)), float32(graphics.InternalImageSize(h))
	u := make([]uint32, graphics.PreservedUniformDwordCount+p.UniformDwordCount)
	u[graphics.DstTextureSizeUniformDwordIndex] = math.Float32bits(iw)
	u[graphics.DstTextureSizeUniformDwordIndex+1] = math.Float32bits(ih)
	u[graphics.SourceTextureSizeUniformDwordIndex] = math.Float32bits(iw)
	u[graphics.SourceTextureSizeUniformDwordIndex+1] = math.Float32bits(ih)
	m := u[graphics.ProjectionMatrixUniformDwordIndex : graphics.ProjectionMatrixUniformDwordIndex+16]
	m[0] = math.Float32bits(2 / iw)
	if flipsY {
		m[5] = math.Float32bits(-2 / ih)
		m[13] = math.Float32bits(1)
	} else {
		m[5] = math.Float32bits(2 / ih)
		m[13] = math.Float32bits(-1)
	}
	m[10] = math.Float32bits(1)
	m[12] = math.Float32bits(-1)
	m[15] = math.Float32bits(1)
	return u
}
