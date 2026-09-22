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

// Package ggfx provides the graphics, windowing and input API that ggui is built on.
//
// An application starts with [Run], which drives a handler with events. The handler creates its
// windows on [StartEvent] and draws on [FrameEvent]; frames are delivered on request rather than
// on a tick.
//
//	func main() {
//	    var window *ggfx.Window
//	    err := ggfx.Run(ggfx.HandlerFunc(func(ev ggfx.Event) error {
//	        switch ev := ev.(type) {
//	        case ggfx.StartEvent:
//	            w, err := ggfx.NewWindow(&ggfx.WindowOptions{Title: "Title", Width: 640, Height: 480})
//	            if err != nil {
//	                return err
//	            }
//	            window = w
//	        case ggfx.FrameEvent:
//	            // Draw ev.Screen.
//	        }
//	        return nil
//	    }), nil)
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	}
//
// The window model and the event contract are documented in docs/window.md.
//
// In the API document, 'the main thread' means the goroutine in init(), main() and their callees without 'go'
// statement. It is assured that 'the main thread' runs on the OS main thread. There are some Ebitengine functions (e.g.,
// AppendMonitors) that must be called on the main thread under some conditions (typically, before
// Run is called).
//
// # Environment variables
//
// `EBITENGINE_SCREENSHOT_KEY` environment variable specifies the key
// `EBITENGINE_GRAPHICS_LIBRARY` environment variable specifies the graphics library.
// If the specified graphics library is not available, Run returns an error.
// This environment variable works when Run is called with GraphicsLibraryAuto.
// This can take one of the following value:
//
//	"auto":         Ebitengine chooses the graphics library automatically. This is the default value.
//	"opengl":       OpenGL, OpenGL ES, or WebGL.
//	"directx":      DirectX. This works only on Windows.
//	"metal":        Metal. This works only on macOS or iOS.
//	"playstation5": PlayStation 5. This works only on PlayStation 5.
//
// `EBITENGINE_DIRECTX` environment variable specifies various parameters for DirectX.
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
// # Build tags
//
// `ebitenginedebug` outputs a log of graphics commands. This is useful to know what happens in Ebitengine. In general, the
// number of graphics commands affects the performance of your game.
//
// `ebitenginegldebug` enables a debug mode for OpenGL. This is valid only when the graphics library is OpenGL.
// This affects performance very much.
//
// `ebitenginesinglethread` disables Ebitengine's thread safety to unlock maximum performance. If you use this you will have
// to manage threads yourself. Functions like `SetWindowSize` will no longer be concurrent-safe with this build tag.
// They must be called from the main thread or the same goroutine as the event handler.
// `ebitenginesinglethread` works only with desktops.
// `ebitenginesinglethread` is deprecated; use RunOptions.SingleThread instead.
//
// `nintendosdk` is for NintendoSDK (e.g. Nintendo Switch).
//
// `nintendosdkprofile` enables a profiler for NintendoSDK.
//
// `playstation5` is for PlayStation 5.
package ggfx
