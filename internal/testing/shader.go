// Copyright 2020 The Ebiten Authors
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

package testing

import (
	"fmt"
	"strings"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/shader"
)

// ShaderProgramFill returns a shader to fill the framebuffer with a color.
func ShaderProgramFill(r, g, b, a byte) *shader.Program {
	ir, err := graphics.CompileShader(fmt.Appendf(nil, `
fn fragment(v: Vertex) -> vec4f {
	return vec4f(%0.9f, %0.9f, %0.9f, %0.9f);
}
`, float64(r)/0xff, float64(g)/0xff, float64(b)/0xff, float64(a)/0xff))
	if err != nil {
		panic(err)
	}
	return ir
}

// ShaderProgramImages returns a shader to render the framebuffer with the sum of the given images.
func ShaderProgramImages(numImages int) *shader.Program {
	if numImages <= 0 {
		panic("testing: numImages must be >= 1")
	}

	var exprs []string
	for i := range numImages {
		if i == 0 {
			exprs = append(exprs, "src0_unsafe_at(v.src_pos)")
		} else {
			exprs = append(exprs, fmt.Sprintf("src%d_unsafe_at_from_src0(v.src_pos)", i))
		}
	}

	ir, err := graphics.CompileShader(fmt.Appendf(nil, `
fn fragment(v: Vertex) -> vec4f {
	return %s;
}
`, strings.Join(exprs, " + ")))
	if err != nil {
		panic(err)
	}
	return ir
}
