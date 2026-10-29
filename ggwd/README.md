# ggwd

ggwd is the window system of [ggui](https://github.com/ironpark/ggui):
windows, an event loop that delivers input and frames as events, and the
platform's cursor, clipboard, monitors, IME, drag and drop, accessibility and
native hooks. It came out of [ggfx](https://github.com/ironpark/ggfx), the
renderer it draws with, and uses nothing of ggfx but its public API, which a
test checks.

The window model and the event contract are documented in
[docs/window.md](docs/window.md).

## License

Apache-2.0, the same as Ebitengine, which it derives from. See ggfx's
LICENSE and NOTICE.md.
