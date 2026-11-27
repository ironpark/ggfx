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
	"sync/atomic"

	"github.com/ironpark/ggfx"
)

// The implementation is based on the following article:
// https://medium.com/@evanwallace/easy-scalable-text-rendering-on-the-gpu-c3f4d782c5ac

// lazyShader is a shader compiled at its first use. After that, getting it takes no lock, as
// fillPaths gets its shaders at every flush.
type lazyShader struct {
	src    string
	shader atomic.Pointer[ggfx.Shader]
	m      sync.Mutex
}

func (l *lazyShader) get() (*ggfx.Shader, error) {
	if s := l.shader.Load(); s != nil {
		return s, nil
	}

	l.m.Lock()
	defer l.m.Unlock()

	if s := l.shader.Load(); s != nil {
		return s, nil
	}
	s, err := ggfx.NewShader([]byte(l.src))
	if err != nil {
		return nil, err
	}
	l.shader.Store(s)
	return s, nil
}

var (
	stencilBufferFillShader   = lazyShader{src: stencilBufferFillShaderSrc}
	stencilBufferBezierShader = lazyShader{src: stencilBufferBezierShaderSrc}
	stencilBufferCoverShader  = lazyShader{src: stencilBufferCoverShaderSrc}
)

func ensureStencilBufferShaders() (*ggfx.Shader, error) {
	return stencilBufferFillShader.get()
}

func ensureStencilBufferBezierShader() (*ggfx.Shader, error) {
	return stencilBufferBezierShader.get()
}

func ensureStencilBufferCoverShader() (*ggfx.Shader, error) {
	return stencilBufferCoverShader.get()
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

// stencilAtSrc is the cover shaders' read of a stencil buffer: custom.z is the index of the
// source image the buffer is on, as a draw covers paths whose buffers are on different images.
const stencilAtSrc = `
fn stencil_at(v: Vertex, pos: vec2f) -> vec4f {
	if (v.custom.z < 0.5) {
		return src0_unsafe_at(pos);
	}
	if (v.custom.z < 1.5) {
		return src1_unsafe_at(pos);
	}
	if (v.custom.z < 2.5) {
		return src2_unsafe_at(pos);
	}
	return src3_unsafe_at(pos);
}
`

// stencilBufferCoverShaderSrc renders a path's stencil buffer(s) with its color.
// custom.w encodes the path's cover mode so paths with different fill rules or antialiasing can
// share a cover draw: 0 is non-zero, 1 is even-odd, 2 is non-zero with antialiasing, and 3 is
// even-odd with antialiasing.
const stencilBufferCoverShaderSrc = stencilAtSrc + `
fn round4(x: vec4f) -> vec4f {
	return floor(x + 0.5);
}

fn fragment(v: Vertex) -> vec4f {
	if (v.custom.w > 1.5) {
		let c0 = stencil_at(v, v.src_pos);
		// custom.xy is the offset to the second sample.
		let c1 = stencil_at(v, v.src_pos + v.custom.xy);
		let ci0 = vec4i(round4(c0 * 255.0));
		let ci1 = vec4i(round4(c1 * 255.0));
		let w0 = abs((ci0 >> vec4u(4u)) - (ci0 & vec4i(15)));
		let w1 = abs((ci1 >> vec4u(4u)) - (ci1 & vec4i(15)));
		if (v.custom.w > 2.5) {
			let v0 = vec4f(w0 % vec4i(2));
			let v1 = vec4f(w1 % vec4i(2));
			return (dot(v0, vec4f(1.0 / 8.0)) + dot(v1, vec4f(1.0 / 8.0))) * v.color;
		}
		let v0 = min(vec4f(w0), vec4f(1.0));
		let v1 = min(vec4f(w1), vec4f(1.0));
		return (dot(v0, vec4f(1.0 / 8.0)) + dot(v1, vec4f(1.0 / 8.0))) * v.color;
	}
	let c = stencil_at(v, v.src_pos);
	let r = i32(floor(c.r * 255.0 + 0.5));
	let w = abs((r >> 4u) - (r & 15));
	if (v.custom.w > 0.5) {
		return f32(w % 2) * v.color;
	}
	return min(f32(w), 1.0) * v.color;
}
`
