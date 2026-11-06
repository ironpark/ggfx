// Copyright 2026 The Ebitengine Authors
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

package ggfx_test

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/host"
	"github.com/ironpark/ggfx/vector"
)

// alphaMask returns a w x h coverage mask whose pixels vary.
func alphaMask(w, h int) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	for j := range h {
		for i := range w {
			m.Pix[j*m.Stride+i] = byte((i*37 + j*91 + i*j*7) % 256)
		}
	}
	return m
}

// whiteOf returns the white-on-transparent RGBA image of the mask m.
func whiteOf(m *image.Alpha) *image.RGBA {
	b := m.Bounds()
	img := image.NewRGBA(b)
	for j := b.Min.Y; j < b.Max.Y; j++ {
		for i := b.Min.X; i < b.Max.X; i++ {
			a := m.AlphaAt(i, j).A
			img.SetRGBA(i, j, color.RGBA{a, a, a, a})
		}
	}
	return img
}

func readRGBA(img *ggfx.Image) []byte {
	b := img.Bounds()
	pix := make([]byte, 4*b.Dx()*b.Dy())
	img.ReadPixels(pix)
	return pix
}

// firstDiff returns the first pixel two RGBA images of width w differ at.
func firstDiff(got, want []byte, w int) (x, y int, g, wa []byte, ok bool) {
	for i := 0; i < len(got); i += 4 {
		if !bytes.Equal(got[i:i+4], want[i:i+4]) {
			p := i / 4
			return p % w, p / w, got[i : i+4], want[i : i+4], true
		}
	}
	return 0, 0, nil, nil, false
}

// compareRGBA reports where RGBA pixels of an image of width w differ by more than tolerance.
func compareRGBA(t *testing.T, got, want []byte, w, tolerance int) {
	t.Helper()
	var n, maxDiff int
	for i := range got {
		d := int(got[i]) - int(want[i])
		d = max(d, -d)
		if d == 0 {
			continue
		}
		n++
		maxDiff = max(maxDiff, d)
		if d > tolerance && n == 1 {
			p := i / 4
			t.Errorf("at (%d, %d): got %v, want %v", p%w, p/w, got[4*p:4*p+4], want[4*p:4*p+4])
		}
	}
	if n > 0 {
		t.Logf("%d of %d bytes differ, by at most %d", n, len(got), maxDiff)
	}
	if maxDiff > tolerance {
		t.Errorf("bytes differ by up to %d, more than %d", maxDiff, tolerance)
	}
}

func newAlpha(w, h int) *ggfx.Image {
	return ggfx.NewImageWithOptions(image.Rect(0, 0, w, h), &ggfx.NewImageOptions{Format: ggfx.PixelFormatAlpha})
}

func a8Native() bool {
	return host.CurrentDriver().Library == host.GraphicsLibraryMetal
}

