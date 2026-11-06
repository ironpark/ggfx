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

package ggfx

import (
	"fmt"
	"image"
	"image/draw"
	"sync"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// PixelFormat is the format of an image's pixels on the GPU.
type PixelFormat int

const (
	// PixelFormatRGBA is premultiplied-alpha RGBA, 4 bytes a pixel. It is the default.
	PixelFormatRGBA PixelFormat = PixelFormat(graphicsdriver.PixelFormatRGBA8)

	// PixelFormatAlpha is alpha only, 1 byte a pixel on the GPU and in uploads: for monochrome
	// content such as glyph coverage, icon masks and path coverage masks.
	//
	// An alpha image of alpha a is drawn as the premultiplied white (a, a, a, a), so it is drawn
	// exactly as a white-on-transparent RGBA image would be: a ColorScale tints it, and shaders,
	// built-in or not, read it as such without change.
	//
	// Drawing into an alpha image keeps the red channel of the premultiplied result, which is the
	// alpha for white content: draw white coverage into it, such as another alpha image, a white
	// fill or a white vector path. Blends that read the destination's alpha (BlendDestinationOver,
	// BlendSourceIn, BlendSourceOut, BlendSourceAtop, BlendDestinationAtop, BlendXor) see it as 1;
	// the usual ones (BlendSourceOver, BlendCopy, BlendClear, BlendLighter, BlendDestinationIn,
	// BlendDestinationOut) work as on an RGBA image. Fill and Set keep the color's alpha.
	//
	// The image.Image and pixel methods stay RGBA: At, ReadPixels and the like give (a, a, a, a),
	// and WritePixels keeps the alpha of the RGBA pixels given. ReadPixels and WritePixels also take
	// one byte a pixel.
	//
	// Where the graphics driver has no alpha-only textures (OpenGL and DirectX at present), an
	// alpha image is an RGBA image on the GPU holding (a, a, a, a), and behaves the same as long as
	// what is drawn into it is white coverage. [Image.Format] reports PixelFormatAlpha either way.
	PixelFormatAlpha PixelFormat = PixelFormat(graphicsdriver.PixelFormatA8)
)

// String returns the format's name.
func (f PixelFormat) String() string {
	switch f {
	case PixelFormatRGBA:
		return "RGBA"
	case PixelFormatAlpha:
		return "Alpha"
	}
	return fmt.Sprintf("PixelFormat(%d)", int(f))
}

// Format returns the pixel format the image was made with. A sub-image has its image's format.
//
// Format panics if the image is disposed.
func (i *Image) Format() PixelFormat {
	if i.isDisposed() {
		panic("ggfx: the image is already disposed")
	}
	return PixelFormat(i.image.Format())
}

// imageToAlpha returns the alpha bytes of img, one a pixel, rows tightly packed.
//
// imageToAlpha might return img.Pix directly without copying when possible.
func imageToAlpha(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	switch img := img.(type) {
	case *image.Alpha:
		if img.Stride == w && len(img.Pix) == w*h {
			return img.Pix
		}
		bs := make([]byte, w*h)
		for j := range h {
			copy(bs[j*w:(j+1)*w], img.Pix[j*img.Stride:j*img.Stride+w])
		}
		return bs
	case *image.RGBA:
		return alphaFromRGBALike(img.Pix, img.Stride, w, h)
	case *image.NRGBA:
		return alphaFromRGBALike(img.Pix, img.Stride, w, h)
	case *image.Uniform:
		_, _, _, a := img.C.RGBA()
		bs := make([]byte, w*h)
		if a := byte(a >> 8); a != 0 {
			for i := range bs {
				bs[i] = a
			}
		}
		return bs
	}

	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst.Pix
}

// alphaFromRGBALike returns the fourth byte of each pixel of 4-byte pixels.
func alphaFromRGBALike(pix []byte, stride, w, h int) []byte {
	bs := make([]byte, w*h)
	for j := range h {
		row := pix[j*stride : j*stride+4*w]
		for i := range w {
			bs[j*w+i] = row[4*i+3]
		}
	}
	return bs
}

var (
	theAlphaShader     *Shader
	theAlphaShaderOnce sync.Once
)

// alphaShader returns a shader that draws the alpha a of source 0 as (a, a, a, a), which an alpha
// image keeps as a.
func alphaShader() *Shader {
	theAlphaShaderOnce.Do(func() {
		s, err := newShader([]byte(`fn fragment(v: Vertex) -> vec4f {
	return vec4f(src0_at(v.src_pos).a);
}`), "alpha")
		if err != nil {
			panic(fmt.Sprintf("ggfx: compiling the alpha shader failed: %v", err))
		}
		theAlphaShader = s
	})
	return theAlphaShader
}
