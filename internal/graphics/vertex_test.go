// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package graphics_test

import (
	"testing"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/shader"
)

// The constants the internal uniform block is filled by must be where the prelude declares the
// members; shader.Compile checks naga's layout against shader.InternalUniforms.
func TestInternalUniformConstants(t *testing.T) {
	l := shader.InternalUniforms(graphics.ShaderSrcImageCount)
	for _, c := range []struct {
		name      string
		got, want int
	}{
		{"DstTextureSizeUniformDwordIndex", graphics.DstTextureSizeUniformDwordIndex, l.DstTextureSize},
		{"DstRegionOriginUniformDwordIndex", graphics.DstRegionOriginUniformDwordIndex, l.DstOrigin},
		{"DstRegionSizeUniformDwordIndex", graphics.DstRegionSizeUniformDwordIndex, l.DstSize},
		{"SourceTextureSizeUniformDwordIndex", graphics.SourceTextureSizeUniformDwordIndex, l.SrcTextureSizes},
		{"SourceImageRegionOriginUniformDwordIndex", graphics.SourceImageRegionOriginUniformDwordIndex, l.SrcOrigins},
		{"SourceImageRegionSizeUniformDwordIndex", graphics.SourceImageRegionSizeUniformDwordIndex, l.SrcSizes},
		{"ProjectionMatrixUniformDwordIndex", graphics.ProjectionMatrixUniformDwordIndex, l.Projection},
		{"PreservedUniformDwordCount", graphics.PreservedUniformDwordCount, l.DwordCount},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, c.got, c.want)
		}
	}
	if _, err := graphics.CompileShader([]byte(`fn fragment(v: Vertex) -> vec4f { return v.color; }`)); err != nil {
		t.Error(err)
	}
}