// TestAlphaImageDrawsAsWhite draws an alpha image and the equivalent white RGBA image the same
// ways: the results are the same pixel for pixel.
func TestAlphaImageDrawsAsWhite(t *testing.T) {
	const w, h = 64, 48
	mask := alphaMask(w, h)

	shader, err := ggfx.NewShader([]byte(`fn fragment(v: Vertex) -> vec4f {
	return src0_at(v.src_pos) * vec4f(0.25, 0.5, 1.0, 1.0);
}`))
	if err != nil {
		t.Fatal(err)
	}

	tint := color.RGBA{0x20, 0x80, 0xc0, 0xe0}
	// linear marks a draw the GPU filters linearly. It samples beyond the source's edges, where a
	// managed image reads what is around it on its atlas page, so it is compared between unmanaged
	// images only, which are at the origin of textures of their own. Its result is compared within
	// 1: two draws of the same RGBA image differ by that much at a few pixels already, as the
	// destinations' positions on their atlas pages shift the interpolated source positions.
	cases := []struct {
		name   string
		linear bool
		draw   func(dst, src *ggfx.Image)
	}{
		{"plain", false, func(dst, src *ggfx.Image) {
			dst.DrawImage(src, nil)
		}},
		{"color scale", false, func(dst, src *ggfx.Image) {
			op := &ggfx.DrawImageOptions{}
			op.ColorScale.ScaleWithColor(tint)
			dst.DrawImage(src, op)
		}},
		{"linear scale up", true, func(dst, src *ggfx.Image) {
			op := &ggfx.DrawImageOptions{Filter: ggfx.FilterLinear}
			op.GeoM.Scale(2.5, 1.75)
			op.GeoM.Rotate(0.3)
			op.ColorScale.ScaleWithColor(tint)
			dst.DrawImage(src, op)
		}},
		{"linear scale down (mipmaps)", true, func(dst, src *ggfx.Image) {
			op := &ggfx.DrawImageOptions{Filter: ggfx.FilterLinear}
			op.GeoM.Scale(0.2, 0.2)
			op.GeoM.Translate(3, 5)
			dst.DrawImage(src, op)
		}},
		{"sub-image", false, func(dst, src *ggfx.Image) {
			op := &ggfx.DrawImageOptions{}
			op.GeoM.Translate(10, 10)
			op.ColorScale.ScaleAlpha(0.5)
			dst.DrawImage(src.SubImage(image.Rect(7, 9, 30, 40)).(*ggfx.Image), op)
		}},
		{"lighter", false, func(dst, src *ggfx.Image) {
			op := &ggfx.DrawImageOptions{Blend: ggfx.BlendLighter}
			op.ColorScale.ScaleWithColor(tint)
			dst.DrawImage(src, op)
		}},
		{"triangles", true, func(dst, src *ggfx.Image) {
			vs := []ggfx.Vertex{
				{DstX: 0, DstY: 0, SrcX: 0, SrcY: 0, ColorR: 1, ColorG: 0, ColorB: 0, ColorA: 1},
				{DstX: 120, DstY: 0, SrcX: w, SrcY: 0, ColorR: 0, ColorG: 1, ColorB: 0, ColorA: 1},
				{DstX: 0, DstY: 90, SrcX: 0, SrcY: h, ColorR: 0, ColorG: 0, ColorB: 1, ColorA: 0.5},
			}
			dst.DrawTriangles(vs, []uint16{0, 1, 2}, src, &ggfx.DrawTrianglesOptions{Filter: ggfx.FilterLinear})
		}},
		{"shader", false, func(dst, src *ggfx.Image) {
			op := &ggfx.DrawRectShaderOptions{}
			op.Images[0] = src
			op.GeoM.Translate(4, 4)
			dst.DrawRectShader(w, h, shader, op)
		}},
	}
	for _, unmanaged := range []bool{false, true} {
		alpha := ggfx.NewImageFromImageWithOptions(mask, &ggfx.NewImageFromImageOptions{Format: ggfx.PixelFormatAlpha, Unmanaged: unmanaged})
		white := ggfx.NewImageFromImageWithOptions(whiteOf(mask), &ggfx.NewImageFromImageOptions{Unmanaged: unmanaged})
		if got := alpha.Format(); got != ggfx.PixelFormatAlpha {
			t.Fatalf("Format(): got %s, want Alpha", got)
		}
		name := "managed"
		if unmanaged {
			name = "unmanaged"
		}
		for _, c := range cases {
			if c.linear && !unmanaged {
				continue
			}
			t.Run(name+"/"+c.name, func(t *testing.T) {
				const dw, dh = 160, 120
				bg := color.RGBA{0x10, 0x30, 0x20, 0x80}
				got := ggfx.NewImage(dw, dh)
				got.Fill(bg)
				want := ggfx.NewImage(dw, dh)
				want.Fill(bg)
				c.draw(got, alpha)
				c.draw(want, white)
				tolerance := 0
				if c.linear {
					tolerance = 1
				}
				compareRGBA(t, readRGBA(got), readRGBA(want), dw, tolerance)
			})
		}
	}
}

