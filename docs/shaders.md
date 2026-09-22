# Shaders

ggfx shaders are written in [WGSL](https://www.w3.org/TR/WGSL/) and compiled
with [gogpu/naga](https://github.com/gogpu/naga), a pure Go port of wgpu's
shader translator. One source is translated to MSL for Metal, HLSL for
DirectX, and GLSL for OpenGL and WebGL.

A shader source is a fragment function plus, optionally, one uniform block.
ggfx prepends a prelude that declares the vertex stage, the fragment entry
point, the source textures and the helper functions below.

```wgsl
struct Uniforms {
	center: vec2f,
	radius: f32,
	tint: vec4f,
}
@group(1) @binding(0) var<uniform> u: Uniforms;

fn fragment(v: Vertex) -> vec4f {
	let d = length(v.src_pos - u.center) - u.radius;
	return u.tint * (1.0 - smoothstep(-0.5, 0.5, d)) * src0_at(v.src_pos);
}
```

```go
s, err := ggfx.NewShader([]byte(src))
op := &ggfx.DrawRectShaderOptions{}
op.Uniforms = map[string]any{
	"center": []float32{8, 8},
	"radius": 6.0,
	"tint":   []float32{1, 0, 0, 1},
}
op.Images[0] = img
dst.DrawRectShader(16, 16, s, op)
```

## The fragment function

`fn fragment(v: Vertex) -> vec4f` is required. It returns a premultiplied
alpha color. `Vertex` is:

| Member | Type | Meaning |
|---|---|---|
| `position` | `vec4f` | The fragment's position on the destination texture, in pixels. |
| `src_pos` | `vec2f` | The corresponding position on source texture 0, in pixels. |
| `color` | `vec4f` | The interpolated vertex color, or the color scale of `DrawRectShader`. |
| `custom` | `vec4f` | The interpolated custom vertex values. |

Do not declare `@vertex` or `@fragment` entry points; the prelude owns them.

## Uniforms

Declare at most one uniform variable, at `@group(1) @binding(0)`. Its struct
members are the keys of `Uniforms` on the draw options. A value may be a Go
number, bool, or a slice or array of them; slices are flattened in WGSL
order: vector components, matrix columns then rows, array elements. A
member missing from the map is zero. A member that does not exist in the
shader is ignored. A value with the wrong number of elements panics.

The variable may also be a bare vector, matrix or array; its key is the
variable's name.

`bool` is not a host-shareable WGSL type; use `i32` or `u32` and pass a Go
bool, which becomes 0 or 1.

## Source images

Up to four source images are bound. Positions are in pixels of the texture
an image lives on, which is why the helpers exist; a shader never sees
normalized coordinates.

| Helper | Meaning |
|---|---|
| `src0_at(pos)` … `src3_at(pos)` | The color at `pos` on source texture N, or transparent black outside source image N. |
| `src0_unsafe_at(pos)` … | The same without the region check. Reading outside the texture is undefined. |
| `src1_at_from_src0(pos)` …, `src1_unsafe_at_from_src0(pos)` … | Like the above, with `pos` in pixels of source texture 0, as `v.src_pos` is. |
| `src0_origin()` …, `src0_size()` … | The origin and size of source image N on its texture. |
| `src0_texture_size()` …, `src_texture_size()` | The size of source texture N; the second is texture 0. |
| `dst_origin()`, `dst_size()`, `dst_texture_size()` | The same for the destination. |
| `front_facing()` | Whether the fragment belongs to a front-facing (clockwise on screen) triangle. |

Texture reads are `textureLoad` at integer pixel coordinates; there is no
sampler. Linear filtering is done by the built-in shaders in WGSL, and a
custom shader can do the same.

## Translation

The prelude binds the internal uniform block at `@group(0) @binding(0)` and
the source textures at `@group(0) @binding(1..4)`. Each driver maps these
onto its API: Metal buffers 1 and 2 and textures 0..3, HLSL registers `b0`,
`b1` and `t0`..`t3`, and GLSL uniform blocks and samplers looked up by name.
Uniform values are laid out on the host following WGSL's uniform address
space rules, which every backend honors, so no driver reorders them.

Shader model 5.0 is required on DirectX (feature level 11.0), GLSL 3.30 on
desktop OpenGL and GLSL ES 3.00 on WebGL 2.
