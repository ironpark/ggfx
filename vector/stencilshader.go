// Copyright 2025 The Ebitengine Authors
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

package vector

import (
	"sync"

	"github.com/ironpark/ggfx"
)

// The implementation is based on the following article:
// https://medium.com/@evanwallace/easy-scalable-text-rendering-on-the-gpu-c3f4d782c5ac

// These values are protected by cacheM.

var (
	stencilBufferFillShader      *ggfx.Shader
	stencilBufferBezierShader    *ggfx.Shader
	stencilBufferNonZeroShader   *ggfx.Shader
	stencilBufferNonZeroAAShader *ggfx.Shader
	stencilBufferEvenOddShader   *ggfx.Shader
	stencilBufferEvenOddAAShader *ggfx.Shader

	stencilBufferM sync.Mutex
)

func ensureStencilBufferShaders() (*ggfx.Shader, error) {
	stencilBufferM.Lock()
	defer stencilBufferM.Unlock()

	if stencilBufferFillShader != nil {
		return stencilBufferFillShader, nil
	}
	s, err := ggfx.NewShader([]byte(stencilBufferFillShaderSrc))
	if err != nil {
		return nil, err
	}
	stencilBufferFillShader = s
	return stencilBufferFillShader, err
}

func ensureStencilBufferBezierShader() (*ggfx.Shader, error) {
	stencilBufferM.Lock()
	defer stencilBufferM.Unlock()

	if stencilBufferBezierShader != nil {
		return stencilBufferBezierShader, nil
	}
	s, err := ggfx.NewShader([]byte(stencilBufferBezierShaderSrc))
	if err != nil {
		return nil, err
	}
	stencilBufferBezierShader = s
	return stencilBufferBezierShader, nil
}

func ensureStencilBufferNonZeroShader(antialias bool) (*ggfx.Shader, error) {
	stencilBufferM.Lock()
	defer stencilBufferM.Unlock()

	if antialias {
		if stencilBufferNonZeroAAShader != nil {
			return stencilBufferNonZeroAAShader, nil
		}
		s, err := ggfx.NewShader([]byte(stencilBufferNonZeroAAShaderSrc))
		if err != nil {
			return nil, err
		}
		stencilBufferNonZeroAAShader = s
		return stencilBufferNonZeroAAShader, nil
	}

	if stencilBufferNonZeroShader != nil {
		return stencilBufferNonZeroShader, nil
	}
	s, err := ggfx.NewShader([]byte(stencilBufferNonZeroShaderSrc))
	if err != nil {
		return nil, err
	}
	stencilBufferNonZeroShader = s
	return stencilBufferNonZeroShader, nil
}

func ensureStencilBufferEvenOddShader(antialias bool) (*ggfx.Shader, error) {
	stencilBufferM.Lock()
	defer stencilBufferM.Unlock()

	if antialias {
		if stencilBufferEvenOddAAShader != nil {
			return stencilBufferEvenOddAAShader, nil
		}
		s, err := ggfx.NewShader([]byte(stencilBufferEvenOddAAShaderSrc))
		if err != nil {
			return nil, err
		}
		stencilBufferEvenOddAAShader = s
		return stencilBufferEvenOddAAShader, nil
	}

	if stencilBufferEvenOddShader != nil {
		return stencilBufferEvenOddShader, nil
	}
	s, err := ggfx.NewShader([]byte(stencilBufferEvenOddShaderSrc))
	if err != nil {
		return nil, err
	}
	stencilBufferEvenOddShader = s
	return stencilBufferEvenOddShader, nil
}

const stencilBufferFillShaderSrc = `
fn fragment(v: Vertex) -> vec4f {
	var value = 1.0 / 255.0;
	if (front_facing()) {
		value *= 16.0;
	}
	return value * v.color;
}
`

const stencilBufferBezierShaderSrc = `
fn fragment(v: Vertex) -> vec4f {
	// See "Resolution Independent Curve Rendering using Programmable Graphics Hardware" by
	// Charles Loop and Jim Blinn.
	let uv = v.custom.xy;
	var value = clamp(-sign(uv.x * uv.x - uv.y), 0.0, 1.0) * 1.0 / 255.0;
	// The winding of a bezier's triangle is the opposite of a fill's triangle.
	if (!front_facing()) {
		value *= 16.0;
	}
	return value * v.color;
}
`

const stencilBufferNonZeroShaderSrc = `
fn fragment(v: Vertex) -> vec4f {
	let c = src0_unsafe_at(v.src_pos);
	let r = i32(floor(c.r * 255.0 + 0.5));
	let w = abs((r >> 4u) - (r & 15));
	let value = min(f32(w), 1.0);
	return value * v.color;
}
`

const stencilBufferNonZeroAAShaderSrc = `
fn round4(x: vec4f) -> vec4f {
	return floor(x + 0.5);
}

fn fragment(v: Vertex) -> vec4f {
	let c0 = src0_unsafe_at(v.src_pos);
	// custom.xy is the offset to the second sample.
	let c1 = src0_unsafe_at(v.src_pos + v.custom.xy);
	let ci0 = vec4i(round4(c0 * 255.0));
	let ci1 = vec4i(round4(c1 * 255.0));
	let w0 = abs((ci0 >> vec4u(4u)) - (ci0 & vec4i(15)));
	let w1 = abs((ci1 >> vec4u(4u)) - (ci1 & vec4i(15)));
	let v0 = min(vec4f(w0), vec4f(1.0));
	let v1 = min(vec4f(w1), vec4f(1.0));
	return (dot(v0, vec4f(1.0 / 8.0)) + dot(v1, vec4f(1.0 / 8.0))) * v.color;
}
`

const stencilBufferEvenOddShaderSrc = `
fn fragment(v: Vertex) -> vec4f {
	let c = src0_unsafe_at(v.src_pos);
	let r = i32(floor(c.r * 255.0 + 0.5));
	let w = abs((r >> 4u) - (r & 15));
	let value = f32(w % 2);
	return value * v.color;
}
`

const stencilBufferEvenOddAAShaderSrc = `
fn round4(x: vec4f) -> vec4f {
	return floor(x + 0.5);
}

fn fragment(v: Vertex) -> vec4f {
	let c0 = src0_unsafe_at(v.src_pos);
	// custom.xy is the offset to the second sample.
	let c1 = src0_unsafe_at(v.src_pos + v.custom.xy);
	let ci0 = vec4i(round4(c0 * 255.0));
	let ci1 = vec4i(round4(c1 * 255.0));
	let w0 = abs((ci0 >> vec4u(4u)) - (ci0 & vec4i(15)));
	let w1 = abs((ci1 >> vec4u(4u)) - (ci1 & vec4i(15)));
	let v0 = vec4f(w0 % vec4i(2));
	let v1 = vec4f(w1 % vec4i(2));
	return (dot(v0, vec4f(1.0 / 8.0)) + dot(v1, vec4f(1.0 / 8.0))) * v.color;
}
`
