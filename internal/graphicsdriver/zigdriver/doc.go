// Copyright 2026 The ggfx Authors
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

//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

// Package zigdriver implements [graphicsdriver.Graphics] by recording batched
// GPU commands into an aligned linear arena ring and submitting them to the
// native Zig driver shared library over a flat C ABI via purego (without cgo).
//
// # Environment Variables
//
//   - GGFX_DRIVER: Selects the driver implementation ("zig" or "go"). When
//     unset, the Go driver is used unless GGFX_DRIVER_LIB or
//     GGFX_GRAPHICS_LIBRARY=zig|vulkan is set. Setting GGFX_DRIVER=go forces
//     the Go driver even if GGFX_DRIVER_LIB is set.
//   - GGFX_DRIVER_LIB: Explicit path to the native driver shared library. If
//     loading this path fails, search stops immediately with that error (and
//     internal/render falls back to the Go driver unless GGFX_DRIVER_STRICT=1).
//   - GGFX_DRIVER_STRICT: When "1", failure to initialize the Zig driver
//     returns an error instead of falling back to the Go driver.
//   - GGFX_DRIVER_DEBUG: When "1", enables backend validation/debug layers
//     (Vulkan validation layers, D3D12 debug layer, etc.).
//   - GGFX_DRIVER_SLOTS: Configures the command arena ring slot count ("2" or
//     "3"; default is "2").
//   - GGFX_ZIG_METAL_DISPATCH: When "1" and no [graphicsdriver.MainThreadRunner]
//     is installed, instructs the Metal backend to dispatch main-thread presents
//     via GCD dispatch_sync_f instead of inline execution.
//
// # Shared Library Search Order
//
//  1. GGFX_DRIVER_LIB (exact path; failure is terminal and does not fall through).
//  2. Next to the executable: filepath.Dir(os.Executable())/<name>, plus
//     ../Frameworks/libggfx_driver.dylib on macOS (.app bundles).
//  3. System library search path (libggfx_driver.dylib on macOS,
//     libggfx_driver.so on Linux, ggfx_driver.dll on Windows).
//
// Immediately after loading, ggfx_abi_version() is checked against
// [DriverABIVersion]. Any mismatch fails with [ErrABIMismatch].
//
// # Threading Contract
//
//   - Device creation (NewGraphics) runs on the OS main thread; Close runs
//     after the render thread has drained and exited.
//   - Begin, End, SetVertices, NewImage, NewShader, DrawTriangles,
//     Image.WritePixels, Image.ReadPixels, Dispose, FinishForcedFrame, and
//     DebugStats are serialized on the render thread.
//   - NewSurface is called on the main thread and may overlap render-thread
//     commands; the native library guards its surface table internally.
//   - Surface.SetDamage, Surface.Kept, and Surface.SetWindowState may be called
//     from the frame goroutine; damage state is synchronized with a mutex and
//     flushed at the head of the next Begin batch.
//   - IsOccluded and SetMainThreadRunner are safe for concurrent calls.
package zigdriver