func TestAlphaImagePixels(t *testing.T) {
	const w, h = 3, 2
	img := newAlpha(w, h)

	// One byte a pixel.
	img.WritePixels([]byte{0, 1, 2, 0x80, 0xfe, 0xff})
	a := make([]byte, w*h)
	img.ReadPixels(a)
	if want := []byte{0, 1, 2, 0x80, 0xfe, 0xff}; !bytes.Equal(a, want) {
		t.Errorf("ReadPixels (alpha): got %v, want %v", a, want)
	}
	rgba := make([]byte, 4*w*h)
	img.ReadPixels(rgba)
	if want := []byte{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 0x80, 0x80, 0x80, 0x80, 0xfe, 0xfe, 0xfe, 0xfe, 0xff, 0xff, 0xff, 0xff}; !bytes.Equal(rgba, want) {
		t.Errorf("ReadPixels (RGBA): got %v, want %v", rgba, want)
	}
	if got, want := img.At(2, 1), (color.RGBA{0xff, 0xff, 0xff, 0xff}); got != want {
		t.Errorf("At(2, 1): got %v, want %v", got, want)
	}

	// RGBA pixels keep their alpha.
	img.WritePixels([]byte{
		0xff, 0, 0, 0x10, 0, 0, 0, 0x20, 1, 2, 3, 0x30,
		0, 0, 0, 0, 0x40, 0x40, 0x40, 0x40, 9, 9, 9, 0xff,
	})
	img.ReadPixels(a)
	if want := []byte{0x10, 0x20, 0x30, 0, 0x40, 0xff}; !bytes.Equal(a, want) {
		t.Errorf("ReadPixels after RGBA WritePixels: got %v, want %v", a, want)
	}

	// Set keeps the alpha.
	img.Set(1, 1, color.NRGBA{0, 0xff, 0, 0x99})
	if got, want := img.At(1, 1), (color.RGBA{0x99, 0x99, 0x99, 0x99}); got != want {
		t.Errorf("At(1, 1) after Set: got %v, want %v", got, want)
	}

	// Fill keeps the alpha.
	img.Fill(color.NRGBA{0xff, 0, 0, 0x80})
	img.ReadPixels(a)
	if want := bytes.Repeat([]byte{0x80}, w*h); !bytes.Equal(a, want) {
		t.Errorf("ReadPixels after Fill: got %v, want %v", a, want)
	}
	img.Clear()
	img.ReadPixels(a)
	if want := make([]byte, w*h); !bytes.Equal(a, want) {
		t.Errorf("ReadPixels after Clear: got %v, want %v", a, want)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("WritePixels with a wrong length did not panic")
			}
		}()
		img.WritePixels(make([]byte, 2*w*h))
	}()
}

func TestAlphaImageUnmanaged(t *testing.T) {
	img := ggfx.NewImageWithOptions(image.Rect(0, 0, 4, 4), &ggfx.NewImageOptions{Format: ggfx.PixelFormatAlpha, Unmanaged: true})
	pix := make([]byte, 16)
	for i := range pix {
		pix[i] = byte(i * 16)
	}
	img.WritePixels(pix)
	got := make([]byte, 16)
	img.ReadPixels(got)
	if !bytes.Equal(got, pix) {
		t.Errorf("got %v, want %v", got, pix)
	}
}

// TestAlphaImageDrawInto draws white coverage into an alpha image: it keeps what an RGBA image
// keeps in its alpha (and red).
func TestAlphaImageDrawInto(t *testing.T) {
	const w, h = 32, 24
	mask := alphaMask(w, h)
	whiteSrc := ggfx.NewImageFromImage(whiteOf(mask))
	alphaSrc := ggfx.NewImageFromImageWithOptions(mask, &ggfx.NewImageFromImageOptions{Format: ggfx.PixelFormatAlpha})

	for _, src := range []struct {
		name string
		img  *ggfx.Image
	}{{"white RGBA", whiteSrc}, {"alpha", alphaSrc}} {
		for _, blend := range []struct {
			name  string
			blend ggfx.Blend
		}{{"source-over", ggfx.BlendSourceOver}, {"copy", ggfx.BlendCopy}, {"lighter", ggfx.BlendLighter}, {"destination-out", ggfx.BlendDestinationOut}} {
			t.Run(src.name+"/"+blend.name, func(t *testing.T) {
				got := newAlpha(w, h)
				got.Fill(color.Alpha{0x30})
				want := ggfx.NewImage(w, h)
				want.Fill(color.Alpha{0x30})
				for _, dst := range []*ggfx.Image{got, want} {
					op := &ggfx.DrawImageOptions{Blend: blend.blend, Filter: ggfx.FilterLinear}
					op.GeoM.Translate(2.5, 1.5)
					op.ColorScale.ScaleAlpha(0.75)
					dst.DrawImage(src.img, op)
				}
				ga := make([]byte, w*h)
				got.ReadPixels(ga)
				wp := readRGBA(want)
				for i := range ga {
					if ga[i] != wp[4*i+3] {
						t.Fatalf("at (%d, %d): got %d, want %d", i%w, i/w, ga[i], wp[4*i+3])
					}
				}

				// Drawing the result gives the same as drawing the RGBA result.
				gd := ggfx.NewImage(w, h)
				wd := ggfx.NewImage(w, h)
				op := &ggfx.DrawImageOptions{}
				op.ColorScale.ScaleWithColor(color.RGBA{0, 0x40, 0x80, 0xff})
				gd.DrawImage(got, op)
				wd.DrawImage(want, op)
				if x, y, g, wa, ok := firstDiff(readRGBA(gd), readRGBA(wd), w); ok {
					t.Errorf("drawn at (%d, %d): got %v, want %v", x, y, g, wa)
				}
			})
		}
	}
}

