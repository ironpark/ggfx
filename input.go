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

package ggfx

import (
	"github.com/ironpark/ggfx/internal/gamepad"
	"github.com/ironpark/ggfx/internal/gamepaddb"
	"github.com/ironpark/ggfx/internal/ui"
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

// GamepadID represents a gamepad identifier.
type GamepadID = gamepad.ID

// GamepadSDLID returns a string with the GUID generated in the same way as SDL.
// To detect devices, see also the community project of gamepad devices database: https://github.com/gabomdq/SDL_GameControllerDB
//
// GamepadSDLID returns an empty string on consoles, where no such GUID exists.
//
// GamepadSDLID returns an empty string before the game starts.
//
// GamepadSDLID is concurrent-safe.
func GamepadSDLID(id GamepadID) string {
	g := gamepad.Get(id)
	if g == nil {
		return ""
	}
	return g.SDLID()
}

// GamepadName returns a string with the name.
// This function may vary in how it returns descriptions for the same device across platforms.
// for example the following drivers/platforms see an Xbox One controller as the following:
//
//   - Windows: "Xbox Controller"
//   - Chrome: "Xbox 360 Controller (XInput STANDARD GAMEPAD)"
//   - Firefox: "xinput"
//
// GamepadName returns an empty string before the game starts.
//
// GamepadName is concurrent-safe.
func GamepadName(id GamepadID) string {
	g := gamepad.Get(id)
	if g == nil {
		return ""
	}
	return g.Name()
}

// AppendGamepadIDs appends available gamepad IDs to gamepadIDs, and returns the extended buffer.
// Giving a slice that already has enough capacity works efficiently.
//
// AppendGamepadIDs appends no ID before the game starts.
//
// AppendGamepadIDs is concurrent-safe.
func AppendGamepadIDs(gamepadIDs []GamepadID) []GamepadID {
	return gamepad.AppendGamepadIDs(gamepadIDs)
}

// GamepadAxisCount returns the number of axes of the gamepad (id).
//
// GamepadAxisCount returns 0 before the game starts.
//
// GamepadAxisCount is concurrent-safe.
func GamepadAxisCount(id GamepadID) int {
	g := gamepad.Get(id)
	if g == nil {
		return 0
	}
	return g.AxisCount()
}

// GamepadButtonCount returns the number of the buttons of the given gamepad (id).
//
// GamepadButtonCount returns 0 before the game starts.
//
// GamepadButtonCount is concurrent-safe.
func GamepadButtonCount(id GamepadID) int {
	g := gamepad.Get(id)
	if g == nil {
		return 0
	}

	// For backward compatibility, hats are treated as buttons in GLFW.
	return g.ButtonCountWithHats()
}

// IsStandardGamepadLayoutAvailable reports whether the gamepad (id) has a standard gamepad layout mapping.
//
// IsStandardGamepadLayoutAvailable returns false before the game starts.
//
// IsStandardGamepadLayoutAvailable is concurrent-safe.
func IsStandardGamepadLayoutAvailable(id GamepadID) bool {
	g := gamepad.Get(id)
	if g == nil {
		return false
	}
	return g.IsStandardLayoutAvailable()
}

// IsStandardGamepadAxisAvailable reports whether the standard gamepad axis is available on the gamepad (id).
//
// IsStandardGamepadAxisAvailable returns false before the game starts.
//
// IsStandardGamepadAxisAvailable is concurrent-safe.
func IsStandardGamepadAxisAvailable(id GamepadID, axis StandardGamepadAxis) bool {
	g := gamepad.Get(id)
	if g == nil {
		return false
	}
	return g.IsStandardAxisAvailable(axis)
}

// IsStandardGamepadButtonAvailable reports whether the standard gamepad button is available on the gamepad (id).
//
// IsStandardGamepadButtonAvailable returns false before the game starts.
//
// IsStandardGamepadButtonAvailable is concurrent-safe.
func IsStandardGamepadButtonAvailable(id GamepadID, button StandardGamepadButton) bool {
	g := gamepad.Get(id)
	if g == nil {
		return false
	}
	return g.IsStandardButtonAvailable(button)
}

// UpdateStandardGamepadLayoutMappings parses the specified string mappings in SDL_GameControllerDB format and
// updates the gamepad layout definitions.
//
// UpdateStandardGamepadLayoutMappings reports whether the mappings were applied,
// and returns an error in case any occurred while parsing the mappings.
//
// One or more input definitions can be provided separated by newlines.
// In particular, it is valid to pass an entire gamecontrollerdb.txt file.
// Note though that Ebitengine already includes its own copy of this file,
// so this call should only be necessary to add mappings for hardware not supported yet;
// ideally games using the StandardGamepad* functions should allow the user to provide mappings and
// then call this function if provided.
// When using this facility to support new hardware, please also send a pull request to
// https://github.com/gabomdq/SDL_GameControllerDB to make your mapping available to everyone else.
//
// A platform field in a line corresponds with a GOOS like the following:
//
//	"Windows":  GOOS=windows
//	"Mac OS X": GOOS=darwin (not ios)
//	"Linux":    GOOS=linux (not android)
//	"Android":  GOOS=android
//	"iOS":      GOOS=ios
//	"":         Any GOOS
//
// UpdateStandardGamepadLayoutMappings is concurrent-safe.
//
// UpdateStandardGamepadLayoutMappings mappings take effect immediately even for already connected gamepads.
//
// UpdateStandardGamepadLayoutMappings works atomically. If an error happens, nothing is updated.
func UpdateStandardGamepadLayoutMappings(mappings string) (bool, error) {
	if err := gamepaddb.Update([]byte(mappings)); err != nil {
		return false, err
	}
	return true, nil
}

// TouchID represents a touch's identifier.
type TouchID int
