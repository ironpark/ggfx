# ggfx

ggfx is the rendering and windowing runtime for
[ggui](https://github.com/ironpark/ggui). It is a desktop-only fork of
[Ebitengine](https://ebitengine.org/) v2.10.2 that keeps the GPU pipeline,
Kage shaders, `text/v2` and `vector`, and drops audio, mobile, console and
browser support.

See [FORK.md](FORK.md) for what changed against upstream and how fixes are
brought in.

## Platforms

- macOS: Metal, OpenGL
- Windows: DirectX 11/12, OpenGL
- Linux, FreeBSD, NetBSD, OpenBSD: OpenGL (requires cgo for glfw)

## License

Apache-2.0, the same as Ebitengine. See [LICENSE](LICENSE) and
[NOTICE.md](NOTICE.md).
