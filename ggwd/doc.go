// Copyright 2026 The Ebiten Authors
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

// Package ggwd is the window system ggui is built on: windows, an event loop that delivers
// input and frames as events, and the platform's cursor, clipboard, monitors, IME and native
// hooks. It renders with ggfx (github.com/ironpark/ggfx) through ggfx's public renderer API.
//
// An application starts with [Run], which drives a handler with events. The handler creates its
// windows on [StartEvent] and draws on [FrameEvent]; frames are delivered on request rather than
// on a tick.
//
//	func main() {
//	    var window *ggwd.Window
//	    err := ggwd.Run(ggwd.HandlerFunc(func(ev ggwd.Event) error {
//	        switch ev := ev.(type) {
//	        case ggwd.StartEvent:
//	            w, err := ggwd.NewWindow(&ggwd.WindowOptions{Title: "Title", Width: 640, Height: 480})
//	            if err != nil {
//	                return err
//	            }
//	            window = w
//	        case ggwd.FrameEvent:
//	            // Draw ev.Screen, a *ggfx.Image.
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
// statement. It is assured that 'the main thread' runs on the OS main thread. There are some functions (e.g.,
// AppendMonitors) that must be called on the main thread under some conditions (typically, before
// Run is called).
//
// # Build tags
//
// `ebitenginesinglethread` runs the loop, the handler and the graphics commands on the main thread,
// as RunOptions.SingleThread does. Window methods are then no longer concurrent-safe: they must be
// called from the main thread or the same goroutine as the event handler. It works only with
// desktops.
package ggwd
