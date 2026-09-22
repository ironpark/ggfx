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

//go:build !android && !ios && !js && !nintendosdk && !playstation5

package ui

import (
	"github.com/ironpark/ggfx/internal/glfw"
)

var glfwMouseButtonToMouseButton = map[glfw.MouseButton]MouseButton{
	glfw.MouseButtonLeft:   MouseButton0,
	glfw.MouseButtonMiddle: MouseButton1,
	glfw.MouseButtonRight:  MouseButton2,
	glfw.MouseButton4:      MouseButton3,
	glfw.MouseButton5:      MouseButton4,
}

func (u *glfwBackend) registerInputCallbacks() error {
	aw := u.appWindow()

	if _, err := u.window.SetKeyCallback(func(w *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
		uk, ok := glfwKeyToUIKey[key]
		if !ok {
			return
		}
		if aw != nil {
			u.pushEvent(KeyEvent{
				Window:    aw,
				Key:       uk,
				Pressed:   action != glfw.Release,
				Repeat:    action == glfw.Repeat,
				Modifiers: KeyModifiers{Shift: mods&glfw.ModShift != 0, Control: mods&glfw.ModControl != 0, Alt: mods&glfw.ModAlt != 0, Meta: mods&glfw.ModSuper != 0},
			})
		}
	}); err != nil {
		return err
	}

	if _, err := u.window.SetMouseButtonCallback(func(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		// Ignore key repeats for now.
		if action == glfw.Repeat {
			return
		}

		ub, ok := glfwMouseButtonToMouseButton[button]
		if !ok {
			return
		}
		if aw != nil {
			x, y, err := u.window.GetCursorPos()
			if err != nil {
				u.setError(err)
				return
			}
			x, y = u.cursorPositionInDIP(x, y)
			u.pushEvent(MouseButtonEvent{
				Window:  aw,
				Button:  ub,
				Pressed: action == glfw.Press,
				X:       x,
				Y:       y,
			})
		}
	}); err != nil {
		return err
	}

	// The character callback skips the characters that are produced with the modifier combinations
	// the platform treats as shortcuts, like Ctrl+= on X11 (#3502).
	if _, err := u.window.SetCharCallback(func(w *glfw.Window, char rune) {
		if aw != nil {
			u.pushEvent(TextEvent{Window: aw, Text: string(char)})
		}
	}); err != nil {
		return err
	}

	if _, err := u.window.SetScrollCallback(func(w *glfw.Window, xoff float64, yoff float64) {
		if aw != nil {
			u.pushEvent(ScrollEvent{Window: aw, X: xoff, Y: yoff})
		}
	}); err != nil {
		return err
	}

	if aw != nil {
		if _, err := u.window.SetCursorPosCallback(func(w *glfw.Window, x, y float64) {
			x, y = u.cursorPositionInDIP(x, y)
			u.pushEvent(MouseMoveEvent{Window: aw, X: x, Y: y})
		}); err != nil {
			return err
		}
		if _, err := u.window.SetFocusCallback(func(w *glfw.Window, focused bool) {
			u.pushEvent(FocusEvent{Window: aw, Focused: focused})
		}); err != nil {
			return err
		}
	}

	return nil
}

func (u *glfwBackend) KeyName(key Key) string {
	gk, ok := uiKeyToGLFWKey[key]
	if !ok {
		return ""
	}

	var name string
	u.mainThread.Call(func() {
		if u.isTerminated() {
			return
		}
		n, err := glfw.GetKeyName(gk, 0)
		if err != nil {
			u.setError(err)
			return
		}
		name = n
	})
	return name
}
