# ggfx

ggfx is the rendering and windowing runtime for
[ggui](https://github.com/ironpark/ggui). It is a fork of
[Ebitengine](https://ebitengine.org/) v2.10.2 for desktop and the browser
that keeps the GPU pipeline, shaders, `text/v2` and `vector`, and drops
audio, mobile and console support.

See [FORK.md](FORK.md) for what changed against upstream and how fixes are
brought in.

## Platforms

- macOS: Metal, OpenGL
- Windows: DirectX 11/12, OpenGL
- Linux, FreeBSD, NetBSD, OpenBSD: OpenGL (requires cgo for glfw)
- Browser: WebGL via `GOOS=js GOARCH=wasm`

## License

Apache-2.0, the same as Ebitengine. See [LICENSE](LICENSE) and
[NOTICE.md](NOTICE.md).
