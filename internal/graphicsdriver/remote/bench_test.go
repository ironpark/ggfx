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

package remote

import (
	"image"
	"testing"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/shader"
)

type discardTransport struct{}

func (discardTransport) Send([]byte, bool) error { return nil }
func (discardTransport) ReadPixels([]byte, uint32, []image.Rectangle, [][]byte) error {
	return nil
}

type noopDriver struct{}
type noopImage struct{}
type noopShader struct{}

func (noopDriver) Initialize() error                                   { return nil }
func (noopDriver) ColorSpace() color.ColorSpace                        { return color.ColorSpaceSRGB }
func (noopDriver) Begin() error                                        { return nil }
func (noopDriver) End(graphicsdriver.FlushMode) error                  { return nil }
func (noopDriver) SetVertices([]float32, []uint32) error               { return nil }
func (noopDriver) NeedsClearingScreen() bool                           { return false }
func (noopDriver) MaxImageSize() int                                   { return 4096 }
func (noopDriver) FlipsY() bool                                        { return false }
func (noopDriver) SupportsPixelFormat(graphicsdriver.PixelFormat) bool { return true }
func (noopDriver) NewSurface(graphicsdriver.SurfaceDescriptor) (graphicsdriver.Surface, error) {
	return nil, nil
}
func (noopDriver) NewImage(int, int, graphicsdriver.PixelFormat) (graphicsdriver.Image, error) {
	return noopImage{}, nil
}
func (noopDriver) NewShader(*graphicsdriver.ShaderDescriptor) (graphicsdriver.Shader, error) {
	return noopShader{}, nil
}
func (noopDriver) DrawTriangles(graphicsdriver.Image, [graphics.ShaderSrcImageCount]graphicsdriver.Image, graphicsdriver.Shader, []graphicsdriver.DstRegion, int, graphicsdriver.Blend, []uint32, graphicsdriver.UniformVersions) error {
	return nil
}
func (noopImage) Dispose()                                      {}
func (noopImage) ReadPixels([]graphicsdriver.PixelsArgs) error  { return nil }
func (noopImage) WritePixels([]graphicsdriver.PixelsArgs) error { return nil }
func (noopShader) Dispose()                                     {}

func BenchmarkEncodeFrame(b *testing.B) {
	g := NewGraphics(testInfo, discardTransport{})
	s, err := g.NewSurface(graphicsdriver.SurfaceDescriptor{Kind: graphicsdriver.SurfaceKindRemote})
	if err != nil {
		b.Fatal(err)
	}
	screen, err := s.NewScreenImage(screenW, screenH)
	if err != nil {
		b.Fatal(err)
	}
	user, err := shader.Compile([]byte(userShader), graphics.ShaderSrcImageCount)
	if err != nil {
		b.Fatal(err)
	}
	sh, err := g.NewShader(user.Descriptor())
	if err != nil {
		b.Fatal(err)
	}
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		b.Fatal(err)
	}

	vs, is := quads(64)
	uniforms := uniformsFor(user, screenW, screenH, 1)
	regions := []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, screenW, screenH), IndexCount: 6}}
	damage := []image.Rectangle{image.Rect(0, 0, 100, 50)}
	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image

	// Warm up batch buffer.
	_ = g.SetVertices(vs, is)
	for i := range 32 {
		_ = g.DrawTriangles(screen, srcs, sh, regions, i*6, graphicsdriver.BlendSourceOver, uniforms, graphicsdriver.UniformVersions{Internal: 1, User: 2})
	}
	s.(*Surface).SetDamage(damage)
	_ = g.End(graphicsdriver.FlushModePresent)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = g.SetVertices(vs, is)
		for i := range 32 {
			_ = g.DrawTriangles(screen, srcs, sh, regions, i*6, graphicsdriver.BlendSourceOver, uniforms, graphicsdriver.UniformVersions{Internal: 1, User: 2})
		}
		s.(*Surface).SetDamage(damage)
		_ = g.End(graphicsdriver.FlushModePresent)
	}
}

func BenchmarkEncodeWritePixels(b *testing.B) {
	g := NewGraphics(testInfo, discardTransport{})
	img, err := g.NewImage(64, 64, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		b.Fatal(err)
	}
	_ = g.End(graphicsdriver.FlushModeEndFrame)

	args := []graphicsdriver.PixelsArgs{
		{Pixels: make([]byte, 4*64*64), Region: image.Rect(0, 0, 64, 64)},
	}
	_ = img.WritePixels(args)
	_ = g.End(graphicsdriver.FlushModeEndFrame)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = img.WritePixels(args)
		_ = g.End(graphicsdriver.FlushModeEndFrame)
	}
}

func BenchmarkReplayFrame(b *testing.B) {
	rec := &recorder{}
	g := NewGraphics(testInfo, rec)
	s, err := g.NewSurface(graphicsdriver.SurfaceDescriptor{Kind: graphicsdriver.SurfaceKindRemote})
	if err != nil {
		b.Fatal(err)
	}
	screen, err := s.NewScreenImage(screenW, screenH)
	if err != nil {
		b.Fatal(err)
	}
	user, err := shader.Compile([]byte(userShader), graphics.ShaderSrcImageCount)
	if err != nil {
		b.Fatal(err)
	}
	sh, err := g.NewShader(user.Descriptor())
	if err != nil {
		b.Fatal(err)
	}
	if err := g.End(graphicsdriver.FlushModeEndFrame); err != nil {
		b.Fatal(err)
	}

	vs, is := quads(64)
	uniforms := uniformsFor(user, screenW, screenH, 1)
	regions := []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, screenW, screenH), IndexCount: 6}}
	var srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image
	_ = g.SetVertices(vs, is)
	for i := range 32 {
		_ = g.DrawTriangles(screen, srcs, sh, regions, i*6, graphicsdriver.BlendSourceOver, uniforms, graphicsdriver.UniformVersions{Internal: 1, User: 2})
	}
	s.(*Surface).SetDamage([]image.Rectangle{image.Rect(0, 0, 100, 50)})
	_ = g.End(graphicsdriver.FlushModePresent)

	setupBatch := rec.batches[0]
	frameBatch := rec.batches[1]
	r := NewReplayer()
	target := &Target{Image: noopImage{}, Width: screenW, Height: screenH, InternalWidth: 208, InternalHeight: 128}
	if _, err := r.Replay(noopDriver{}, setupBatch, target); err != nil {
		b.Fatal(err)
	}
	if _, err := r.Replay(noopDriver{}, frameBatch, target); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.Replay(noopDriver{}, frameBatch, target); err != nil {
			b.Fatal(err)
		}
	}
}
