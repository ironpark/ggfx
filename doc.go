// Copyright 2014 Hajime Hoshi
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

// Package ggfx is a renderer: images, shaders, text through text/v2, vectors through vector and
// SVG through svg, drawn on the GPU and presented on surfaces that a window system owns.
//
// ggfx has no windows. A window system initializes it with [Init], makes a [Surface] of each
// window's native view with [NewSurface], and draws a frame by drawing to the surface's
// [Surface.Screen] between [BeginFrame] and [EndFrame] and presenting with [Flush]. ggwd
// (github.com/ironpark/ggwd) is the window system ggui uses; renderer.go shows the shape of
// a frame.
//
// Images can be made and drawn to at any time from any goroutine. Their draws run in a frame, and
// reading pixels waits for one. [Init] is called on the main thread, the goroutine running main
// on the OS main thread.
//
// # Environment variables
//
// `GGFX_GRAPHICS_LIBRARY` environment variable specifies the graphics library.
// If the specified graphics library is not available, Init returns an error.
// This environment variable works when Init is called with GraphicsLibraryAuto.
// This can take one of the following value:
//
//	"auto":    The renderer chooses the graphics library automatically. This is the default value.
//	"opengl":  OpenGL, OpenGL ES, or WebGL.
//	"directx": DirectX. This works only on Windows.
//	"metal":   Metal. This works only on macOS or iOS.
//
// `GGFX_DIRECTX` environment variable specifies various parameters for DirectX.
// You can specify multiple values separated by a comma. The default value is empty (i.e. no parameters).
//
//	"debug":                      Use a debug layer.
//	"dred":                       Enable Device Removed Extended Data to diagnose a device removal. This is for DirectX 12.
//	"warp":                       Use WARP (i.e. software rendering).
//	"version=VERSION":            Specify a DirectX version (e.g. 11).
//	"featurelevel=FEATURE_LEVEL": Specify a feature level (e.g. 11_0). This is for DirectX 12.
//
// The options taking arguments are exclusive, and if multiples are specified, the lastly specified value is adopted.
//
// The possible values for the option "version" are "11" and "12".
// If the version is not specified, the default version 11 is adopted.
// On Xbox, the "version" option is ignored and DirectX 12 is always adopted.
//
// The option "featurelevel" is valid only for DirectX 12.
// The possible values are "11_0", "11_1", "12_0", "12_1", and "12_2". The default value is "11_0".
//
// The option "dred" is valid only for DirectX 12 and is independent of "debug".
// On a device removal, it reports the GPU's last command and the page-fault address via log/slog.
//
// Ebitengine's names, `EBITENGINE_GRAPHICS_LIBRARY` and `EBITENGINE_DIRECTX`, are read when the
// ggfx ones are not set.
//
// # Build tags
//
// `ggfxdebug` outputs a log of graphics commands. This is useful to know what happens in the renderer. In general, the
// number of graphics commands affects the performance of your app.
//
// `ggfxgldebug` enables a debug mode for OpenGL. This is valid only when the graphics library is OpenGL.
// This affects performance very much.
//
// `ggfxnowasmsimd` excludes the WebAssembly SIMD (SIMD128) instructions that speed up batches of vertices and
// indices. A WebAssembly engine without SIMD128 support, such as Safari before 16.4, cannot load a module that
// includes them at all. This is valid only for WebAssembly.
//
// Ebitengine's tags, `ebitenginedebug` and `ebitenginegldebug`, work too.
package ggfx
