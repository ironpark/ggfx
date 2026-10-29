// Copyright 2015 Hajime Hoshi
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

package ggwd

import (
	"github.com/ironpark/ggfx/ggwd/internal/ui"
)

// What is left here asks what a device is, not what it is doing: input state arrives as events.
// See [Run] and the event types in app.go.

// KeyName returns a key name for the current keyboard layout.
// For example, KeyName(KeyQ) returns 'q' for a QWERTY keyboard, and returns 'a' for an AZERTY keyboard.
//
// KeyName returns an empty string if 1) the key doesn't have a physical key name, 2) the platform doesn't support KeyName,
// or 3) the main loop doesn't start yet.
//
// KeyName is supported by desktops and browsers.
//
// KeyName is concurrent-safe.
func KeyName(key Key) string {
	return ui.Get().KeyName(ui.Key(key))
}

// TouchID represents a touch's identifier.
type TouchID int
