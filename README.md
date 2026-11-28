# ggfx

ggfx is the renderer of [ggui](https://github.com/ironpark/ggui): it draws
images, shaders and vectors on the GPU and presents them on surfaces that a
window system owns. It began as a fork of
[Ebitengine](https://ebitengine.org/) v2.10.2 for desktop and the browser
that keeps the GPU pipeline and `vector`, replaces the Kage
shader language with WGSL compiled by [naga](https://github.com/gogpu/naga),
and drops audio, mobile and console support. It no longer follows
Ebitengine.

ggfx has no windows. [ggwd](https://github.com/ironpark/ggwd) is the window
system ggui uses, and it drives ggfx through the public renderer API in
package `host` alone.

Shaders are documented in [docs/shaders.md](docs/shaders.md).

See [FORK.md](FORK.md) for what changed against Ebitengine.

## Platforms

ggfx requires Go 1.27 or later, and so macOS 13 Ventura or later.

- macOS: Metal, OpenGL
- Windows: DirectX 11/12 (feature level 11.0), OpenGL
- Linux, FreeBSD, NetBSD, OpenBSD: OpenGL (ggwd requires cgo for glfw)
- Browser: WebGL via `GOOS=js GOARCH=wasm`

## Native driver

A second driver, written in Zig, draws with Metal on macOS, DirectX 12 on
Windows and Vulkan on Linux (amd64 and arm64). ggfx loads it as a shared
library without cgo. Build with the `ggfxzig` tag to make it the default; the
library is then embedded in the program:

```sh
go build -tags ggfxzig ./...
```

Add `ggfxnoembed` to leave the library out and ship it yourself, next to the
executable or at a path fixed at link time. The libraries are attached to each
[release](https://github.com/ironpark/ggfx/releases). See
[docs/zig-driver-design.md](docs/zig-driver-design.md#build-tags-and-the-embedded-library).

## License

Apache-2.0, the same as Ebitengine. See [LICENSE](LICENSE) and
[NOTICE.md](NOTICE.md).
