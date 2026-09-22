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
	"image"
	"image/color"
	"testing"

	"github.com/ironpark/ggfx"
)

func TestImageDrawTrianglesWithStencilBufferOnEmptyDestination(t *testing.T) {
	src := ggfx.NewImage(1, 1)
	src.Fill(color.White)

	shader, err := ggfx.NewShader([]byte(`//kage:unit pixels

package main

func Fragment(dstPos vec4, src0Pos vec2, color vec4) vec4 {
	return color
}
`))
	if err != nil {
		t.Fatal(err)
	}

	vs := []ggfx.Vertex{{DstX: 0, DstY: 0}, {DstX: 1, DstY: 0}, {DstX: 0, DstY: 1}}
	is := []uint32{0, 1, 2}

	for _, tc := range []struct {
		name string
		draw func(dst *ggfx.Image)
	}{
		{
			name: "FillRule",
			draw: func(dst *ggfx.Image) {
				dst.DrawTriangles32(vs, is, src, &ggfx.DrawTrianglesOptions{FillRule: ggfx.FillRuleNonZero})
			},
		},
		{
			name: "AntiAlias",
			draw: func(dst *ggfx.Image) {
				dst.DrawTriangles32(vs, is, src, &ggfx.DrawTrianglesOptions{AntiAlias: true})
			},
		},
		{
			name: "ShaderFillRule",
			draw: func(dst *ggfx.Image) {
				dst.DrawTrianglesShader32(vs, is, shader, &ggfx.DrawTrianglesShaderOptions{FillRule: ggfx.FillRuleNonZero})
			},
		},
		{
			name: "ShaderAntiAlias",
			draw: func(dst *ggfx.Image) {
				dst.DrawTrianglesShader32(vs, is, shader, &ggfx.DrawTrianglesShaderOptions{AntiAlias: true})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ggfx.ResetStencilBufferImagesForTesting()
			dst := ggfx.NewImage(1, 1).SubImage(image.Rectangle{}).(*ggfx.Image)
			tc.draw(dst)
		})
	}
}

func TestImageDrawTrianglesWithStencilBufferOnEmptyDestinationValidatesIndices(t *testing.T) {
	src := ggfx.NewImage(1, 1)
	shader, err := ggfx.NewShader([]byte(`//kage:unit pixels

package main

func Fragment(dstPos vec4, src0Pos vec2, color vec4) vec4 {
	return color
}
`))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		draw func(dst *ggfx.Image)
	}{
		{
			name: "DrawTriangles",
			draw: func(dst *ggfx.Image) {
				dst.DrawTriangles32(nil, []uint32{0}, src, &ggfx.DrawTrianglesOptions{AntiAlias: true})
			},
		},
		{
			name: "DrawTrianglesShader",
			draw: func(dst *ggfx.Image) {
				dst.DrawTrianglesShader32(nil, []uint32{0}, shader, &ggfx.DrawTrianglesShaderOptions{AntiAlias: true})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := ggfx.NewImage(1, 1).SubImage(image.Rectangle{}).(*ggfx.Image)
			defer func() {
				if r := recover(); r == nil {
					t.Error("a draw with an invalid index count must panic")
				}
			}()
			tc.draw(dst)
		})
	}
}

