# rasterx (fork)

This package is a fork of [github.com/srwiley/rasterx](https://github.com/srwiley/rasterx)
at version `v0.0.0-20220730225603-2ab79fcdd4ef`, which is no longer maintained.
`internal/oksvg` rasterizes with it. The original is licensed under the
BSD-3-Clause license; its full text is reproduced in the repository root
NOTICE.md. This fork is distributed under the Apache License 2.0 like the
rest of this repository.

The `testdata` and `doc` directories of the original repository are not
included. `bezier_test.go` and `rasterx_test.go` are, changed as listed
below.

## Local modifications

- The sources are formatted with gofmt.
- `ScannerGV` no longer draws with `golang.org/x/image/vector`'s
  `Rasterizer`, sized to the target, which made each path cost the
  target's whole area. `raster.go` ports its line rasterization and mask
  accumulation, under the Go Authors' BSD license (see NOTICE.md), to
  accumulate only a window around the path's extent. The window's mask is
  vector's, byte for byte: the same arithmetic on the same coordinates, the
  same choice of fixed or floating point by the target's size, and, on
  amd64, the order its SSE4.1 assembly sums in. Outside the window,
  vector's mask is the coverage's rounding error, which draws as nothing
  unless it reaches 1/256 (`TestFaintMaskChangesNothing`); where it does,
  or a line strays from the window, the path is accumulated all over, as
  vector would. `TestScannerDrawsAsVector` checks the result against
  vector's on random paths. The scanner keeps the path's points until
  `Draw`, which draws only the pixels the mask covers, with the arithmetic
  of vector's `Draw`.
- Paint is typed: a `Paint` is a premultiplied `color.RGBA64`, or a
  `ColorFunc` returning one at each pixel, in place of `interface{}`
  holding a `color.Color` or a `ColorFunc` returning a `color.Color`.
  `Scanner.SetColor` is `SetPaint`, `Gradient.GetColorFunction` and
  `GetColorFunctionUS` return a `Paint`, and `ColorPaint` and `FuncPaint`
  make them. The `image.Image` sources `ColorFuncImage` and `ClipImage`,
  and `ScannerGV.Source`, which boxed a color at each pixel, are gone; a
  gradient no longer boxes one either.
- `ScannerGV.Dest` is an `*image.RGBA`, not a `draw.Image`: every caller
  draws into one, and the generic `At`/`Set` path is gone. `Targ` is where
  the mask's origin goes in `Dest`, which may hold only part of it;
  `Touched` records what a draw changed, and a nil `Dest` draws nothing
  but records the window the path would be drawn in, without rasterizing
  it.
- `Gradient.GetColorFunctionUS` no longer sorts `Stops`, which mutated the
  gradient as it drew; its caller orders them.
- The even-odd fill rule: `ScannerGV.SetWinding(false)` folds the
  accumulated coverage modulo 2, in fixed and floating point, where the
  original ignored it. The non-zero rule's mask is vector's as before.
- `ApplyOpacity` returns the color not premultiplied, its alpha scaled by
  the opacity and clamped to [0, 1]. The original took the low byte of
  `RGBA`'s 16-bit premultiplied values and dropped the color's alpha,
  right only for an opaque 8-bit color. A gradient blends its stops'
  colors not premultiplied and their opacities apart, a stop color's
  alpha part of its opacity, and the first stop, drawn where a repeated
  gradient's last and first stops meet, takes the gradient's opacity,
  which it dropped.
- A gradient's paint decodes its stops' colors once, not at each pixel,
  and an `NRGBA64` stop color blends by its own 16-bit values, not by
  `RGBA`'s premultiplied ones unpremultiplied.
- `rasterx_test.go` keeps the original's assertions; the drawings it wrote
  to `testdata` are compared with goldens, a hash per drawing and
  `GOARCH`, in `testdata/golden.txt`, through `internal/goldentest`.
  `bezier_test.go` seeds its random numbers with constants, not the
  clock.
