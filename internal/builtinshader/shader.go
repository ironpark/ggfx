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

package builtinshader

import (
	"bytes"
	"fmt"
	"sync"
	"text/template"
)

type Filter int

const (
	FilterNearest Filter = iota
	FilterLinear
	FilterPixelated
)

const FilterCount = 3

type Address int

const (
	AddressUnsafe Address = iota
	AddressClampToZero
	AddressRepeat
)

const AddressCount = 3

var (
	shaders  [FilterCount][AddressCount][]byte
	shadersM sync.Mutex
)

// Sampling is the WGSL body that reads and filters the source color. It is shared with the
// ColorM variant of the shader. The result is left in `clr`.
const Sampling = `
{{if eq .Filter .FilterNearest}}
{{if eq .Address .AddressUnsafe}}
	var clr = src0_unsafe_at(v.src_pos);
{{else if eq .Address .AddressClampToZero}}
	var clr = src0_at(v.src_pos);
{{else if eq .Address .AddressRepeat}}
	var clr = src0_at(adjust_src_pos_for_address_repeat(v.src_pos));
{{end}}
{{else}}
{{if eq .Filter .FilterLinear}}
	var p0 = v.src_pos - 0.5;
	var p1 = v.src_pos + 0.5;
{{else if eq .Filter .FilterPixelated}}
	// inversed_scale is the size of the region on the source image.
	// The size is the inverse of the geometry-matrix scale.
	var inversed_scale = vec2f(abs(dpdx(v.src_pos.x)), abs(dpdy(v.src_pos.y)));
	// Cap the inversed_scale to 1 as dpdx/dpdy is not accurate on some machines (#3182).
	inversed_scale = min(inversed_scale, vec2f(1.0));
	var p0 = v.src_pos - inversed_scale / 2.0;
	var p1 = v.src_pos + inversed_scale / 2.0;
{{end}}

{{if eq .Address .AddressRepeat}}
	p0 = adjust_src_pos_for_address_repeat(p0);
	p1 = adjust_src_pos_for_address_repeat(p1);
{{end}}

{{if eq .Address .AddressUnsafe}}
	let c0 = src0_unsafe_at(p0);
	let c1 = src0_unsafe_at(vec2f(p1.x, p0.y));
	let c2 = src0_unsafe_at(vec2f(p0.x, p1.y));
	let c3 = src0_unsafe_at(p1);
{{else}}
	let c0 = src0_at(p0);
	let c1 = src0_at(vec2f(p1.x, p0.y));
	let c2 = src0_at(vec2f(p0.x, p1.y));
	let c3 = src0_at(p1);
{{end}}

{{if eq .Filter .FilterLinear}}
	let rate = fract(p1);
{{else if eq .Filter .FilterPixelated}}
	let rate = clamp(fract(p1) / inversed_scale, vec2f(0.0), vec2f(1.0));
{{end}}
	var clr = mix(mix(c0, c1, rate.x), mix(c2, c3, rate.x), rate.y);
{{end}}
`

// Repeat is the WGSL helper the repeat address mode needs.
const Repeat = `
{{if eq .Address .AddressRepeat}}
fn adjust_src_pos_for_address_repeat(p: vec2f) -> vec2f {
	let origin = src0_origin();
	let size = src0_size();
	let d = p - origin;
	return d - size * floor(d / size) + origin;
}
{{end}}
`

var tmpl = template.Must(template.New("tmpl").Parse(Repeat + `
fn fragment(v: Vertex) -> vec4f {
` + Sampling + `
	// Apply the color scale.
	clr *= v.color;

	return clr;
}
`))

// Params is the data the shader templates take.
type Params struct {
	Filter             Filter
	FilterNearest      Filter
	FilterLinear       Filter
	FilterPixelated    Filter
	Address            Address
	AddressUnsafe      Address
	AddressClampToZero Address
	AddressRepeat      Address
}

// NewParams returns the template data for filter and address.
func NewParams(filter Filter, address Address) Params {
	return Params{
		Filter:             filter,
		FilterNearest:      FilterNearest,
		FilterLinear:       FilterLinear,
		FilterPixelated:    FilterPixelated,
		Address:            address,
		AddressUnsafe:      AddressUnsafe,
		AddressClampToZero: AddressClampToZero,
		AddressRepeat:      AddressRepeat,
	}
}

func ShaderSource(filter Filter, address Address) []byte {
	shadersM.Lock()
	defer shadersM.Unlock()

	if s := shaders[filter][address]; s != nil {
		return s
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, NewParams(filter, address)); err != nil {
		panic(fmt.Sprintf("builtinshader: tmpl.Execute failed: %v", err))
	}

	b := buf.Bytes()
	shaders[filter][address] = b
	return b
}

const ClearShaderSource = `
fn fragment(v: Vertex) -> vec4f {
	return vec4f(0.0);
}
`
