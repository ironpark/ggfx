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

// Package ggfx is a renderer: images, shaders, vectors through vector and SVG through svg, drawn
// on the GPU and presented on surfaces that a window system owns.
//
// ggfx has no windows. A window system drives it through package host: it initializes the
// renderer, makes a surface of each window's native view, and draws a frame to the surface's
// screen and presents it. ggwd (github.com/ironpark/ggwd) is the window system ggui uses.
//
// Images can be made and drawn to at any time from any goroutine. Their draws run in a frame, and
// reading pixels waits for one. host.Init is called on the main thread, the goroutine running main
// on the OS main thread. The graphics library, the color space and the environment variables
// that choose them are host's.
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