func TestAlphaImageVectorFill(t *testing.T) {
	const w, h = 40, 40
	var p vector.Path
	p.MoveTo(5, 3)
	p.LineTo(36, 12)
	p.QuadTo(20, 40, 4, 30)
	p.Close()

	got := newAlpha(w, h)
	want := ggfx.NewImage(w, h)
	for _, dst := range []*ggfx.Image{got, want} {
		vector.FillPath(dst, &p, nil, &vector.DrawPathOptions{AntiAlias: true})
	}
	ga := make([]byte, w*h)
	got.ReadPixels(ga)
	wp := readRGBA(want)
	var covered int
	for i := range ga {
		if ga[i] != wp[4*i+3] {
			t.Fatalf("at (%d, %d): got %d, want %d", i%w, i/w, ga[i], wp[4*i+3])
		}
		if ga[i] != 0 {
			covered++
		}
	}
	if covered == 0 {
		t.Error("the path covered nothing")
	}
}

func TestNewImageFromImageAlpha(t *testing.T) {
	const w, h = 5, 4
	mask := alphaMask(w, h)
	want := mask.Pix

	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i, a := range mask.Pix {
		nrgba.Pix[4*i], nrgba.Pix[4*i+1], nrgba.Pix[4*i+3] = 0xff, 0x10, a
	}
	colored := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, a := range mask.Pix {
		colored.Pix[4*i+2], colored.Pix[4*i+3] = a/2, a // Blue at half, red zero.
	}
	big := alphaMask(w+3, h+2)
	sub := big.SubImage(image.Rect(2, 1, 2+w, 1+h)).(*image.Alpha)
	subWant := make([]byte, 0, w*h)
	for j := 1; j < 1+h; j++ {
		subWant = append(subWant, big.Pix[j*big.Stride+2:j*big.Stride+2+w]...)
	}

	for _, c := range []struct {
		name string
		src  image.Image
		want []byte
	}{
		{"Alpha", mask, want},
		{"Alpha sub-image", sub, subWant},
		{"RGBA", whiteOf(mask), want},
		{"RGBA colored", colored, want},
		{"NRGBA", nrgba, want},
		{"Uniform", image.NewUniform(color.NRGBA{0xff, 0, 0, 0x42}), nil},
		{"Gray", image.NewGray(image.Rect(0, 0, w, h)), bytes.Repeat([]byte{0xff}, w*h)},
		{"ggfx RGBA colored", ggfx.NewImageFromImage(colored), want},
		{"ggfx alpha", ggfx.NewImageFromImageWithOptions(mask, &ggfx.NewImageFromImageOptions{Format: ggfx.PixelFormatAlpha}), want},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := c.src
			want := c.want
			if u, ok := src.(*image.Uniform); ok {
				// A Uniform's bounds are infinite: take a part.
				src = &boundedImage{u, image.Rect(0, 0, w, h)}
				want = bytes.Repeat([]byte{0x42}, w*h)
			}
			img := ggfx.NewImageFromImageWithOptions(src, &ggfx.NewImageFromImageOptions{Format: ggfx.PixelFormatAlpha})
			if img.Format() != ggfx.PixelFormatAlpha {
				t.Errorf("Format(): got %s", img.Format())
			}
			got := make([]byte, w*h)
			img.ReadPixels(got)
			if !bytes.Equal(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}

	t.Run("PreserveBounds", func(t *testing.T) {
		src := ggfx.NewImageFromImage(colored).SubImage(image.Rect(1, 1, 4, 3)).(*ggfx.Image)
		img := ggfx.NewImageFromImageWithOptions(src, &ggfx.NewImageFromImageOptions{Format: ggfx.PixelFormatAlpha, PreserveBounds: true})
		if got, want := img.Bounds(), image.Rect(1, 1, 4, 3); got != want {
			t.Fatalf("Bounds(): got %v, want %v", got, want)
		}
		for j := 1; j < 3; j++ {
			for i := 1; i < 4; i++ {
				a := mask.Pix[j*w+i]
				if got, want := img.At(i, j), (color.RGBA{a, a, a, a}); got != want {
					t.Errorf("At(%d, %d): got %v, want %v", i, j, got, want)
				}
			}
		}
	})
}

// boundedImage is an image.Image of an unbounded one within bounds.
type boundedImage struct {
	image.Image
	bounds image.Rectangle
}

func (b *boundedImage) Bounds() image.Rectangle { return b.bounds }

func TestAlphaSubImage(t *testing.T) {
	const w, h = 8, 6
	mask := alphaMask(w, h)
	img := ggfx.NewImageFromImageWithOptions(mask, &ggfx.NewImageFromImageOptions{Format: ggfx.PixelFormatAlpha})
	r := image.Rect(2, 1, 6, 4)
	sub := img.SubImage(r).(*ggfx.Image)
	if sub.Format() != ggfx.PixelFormatAlpha {
		t.Errorf("sub-image Format(): got %s", sub.Format())
	}

	got := make([]byte, r.Dx()*r.Dy())
	sub.ReadPixels(got)
	var want []byte
	for j := r.Min.Y; j < r.Max.Y; j++ {
		want = append(want, mask.Pix[j*w+r.Min.X:j*w+r.Max.X]...)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("ReadPixels: got %v, want %v", got, want)
	}
	if a := mask.Pix[2*w+3]; sub.At(3, 2) != (color.RGBA{a, a, a, a}) {
		t.Errorf("At(3, 2): got %v, want %d", sub.At(3, 2), a)
	}

	// Writing to the sub-image writes only its part.
	sub.WritePixels(bytes.Repeat([]byte{0x77}, r.Dx()*r.Dy()))
	all := make([]byte, w*h)
	img.ReadPixels(all)
	for j := range h {
		for i := range w {
			want := mask.Pix[j*w+i]
			if image.Pt(i, j).In(r) {
				want = 0x77
			}
			if all[j*w+i] != want {
				t.Errorf("(%d, %d): got %d, want %d", i, j, all[j*w+i], want)
			}
		}
	}

	// Filling a sub-image of an alpha image.
	sub.Fill(color.Alpha{0x11})
	if got, want := img.At(2, 1), (color.RGBA{0x11, 0x11, 0x11, 0x11}); got != want {
		t.Errorf("At(2, 1) after Fill: got %v, want %v", got, want)
	}
	if a := mask.Pix[0]; img.At(0, 0) != (color.RGBA{a, a, a, a}) {
		t.Errorf("At(0, 0) after Fill: got %v, want %d", img.At(0, 0), a)
	}
}

// TestAlphaImagesShareAtlasPage puts managed alpha images on the atlas: where the driver has
// alpha textures, they share a page of their own.
func TestAlphaImagesShareAtlasPage(t *testing.T) {
	const n = 8
	var imgs []*ggfx.Image
	for i := range n {
		img := newAlpha(10+i, 12)
		img.Fill(color.Alpha{byte(20 * (i + 1))})
		imgs = append(imgs, img)
	}
	// Drawing them all reads each from its page.
	dst := ggfx.NewImage(200, 20)
	for i, img := range imgs {
		op := &ggfx.DrawImageOptions{}
		op.GeoM.Translate(float64(24*i), 0)
		dst.DrawImage(img, op)
	}
	for i := range imgs {
		a := byte(20 * (i + 1))
		if got, want := dst.At(24*i+1, 1), (color.RGBA{a, a, a, a}); got != want {
			t.Errorf("image %d: got %v, want %v", i, got, want)
		}
	}

	var d ggfx.DebugInfo
	ggfx.ReadDebugInfo(&d)
	maxImages := 0
	for _, p := range d.AtlasPages {
		if p.Format == ggfx.PixelFormatAlpha {
			maxImages = max(maxImages, p.Images)
			if want := int64(p.Width) * int64(p.Height); p.Bytes < want || p.Bytes >= 4*want {
				t.Errorf("an alpha page of %dx%d takes %d bytes", p.Width, p.Height, p.Bytes)
			}
		}
	}
	if a8Native() {
		if maxImages < n {
			t.Errorf("the most images on an alpha page: got %d, want at least %d", maxImages, n)
		}
	} else if maxImages != 0 {
		t.Errorf("an alpha page exists on a driver without alpha textures")
	}
}
