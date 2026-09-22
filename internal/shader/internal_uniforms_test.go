// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package shader_test

import (
	"testing"

	"github.com/ironpark/ggfx/internal/builtinshader"
	"github.com/ironpark/ggfx/internal/shader"
)

// The builtin unsafe shader reads only the projection matrix, so the regions
// that differ per draw are zeroed and draws can be merged. The clamp shader
// reads the source regions as well.
func TestFilterInternalUniforms(t *testing.T) {
	const (
		dwords     = 72
		projection = 56
		srcRegions = 24
	)
	for _, tc := range []struct {
		address builtinshader.Address
		from    int // the first dword that survives
	}{
		{builtinshader.AddressUnsafe, projection},
		{builtinshader.AddressClampToZero, srcRegions},
	} {
		p, err := shader.Compile(builtinshader.ShaderSource(builtinshader.FilterNearest, tc.address), 4)
		if err != nil {
			t.Fatal(err)
		}
		u := make([]uint32, dwords)
		for i := range u {
			u[i] = 1
		}
		p.FilterInternalUniforms(u)
		for i, v := range u {
			want := uint32(0)
			if i >= tc.from {
				want = 1
			}
			if v != want {
				t.Fatalf("address %d: dword %d = %d, want %d", tc.address, i, v, want)
			}
		}
	}
}
