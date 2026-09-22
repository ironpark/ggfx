# ggfx

ggfx is the rendering and windowing runtime for
[ggui](https://github.com/ironpark/ggui). It is a fork of
[Ebitengine](https://ebitengine.org/) v2.10.2 for desktop and the browser
that keeps the GPU pipeline, `text/v2` and `vector`, replaces the Kage
shader language with WGSL compiled by [naga](https://github.com/gogpu/naga),
and drops audio, mobile and console support.

Shaders are documented in [docs/shaders.md](docs/shaders.md).

See [FORK.md](FORK.md) for what changed against upstream and how fixes are
brought in.

## Platforms

- macOS: Metal, OpenGL
- Windows: DirectX 11/12 (feature level 11.0), OpenGL
- Linux, FreeBSD, NetBSD, OpenBSD: OpenGL (requires cgo for glfw)
- Browser: WebGL via `GOOS=js GOARCH=wasm`

## License

Apache-2.0, the same as Ebitengine. See [LICENSE](LICENSE) and
[NOTICE.md](NOTICE.md).
