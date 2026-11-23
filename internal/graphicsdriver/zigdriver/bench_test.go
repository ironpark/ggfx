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
	"image"
	"math"
	"runtime"
	"testing"

	"github.com/ironpark/ggfx/internal/builtinshader"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/shader"
)

func makeBenchUniforms(p *shader.Program, w, h int, flipsY bool) []uint32 {
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

func runBenchDrivers(b *testing.B, fn func(b *testing.B, g graphicsdriver.Graphics)) {
	b.Run("zig", func(b *testing.B) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if resolvedTestLibPath == "" {
			b.Skip("zig driver library not found (set GGFX_DRIVER_LIB)")
		}
		g, err := NewGraphics(Options{LibPath: resolvedTestLibPath})
		if err != nil {
			b.Fatalf("NewGraphics: %v", err)
		}
		defer func() {
			if err := g.Close(); err != nil {
				b.Fatalf("Close: %v", err)
			}
		}()
		fn(b, g)
	})

	if g, ok := newGoBenchDriver(b); ok {
		b.Run("go", func(b *testing.B) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			if c, ok := g.(graphicsdriver.Closer); ok {
				defer func() { _ = c.Close() }()
			}
			fn(b, g)
		})
	}
}

func BenchmarkDraw10k(b *testing.B) {
	prog, err := shader.CompileUser(builtinshader.ShaderSource(builtinshader.FilterNearest, builtinshader.AddressUnsafe))
	if err != nil {
		b.Fatal(err)
	}

	runBenchDrivers(b, func(b *testing.B, g graphicsdriver.Graphics) {
		const w, h = 256, 256
		if err := g.Begin(); err != nil {
			b.Fatal(err)
		}
		src, err := g.NewImage(16, 16, graphicsdriver.PixelFormatRGBA8)
		if err != nil {
			b.Fatal(err)
		}
		defer src.Dispose()
		dst, err := g.NewImage(w, h, graphicsdriver.PixelFormatRGBA8)
		if err != nil {
			b.Fatal(err)
		}
		defer dst.Dispose()
		sh, err := g.NewShader(prog.Descriptor())
		if err != nil {
			b.Fatal(err)
		}
		defer sh.Dispose()
		if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
			b.Fatal(err)
		}

		vs := make([]float32, 4*graphics.VertexFloatCount)
		graphics.QuadVerticesFromDstAndSrc(vs, 0, 0, 16, 16, 0, 0, 16, 16, 1, 1, 1, 1)
		is := graphics.QuadIndices()
		uniforms := makeBenchUniforms(prog, w, h, g.FlipsY())
		var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
		srcs[0] = src
		regions := []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, w, h), IndexCount: len(is)}}
		vers := graphicsdriver.UniformVersions{Internal: 1, User: 1}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := g.Begin(); err != nil {
				b.Fatal(err)
			}
			if err := g.SetVertices(vs, is); err != nil {
				b.Fatal(err)
			}
			for range 10_000 {
				if err := g.DrawTriangles(dst, srcs, sh, regions, 0, graphicsdriver.BlendSourceOver, uniforms, vers); err != nil {
					b.Fatal(err)
				}
			}
			if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkWritePixelsGlyphs(b *testing.B) {
	runBenchDrivers(b, func(b *testing.B, g graphicsdriver.Graphics) {
		format := graphicsdriver.PixelFormatA8
		if !g.SupportsPixelFormat(format) {
			format = graphicsdriver.PixelFormatRGBA8
		}
		bpp := format.BytesPerPixel()

		if err := g.Begin(); err != nil {
			b.Fatal(err)
		}
		atlas, err := g.NewImage(512, 512, format)
		if err != nil {
			b.Fatal(err)
		}
		defer atlas.Dispose()
		if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
			b.Fatal(err)
		}

		const glyphCount = 200
		const gw, gh = 12, 16
		glyphArgs := make([]graphicsdriver.PixelsArgs, glyphCount)
		for i := range glyphCount {
			pix := make([]byte, bpp*gw*gh)
			for j := range pix {
				pix[j] = byte((i + j) & 0xff)
			}
			x := (i % 32) * gw
			y := (i / 32) * gh
			glyphArgs[i] = graphicsdriver.PixelsArgs{
				Pixels: pix,
				Region: image.Rect(x, y, x+gw, y+gh),
			}
		}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := g.Begin(); err != nil {
				b.Fatal(err)
			}
			for i := range glyphCount {
				if err := atlas.WritePixels(glyphArgs[i : i+1]); err != nil {
					b.Fatal(err)
				}
			}
			if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkSubmitEmpty(b *testing.B) {
	runBenchDrivers(b, func(b *testing.B, g graphicsdriver.Graphics) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := g.Begin(); err != nil {
				b.Fatal(err)
			}
			if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkWritePixelsLarge(b *testing.B) {
	runBenchDrivers(b, func(b *testing.B, g graphicsdriver.Graphics) {
		const w, h = 1024, 1024
		if err := g.Begin(); err != nil {
			b.Fatal(err)
		}
		img, err := g.NewImage(w, h, graphicsdriver.PixelFormatRGBA8)
		if err != nil {
			b.Fatal(err)
		}
		defer img.Dispose()
		if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
			b.Fatal(err)
		}

		pix := make([]byte, 4*w*h)
		for i := range pix {
			pix[i] = byte(i)
		}
		args := []graphicsdriver.PixelsArgs{{
			Pixels: pix,
			Region: image.Rect(0, 0, w, h),
		}}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := g.Begin(); err != nil {
				b.Fatal(err)
			}
			if err := img.WritePixels(args); err != nil {
				b.Fatal(err)
			}
			if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
				b.Fatal(err)
			}
		}
	})
}
