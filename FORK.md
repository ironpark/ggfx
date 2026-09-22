# Fork notice

ggfx is a fork of [Ebitengine](https://github.com/hajimehoshi/ebiten),
made to become the rendering and windowing runtime of
[ggui](https://github.com/ironpark/ggui). It is not a general-purpose game
engine and does not track Ebitengine releases.

## Provenance

| | |
|---|---|
| Upstream | `https://github.com/hajimehoshi/ebiten` |
| Forked at | tag `v2.10.2`, commit `221aa13ad` |
| Upstream remote | `upstream` (kept for cherry-picks) |
| License | Apache-2.0, unchanged. See `LICENSE` and `NOTICE.md`. |

The module path is `github.com/ironpark/ggfx` and the root package is
`ggfx`. Everything else keeps its upstream file layout so that upstream
commits to the retained packages can be cherry-picked.

## What was removed and why

ggui targets desktop and the browser and uses a small part of the engine: images and
shaders, `text/v2`, `vector`, `exp/textinput`, `inpututil`, input, cursor,
monitor and window APIs. Everything outside that was dropped.

- `audio`, `mobile`, `cmd` (ebitenmobile), `examples`, `misc`, `skills`,
  `ebitenutil`, `colorm`, old `text` v1, `vibrate`.
- Console and mobile platforms: Nintendo Switch, PlayStation 5, Xbox GDK,
  Android and iOS sources. The browser (`js`/`wasm`) target is kept. `internal/microsoftgdk`
  survives as a stub whose `IsXbox` is always false so the glfw and DirectX
  drivers stay untouched.
- The VM guest/host remote rendering backend (`exp/vmhost`,
  `internal/vmguest`, `internal/vmprotocol`, `internal/graphicsdriver/remote`)
  and the `RunGameOptions.VMGuestEndpoint` option.
- The Linux framebuffer backend (`internal/fbdev`).
- Tooling: `internal/vettools`, `internal/processtest`,
  `internal/beforemaintest`, `internal/shadercollector`, `exp/shaderprecomp`.
- Test fonts and images that lived under `examples/resources` moved to
  `internal/testresources`.

Retained platforms: macOS (Metal, OpenGL), Windows (DirectX, OpenGL),
Linux and the BSDs (OpenGL through glfw; cgo required), and the browser
(WebGL through `js`/`wasm`). Only macOS was built and tested in the fork
pass; Windows and wasm were cross-compiled; Linux and BSD were not built.

## Upstream policy

Do not merge upstream wholesale. Cherry-pick fixes to the layers that are
kept as-is: `internal/graphicsdriver`, `internal/graphicscommand`,
`internal/atlas`, `internal/restorable`, `internal/shader`,
`internal/shaderir`, `text/v2`, `vector`. The windowing and run loop
(`internal/ui`, `run.go`, `window.go`, `input.go`) will diverge and are not
expected to merge.

## Not done yet

- Multiple windows. Ebitengine assumes one window and one game loop.
- Replacing glfw with a purego Cocoa and Win32 layer shared with ggui.
- Deciding whether gamepad support stays.
- `genkeys.go` still generates key tables for the removed mobile platforms.
- The `ebiten:` prefix in error messages and documentation wording.
