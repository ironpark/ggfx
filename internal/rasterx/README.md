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
  target's whole area. `raster.go` ports its line rasterization, under the
  Go Authors' BSD license (see NOTICE.md), to accumulate only a window
  around the path's extent, clipped to what is drawn. Only vector's
  floating-point accumulator is ported, used at every size: its fixed
  point, which vector chose up to 512 pixels a side, stepped x down a
  line row by row, rounding each step the same way, and along a long
  steep edge drifted by most of a pixel. Each row is summed on its own,
  from 0 at its start, in one scalar order, and where a line enters and
  leaves a row is found from the line's start, not stepped to from the
  row above, so rows above the window are skipped. Coverage left of the
  window starts a row's sum, and coverage past the right edge falls in a
  spare cell past the row's end, where vector carried it into the next
  row's first pixel; so the window's mask is the whole target's whatever
  part of the path it holds (but for the order the cells left of it are
  added in), with no fallback to accumulating the whole target, and the
  same on every architecture, where vector's amd64 assembly summed in an
  order of its own. A path's lines must enclose, as a `Filler`'s and a
  `Stroker`'s do: what an open path would cover past its right end is not
  drawn. `TestCoverageIsExact` checks the mask against polygons' exact
  per-pixel areas, long steep edges and slivers among them, within 1e-4
  of full coverage; `TestScannerDrawsAsVector` checks the result against
  vector's, in floating point, on random paths, within one in each byte
  (`maxVectorDiff`), and a clipped draw against the whole.
  The scanner keeps the path's points until `Draw`, which draws only the
  pixels the mask covers, with the arithmetic of vector's `Draw`.
- No multiplication is fused with an addition: each product that is
  added is converted to its type, `float64(x*y) + z`, which forbids the
  fused multiply-add the compiler emits on arm64 but not on amd64 or wasm.
  `Sin`, `Cos`, `Tan` and `Atan2` (`trig.go`) are the math package's,
  ported under the Go Authors' BSD license with their products so
  converted: the compiler fuses the math package's own on arm64. So the
  package draws the same bytes on every architecture
  (`TestTrigIsTheSameEverywhere`).
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
- An `ObjectBoundingBox` gradient's `Bounds` is a box in the user space
  `GetColorFunctionUS` is given: as SVG defines it, the gradient is a
  `UserSpaceOnUse` one under that space's matrix times the box's, each
  pixel mapped back into the gradient's own space. The original took the
  box in device space and mapped only the gradient's transform back, which
  under a rotation or skew drew the gradient in the wrong box. A
  `UserSpaceOnUse` gradient is still placed by mapping its points forward
  and its radii as vectors, which is right only where the matrices
  neither rotate nor skew. `Path.Bounds` is a path's extent, its curves'
  own, not their control points'.
- A radial gradient whose focus is not its center finds each pixel's
  place on the ray from the focus in closed form, with one square root,
  where the original intersected the ray with the circle and took two
  more.
- The even-odd fill rule: `ScannerGV.SetWinding(false)` folds the
  accumulated coverage modulo 2, where the
  original ignored it. The non-zero rule clamps it to 1, as vector does.
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
  to `testdata` are compared with goldens, a hash per drawing, the same
  on every architecture, in `testdata/golden.txt`, through
  `internal/goldentest`.
  `bezier_test.go` seeds its random numbers with constants, not the
  clock.