func TestImageDrawTrianglesWithStencilBufferOnSubImage(t *testing.T) {
	whiteImage := ggfx.NewImage(3, 3)
	whiteImage.Fill(color.White)
	whiteSubImage := whiteImage.SubImage(image.Rect(1, 1, 2, 2)).(*ggfx.Image)

	shader, err := ggfx.NewShader([]byte(`//kage:unit pixels

package main

func Fragment(dstPos vec4, src0Pos vec2, color vec4) vec4 {
	return vec4(1, 0, 0, 1)
}
`))
	if err != nil {
		t.Fatal(err)
	}

	const size = 20
	// The sub-image's origin is at least as large as its own size, so that a
	// draw lost by keeping the destination's coordinate space cannot
	// accidentally fall inside the sub-image.
	dstMin := image.Pt(40, 30)

	vertices := func(ox, oy int) []ggfx.Vertex {
		v := func(x, y int) ggfx.Vertex {
			return ggfx.Vertex{
				DstX:   float32(ox + x),
				DstY:   float32(oy + y),
				SrcX:   1,
				SrcY:   1,
				ColorR: 1,
				ColorG: 1,
				ColorB: 1,
				ColorA: 1,
			}
		}
		return []ggfx.Vertex{
			v(2, 2),
			v(18, 2),
			v(2, 18),
			v(4, 4),
			v(10, 4),
			v(4, 10),
		}
	}
	// The second triangle is inside the first one with the same winding
	// order, so that FillRuleNonZero and FillRuleEvenOdd result in different
	// pixels.
	is := []uint32{0, 1, 2, 3, 4, 5}

	dt := func(options *ggfx.DrawTrianglesOptions) func(*ggfx.Image, int, int) {
		return func(dst *ggfx.Image, ox, oy int) {
			dst.DrawTriangles32(vertices(ox, oy), is, whiteSubImage, options)
		}
	}
	dts := func(options *ggfx.DrawTrianglesShaderOptions) func(*ggfx.Image, int, int) {
		return func(dst *ggfx.Image, ox, oy int) {
			options.Images[0] = whiteSubImage
			dst.DrawTrianglesShader32(vertices(ox, oy), is, shader, options)
		}
	}

	for _, tc := range []struct {
		name string
		draw func(dst *ggfx.Image, ox, oy int)
	}{
		{
			name: "FillRuleNonZero",
			draw: dt(&ggfx.DrawTrianglesOptions{
				FillRule: ggfx.FillRuleNonZero,
			}),
		},
		{
			name: "FillRuleEvenOdd",
			draw: dt(&ggfx.DrawTrianglesOptions{
				FillRule: ggfx.FillRuleEvenOdd,
			}),
		},
		{
			name: "AntiAlias",
			draw: dt(&ggfx.DrawTrianglesOptions{
				AntiAlias: true,
			}),
		},
		{
			name: "AntiAliasFillRuleNonZero",
			draw: dt(&ggfx.DrawTrianglesOptions{
				AntiAlias: true,
				FillRule:  ggfx.FillRuleNonZero,
			}),
		},
		{
			name: "AntiAliasBlendCopy",
			draw: dt(&ggfx.DrawTrianglesOptions{
				AntiAlias: true,
				Blend:     ggfx.BlendCopy,
			}),
		},
		{
			name: "ShaderFillRuleNonZero",
			draw: dts(&ggfx.DrawTrianglesShaderOptions{
				FillRule: ggfx.FillRuleNonZero,
			}),
		},
		{
			name: "ShaderFillRuleEvenOdd",
			draw: dts(&ggfx.DrawTrianglesShaderOptions{
				FillRule: ggfx.FillRuleEvenOdd,
			}),
		},
		{
			name: "ShaderAntiAlias",
			draw: dts(&ggfx.DrawTrianglesShaderOptions{
				AntiAlias: true,
			}),
		},
		{
			name: "ShaderAntiAliasFillRuleNonZero",
			draw: dts(&ggfx.DrawTrianglesShaderOptions{
				AntiAlias: true,
				FillRule:  ggfx.FillRuleNonZero,
			}),
		},
		{
			name: "ShaderAntiAliasBlendCopy",
			draw: dts(&ggfx.DrawTrianglesShaderOptions{
				AntiAlias: true,
				Blend:     ggfx.BlendCopy,
			}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			background := color.RGBA{0, 0, 0, 0xff}

			dstPlain := ggfx.NewImage(size, size)
			dstPlain.Fill(background)

			base := ggfx.NewImage(dstMin.X+size, dstMin.Y+size)
			base.Fill(background)
			dstSub := base.SubImage(image.Rectangle{Min: dstMin, Max: dstMin.Add(image.Pt(size, size))}).(*ggfx.Image)

			tc.draw(dstPlain, 0, 0)
			tc.draw(dstSub, dstMin.X, dstMin.Y)

			for j := range size {
				for i := range size {
					want := dstPlain.At(i, j).(color.RGBA)
					got := dstSub.At(dstMin.X+i, dstMin.Y+j).(color.RGBA)
					if got != want {
						// Report only the first mismatch to keep the log readable.
						t.Errorf("pixel (%d, %d) of the sub-image: got %v, want %v", i, j, got, want)
						return
					}
				}
			}
		})
	}
}

