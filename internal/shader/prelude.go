// Copyright 2026 The ggfx Authors
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

package shader

import (
	"fmt"
	"strings"
)

// The names of the entry points the prelude declares. Backends look them up by these names.
const (
	VertexEntryPoint   = "ggfx_vertex"
	FragmentEntryPoint = "ggfx_fragment"
)

// The name of the function a user shader must define.
const UserFragmentFunction = "fragment"

// Prelude returns the WGSL that is prepended to every user shader. It declares the internal
// uniform block, the source textures, the vertex entry point, the fragment entry point that calls
// the user's fragment function, and the helper functions a shader may call.
//
// The internal uniform block is bound at group 0 binding 0, the source textures at group 0
// bindings 1..textureCount, and a user's uniform block, if any, at group 1 binding 0.
func Prelude(textureCount int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `struct GgfxInternal {
	dst_texture_size: vec2f,
	dst_origin: vec2f,
	dst_size: vec2f,
	src_texture_sizes: array<vec4f, %[1]d>,
	src_origins: array<vec4f, %[1]d>,
	src_sizes: array<vec4f, %[1]d>,
	projection: mat4x4f,
}
@group(0) @binding(0) var<uniform> ggfx_internal: GgfxInternal;
`, textureCount)
	for i := range textureCount {
		fmt.Fprintf(&b, "@group(0) @binding(%d) var ggfx_src%d: texture_2d<f32>;\n", i+1, i)
	}
	b.WriteString(`
// Vertex is what the fragment function receives: the fragment's position on the destination
// texture in pixels, the corresponding position on the first source texture in pixels, the vertex
// color, and the custom vertex values.
struct Vertex {
	position: vec4f,
	src_pos: vec2f,
	color: vec4f,
	custom: vec4f,
}

struct GgfxVertexIn {
	@location(0) dst_pos: vec2f,
	@location(1) src_pos: vec2f,
	@location(2) color: vec4f,
	@location(3) custom: vec4f,
}

struct GgfxVertexOut {
	@builtin(position) position: vec4f,
	@location(0) src_pos: vec2f,
	@location(1) color: vec4f,
	@location(2) custom: vec4f,
}

@vertex fn ggfx_vertex(in: GgfxVertexIn) -> GgfxVertexOut {
	var out: GgfxVertexOut;
	out.position = ggfx_internal.projection * vec4f(in.dst_pos, 0.0, 1.0);
	out.src_pos = in.src_pos;
	out.color = in.color;
	out.custom = in.custom;
	return out;
}

var<private> ggfx_front_facing: bool;

@fragment fn ggfx_fragment(in: GgfxVertexOut, @builtin(front_facing) ff: bool) -> @location(0) vec4f {
	ggfx_front_facing = ff;
	var v: Vertex;
	v.position = in.position;
	v.src_pos = in.src_pos;
	v.color = in.color;
	v.custom = in.custom;
	return fragment(v);
}

// front_facing reports whether the current fragment belongs to a front-facing triangle.
fn front_facing() -> bool { return ggfx_front_facing; }

// dst_texture_size returns the size of the destination texture in pixels.
fn dst_texture_size() -> vec2f { return ggfx_internal.dst_texture_size; }

// dst_origin returns the origin of the destination image on its texture in pixels.
fn dst_origin() -> vec2f { return ggfx_internal.dst_origin; }

// dst_size returns the size of the destination image in pixels.
fn dst_size() -> vec2f { return ggfx_internal.dst_size; }

// src_texture_size returns the size of the first source texture in pixels.
fn src_texture_size() -> vec2f { return ggfx_internal.src_texture_sizes[0].xy; }
`)
	for i := range textureCount {
		fmt.Fprintf(&b, `
// src%[1]d_texture_size returns the size of source texture %[1]d in pixels.
fn src%[1]d_texture_size() -> vec2f { return ggfx_internal.src_texture_sizes[%[1]d].xy; }

// src%[1]d_origin returns the origin of source image %[1]d on its texture in pixels.
fn src%[1]d_origin() -> vec2f { return ggfx_internal.src_origins[%[1]d].xy; }

// src%[1]d_size returns the size of source image %[1]d in pixels.
fn src%[1]d_size() -> vec2f { return ggfx_internal.src_sizes[%[1]d].xy; }

// src%[1]d_unsafe_at returns the color at pos on source texture %[1]d without checking that pos is
// inside the source image. pos is in pixels of source texture %[1]d.
fn src%[1]d_unsafe_at(pos: vec2f) -> vec4f {
	return textureLoad(ggfx_src%[1]d, vec2i(pos), 0);
}

// src%[1]d_at returns the color at pos on source texture %[1]d, or transparent black when pos is
// outside source image %[1]d. pos is in pixels of source texture %[1]d.
fn src%[1]d_at(pos: vec2f) -> vec4f {
	let origin = ggfx_internal.src_origins[%[1]d].xy;
	let inside = step(origin, pos) - step(origin + ggfx_internal.src_sizes[%[1]d].xy, pos);
	return textureLoad(ggfx_src%[1]d, vec2i(pos), 0) * inside.x * inside.y;
}
`, i)
		if i == 0 {
			continue
		}
		fmt.Fprintf(&b, `
// src%[1]d_unsafe_at_from_src0 is src%[1]d_unsafe_at with pos given in pixels of source texture 0.
fn src%[1]d_unsafe_at_from_src0(pos: vec2f) -> vec4f {
	return src%[1]d_unsafe_at(pos - ggfx_internal.src_origins[0].xy + ggfx_internal.src_origins[%[1]d].xy);
}

// src%[1]d_at_from_src0 is src%[1]d_at with pos given in pixels of source texture 0.
fn src%[1]d_at_from_src0(pos: vec2f) -> vec4f {
	return src%[1]d_at(pos - ggfx_internal.src_origins[0].xy + ggfx_internal.src_origins[%[1]d].xy);
}
`, i)
	}
	return b.String()
}
