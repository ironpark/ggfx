// Copyright 2023 The Ebiten Authors
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

// Package colormshader provides the built-in shaders that apply a color matrix.
package colormshader

import (
	"bytes"
	"fmt"
	"sync"
	"text/template"

	"github.com/ironpark/ggfx/internal/builtinshader"
)

type Filter = builtinshader.Filter

const (
	FilterNearest   = builtinshader.FilterNearest
	FilterLinear    = builtinshader.FilterLinear
	FilterPixelated = builtinshader.FilterPixelated
)

const FilterCount = builtinshader.FilterCount

type Address = builtinshader.Address

const (
	AddressUnsafe      = builtinshader.AddressUnsafe
	AddressClampToZero = builtinshader.AddressClampToZero
	AddressRepeat      = builtinshader.AddressRepeat
)

const AddressCount = builtinshader.AddressCount

// The names of the uniform members.
const (
	UniformColorMBody        = "body"
	UniformColorMTranslation = "translation"
)

var (
	shaders  [FilterCount][AddressCount][]byte
	shadersM sync.Mutex
)

var tmpl = template.Must(template.New("tmpl").Parse(`
struct ColorM {
	body: mat4x4f,
	translation: vec4f,
}
@group(1) @binding(0) var<uniform> colorm: ColorM;
` + builtinshader.Repeat + `
fn fragment(v: Vertex) -> vec4f {
` + builtinshader.Sampling + `
	// Convert to straight alpha, apply the matrix, and convert back to premultiplied alpha.
	let straight = clr.rgb / (clr.a + (1.0 - sign(clr.a)));
	clr = (colorm.body * vec4f(straight, clr.a)) + colorm.translation;
	clr = vec4f(clr.rgb * clr.a, clr.a);
	clr *= v.color;
	clr = vec4f(min(clr.rgb, vec3f(clr.a)), clr.a);

	return clr;
}
`))

func ShaderSource(filter Filter, address Address) []byte {
	shadersM.Lock()
	defer shadersM.Unlock()

	if s := shaders[filter][address]; s != nil {
		return s
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, builtinshader.NewParams(filter, address)); err != nil {
		panic(fmt.Sprintf("colormshader: tmpl.Execute failed: %v", err))
	}

	b := buf.Bytes()
	shaders[filter][address] = b
	return b
}
