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

package ui

import (
	"math"

	"github.com/ironpark/ggfx/internal/gamepad"
	"github.com/ironpark/ggfx/internal/gamepaddb"
)

// gamepadAxisThreshold is how far a stick must move before an axis change is reported. Sticks rest
// noisily around zero, and without a threshold an idle controller would wake the loop forever.
const gamepadAxisThreshold = 1.0 / 64

// gamepadSnapshot is one gamepad's inputs as of the last poll. It is compared with the next poll to
// decide which events to emit; nothing else reads it.
type gamepadSnapshot struct {
	name  string
	sdlID string

	// standard reports whether the gamepad has a standard layout, which decides whether the
	// standard members below are meaningful.
	standard bool

	buttons []bool
	axes    []float64

	standardButtons map[gamepaddb.StandardButton]float64
	standardAxes    map[gamepaddb.StandardAxis]float64
}

// gamepadTracker turns polled gamepad state into events. It is owned by the loop and used from the
// loop's goroutine only.
type gamepadTracker struct {
	prev map[gamepad.ID]*gamepadSnapshot
	ids  []gamepad.ID
}

// connected reports whether any gamepad was connected as of the last poll.
func (t *gamepadTracker) connected() bool {
	return len(t.prev) > 0
}

// snapshot reads one gamepad's current inputs.
func snapshotGamepad(g *gamepad.Gamepad) *gamepadSnapshot {
	s := &gamepadSnapshot{
		name:     g.Name(),
		sdlID:    g.SDLID(),
		standard: g.IsStandardLayoutAvailable(),
	}

	buttonCount := g.ButtonCountWithHats()
	s.buttons = make([]bool, buttonCount)
	for i := range buttonCount {
		s.buttons[i] = g.IsButtonPressedWithHats(i)
	}

	axisCount := g.AxisCount()
	s.axes = make([]float64, axisCount)
	for i := range axisCount {
		if g.IsAxisReady(i) {
			s.axes[i] = g.Axis(i)
		}
	}

	if s.standard {
		s.standardButtons = map[gamepaddb.StandardButton]float64{}
		for b := gamepaddb.StandardButton(0); b <= gamepaddb.StandardButtonMax; b++ {
			if g.IsStandardButtonAvailable(b) {
				s.standardButtons[b] = g.StandardButtonValue(b)
			}
		}
		s.standardAxes = map[gamepaddb.StandardAxis]float64{}
		for a := gamepaddb.StandardAxis(0); a <= gamepaddb.StandardAxisMax; a++ {
			if g.IsStandardAxisAvailable(a) {
				s.standardAxes[a] = g.StandardAxisValue(a)
			}
		}
	}
	return s
}

// update compares the current gamepad state with the previous poll and appends the events the
// difference implies to dst: connections first, then the changes on each gamepad, then the
// disconnections.
func (t *gamepadTracker) update(dst []Event) []Event {
	if t.prev == nil {
		t.prev = map[gamepad.ID]*gamepadSnapshot{}
	}

	t.ids = gamepad.AppendGamepadIDs(t.ids[:0])
	seen := make(map[gamepad.ID]struct{}, len(t.ids))

	for _, id := range t.ids {
		g := gamepad.Get(id)
		if g == nil {
			continue
		}
		seen[id] = struct{}{}
		cur := snapshotGamepad(g)
		prev, ok := t.prev[id]
		t.prev[id] = cur
		if !ok {
			dst = append(dst, GamepadConnectEvent{
				ID:       id,
				Name:     cur.name,
				SDLID:    cur.sdlID,
				Standard: cur.standard,
			})
			// A gamepad's first poll is its resting state, not a change to report.
			continue
		}
		dst = appendGamepadChanges(dst, id, prev, cur)
	}

	for id := range t.prev {
		if _, ok := seen[id]; ok {
			continue
		}
		delete(t.prev, id)
		dst = append(dst, GamepadDisconnectEvent{ID: id})
	}
	return dst
}

// appendGamepadChanges appends the events for what changed between two snapshots of one gamepad.
func appendGamepadChanges(dst []Event, id gamepad.ID, prev, cur *gamepadSnapshot) []Event {
	for i, pressed := range cur.buttons {
		if i < len(prev.buttons) && prev.buttons[i] == pressed {
			continue
		}
		dst = append(dst, GamepadButtonEvent{ID: id, Button: i, Pressed: pressed})
	}
	for i, v := range cur.axes {
		if i < len(prev.axes) && math.Abs(prev.axes[i]-v) < gamepadAxisThreshold {
			continue
		}
		dst = append(dst, GamepadAxisEvent{ID: id, Axis: i, Value: v})
	}

	for b, v := range cur.standardButtons {
		p, ok := prev.standardButtons[b]
		if ok && (p > 0) == (v > 0) && math.Abs(p-v) < gamepadAxisThreshold {
			continue
		}
		dst = append(dst, GamepadStandardButtonEvent{ID: id, Button: b, Pressed: v > 0, Value: v})
	}
	for a, v := range cur.standardAxes {
		if p, ok := prev.standardAxes[a]; ok && math.Abs(p-v) < gamepadAxisThreshold {
			continue
		}
		dst = append(dst, GamepadStandardAxisEvent{ID: id, Axis: a, Value: v})
	}
	return dst
}

// Gamepad poll intervals, in seconds. A connected gamepad is read often enough to feel immediate;
// with none connected the loop only has to notice one arriving.
//
// macOS registers IOKit device matching callbacks and Linux watches the input directory with
// inotify, so on those platforms a waiting loop could be woken by the backend with
// glfw.PostEmptyEvent instead of by this timer. Windows enumerates DirectInput devices on demand
// and the browser reads navigator.getGamepads, so those have to be asked either way.
const (
	gamepadPollInterval   = 1.0 / 120
	gamepadDetectInterval = 1.0
)

// gamepadWaitTimeout is how long the idle loop may sleep before it has to poll gamepads again, or 0
// when it can sleep until the window system wakes it.
func (u *UserInterface) gamepadWaitTimeout() float64 {
	if u.gamepads == nil {
		return 0
	}
	if u.gamepads.connected() {
		return gamepadPollInterval
	}
	return gamepadDetectInterval
}

// emitGamepadEvents polls the gamepads and queues what changed since the last poll.
func (u *UserInterface) emitGamepadEvents() {
	if u.gamepads == nil {
		return
	}
	for _, ev := range u.gamepads.update(nil) {
		u.pushEvent(ev)
	}
}