func TestImageDrawTrianglesWithStencilBufferWithEmptyIndices(t *testing.T) {
	src := ggfx.NewImage(3, 3)
	src.Fill(color.White)

	shader, err := ggfx.NewShader([]byte(`//kage:unit pixels

package main

func Fragment(dstPos vec4, src0Pos vec2, color vec4) vec4 {
	return color
}
`))
	if err != nil {
		t.Fatal(err)
	}

	// BlendCopy with a FillRule draw must not clear the destination even if
	// there is nothing to draw.
	for _, tc := range []struct {
		name string
		draw func(dst *ggfx.Image)
	}{
		{
			name: "FillRuleBlendCopy",
			draw: func(dst *ggfx.Image) {
				dst.DrawTriangles32(nil, nil, src, &ggfx.DrawTrianglesOptions{
					FillRule:      ggfx.FillRuleNonZero,
					CompositeMode: ggfx.CompositeModeCustom,
					Blend:         ggfx.BlendCopy,
				})
			},
		},
		{
			name: "ShaderFillRuleBlendCopy",
			draw: func(dst *ggfx.Image) {
				dst.DrawTrianglesShader32(nil, nil, shader, &ggfx.DrawTrianglesShaderOptions{
					FillRule:      ggfx.FillRuleNonZero,
					CompositeMode: ggfx.CompositeModeCustom,
					Blend:         ggfx.BlendCopy,
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := color.RGBA{0xff, 0, 0, 0xff}
			dst := ggfx.NewImage(3, 3)
			dst.Fill(want)

			// This must be a no-op.
			tc.draw(dst)

			for j := range 3 {
				for i := range 3 {
					if got := dst.At(i, j).(color.RGBA); got != want {
						t.Errorf("pixel (%d, %d): got %v, want %v", i, j, got, want)
					}
				}
			}
		})
	}
}

func TestImageDrawTrianglesWithStencilBufferWithEmptyIndicesStateChecks(t *testing.T) {
	disposedShader, err := ggfx.NewShader([]byte(`//kage:unit pixels

package main

func Fragment(dstPos vec4, src0Pos vec2, color vec4) vec4 {
	return color
}
`))
	if err != nil {
		t.Fatal(err)
	}
	disposedShader.Dispose()

	disposedImage := ggfx.NewImage(3, 3)
	disposedImage.Dispose()

	// The empty-indices check must be below the disposed checks, so that an
	// empty draw detects an invalid source in the same way as a non-empty
	// draw does.
	for _, tc := range []struct {
		name string
		draw func(dst *ggfx.Image)
	}{
		{
			name: "DisposedSourceImage",
			draw: func(dst *ggfx.Image) {
				dst.DrawTriangles32(nil, nil, disposedImage, &ggfx.DrawTrianglesOptions{FillRule: ggfx.FillRuleNonZero})
			},
		},
		{
			name: "DisposedShader",
			draw: func(dst *ggfx.Image) {
				dst.DrawTrianglesShader32(nil, nil, disposedShader, &ggfx.DrawTrianglesShaderOptions{FillRule: ggfx.FillRuleNonZero})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := ggfx.NewImage(3, 3)
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("an empty draw on an invalid source must panic, but it did not")
				}
			}()
			tc.draw(dst)
		})
	}
}

func TestImageDrawTrianglesWithStencilBufferOnDisposedDestination(t *testing.T) {
	src := ggfx.NewImage(3, 3)
	src.Fill(color.White)

	shader, err := ggfx.NewShader([]byte(`//kage:unit pixels

package main

func Fragment(dstPos vec4, src0Pos vec2, color vec4) vec4 {
	return color
}
`))
	if err != nil {
		t.Fatal(err)
	}

	vs := []ggfx.Vertex{{DstX: 0, DstY: 0}, {DstX: 2, DstY: 0}, {DstX: 0, DstY: 2}}
	is := []uint32{0, 1, 2}

	for _, tc := range []struct {
		name string
		draw func(dst *ggfx.Image)
	}{
		{
			name: "FillRule",
			draw: func(dst *ggfx.Image) {
				dst.DrawTriangles32(vs, is, src, &ggfx.DrawTrianglesOptions{FillRule: ggfx.FillRuleNonZero})
			},
		},
		{
			name: "AntiAlias",
			draw: func(dst *ggfx.Image) {
				dst.DrawTriangles32(vs, is, src, &ggfx.DrawTrianglesOptions{AntiAlias: true})
			},
		},
		{
			name: "ShaderFillRule",
			draw: func(dst *ggfx.Image) {
				dst.DrawTrianglesShader32(vs, is, shader, &ggfx.DrawTrianglesShaderOptions{FillRule: ggfx.FillRuleNonZero})
			},
		},
		{
			name: "ShaderAntiAlias",
			draw: func(dst *ggfx.Image) {
				dst.DrawTrianglesShader32(vs, is, shader, &ggfx.DrawTrianglesShaderOptions{AntiAlias: true})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := ggfx.NewImage(3, 3)
			dst.Dispose()
			// This must not panic.
			tc.draw(dst)
		})
	}
}
