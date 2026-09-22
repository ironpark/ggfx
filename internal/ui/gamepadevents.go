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
	"time"

	"github.com/ironpark/ggfx/internal/gamepad"
	"github.com/ironpark/ggfx/internal/gamepaddb"
)

// gamepadAxisThreshold is how far an axis must move before the change is reported. Sticks rest
// noisily around zero, and without a threshold an idle gamepad would wake the loop forever.
const gamepadAxisThreshold = 1.0 / 64

// Gamepad poll intervals. A connected gamepad is read often enough to feel immediate; with none
// connected the loop only has to notice one arriving.
//
// macOS registers IOKit device matching callbacks and Linux watches the input directory with
// inotify, so on those platforms a waiting loop could be woken by the backend with
// glfw.PostEmptyEvent instead of by this timer. Windows enumerates DirectInput devices on demand
// and the browser reads navigator.getGamepads, so those have to be asked either way.
const (
	gamepadPollInterval   = time.Second / 120
	gamepadDetectInterval = time.Second
)

const (
	standardButtonCount = int(gamepaddb.StandardButtonMax) + 1
	standardAxisCount   = int(gamepaddb.StandardAxisMax) + 1
)

// gamepadState is one gamepad's inputs as of one poll. Two of them are kept per gamepad and
// swapped, so a steady-state poll allocates nothing.
type gamepadState struct {
	buttons []bool
	axes    []float64

	standardPressed [standardButtonCount]bool
	standardValues  [standardButtonCount]float64
	standardAxes    [standardAxisCount]float64
}

// gamepadEntry tracks one connected gamepad: what it is, which standard inputs it has, and its
// last two polls.
type gamepadEntry struct {
	// standard reports whether the gamepad has a standard layout, and the two masks which of
	// the standard inputs it offers. All three are fixed for a device, so they are resolved
	// once, when it connects: asking the gamepad database costs a lock and a map lookup each.
	standard       bool
	buttonsPresent [standardButtonCount]bool
	axesPresent    [standardAxisCount]bool

	prev gamepadState
	cur  gamepadState

	// gen is the poll this gamepad was last seen in, which is how a disconnection is noticed
	// without building a set per poll.
	gen uint64
}

// gamepadTracker turns polled gamepad state into events. It is owned by the loop and used from the
// loop's goroutine only.
type gamepadTracker struct {
	entries map[gamepad.ID]*gamepadEntry
	ids     []gamepad.ID
	events  []Event
	gen     uint64

	// lastPoll is when the gamepads were last read, so that a loop woken often for another
	// reason does not read them faster than gamepadPollInterval.
	lastPoll time.Time
}

// connected reports whether any gamepad was connected as of the last poll.
func (t *gamepadTracker) connected() bool {
	return len(t.entries) > 0
}

// due reports whether the gamepads should be read now, and records the time if so.
func (t *gamepadTracker) due(now time.Time) bool {
	if !t.lastPoll.IsZero() && now.Sub(t.lastPoll) < gamepadPollInterval {
		return false
	}
	t.lastPoll = now
	return true
}

// resize returns a slice of length n backed by s's array when it is big enough, so that a poll
// after the first allocates nothing.
func resize[T any](s []T, n int) []T {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]T, n)
}

// read fills s with g's current inputs, reusing s's slices.
func (e *gamepadEntry) read(s *gamepadState, g *gamepad.Gamepad) {
	s.buttons = resize(s.buttons, g.ButtonCountWithHats())
	for i := range s.buttons {
		s.buttons[i] = g.IsButtonPressedWithHats(i)
	}

	s.axes = resize(s.axes, g.AxisCount())
	for i := range s.axes {
		s.axes[i] = 0
	}
	for i := range s.axes {
		if g.IsAxisReady(i) {
			s.axes[i] = g.Axis(i)
		}
	}

	for b := range standardButtonCount {
		if !e.buttonsPresent[b] {
			continue
		}
		button := gamepaddb.StandardButton(b)
		s.standardPressed[b] = g.IsStandardButtonPressed(button)
		s.standardValues[b] = g.StandardButtonValue(button)
	}
	for a := range standardAxisCount {
		if e.axesPresent[a] {
			s.standardAxes[a] = g.StandardAxisValue(gamepaddb.StandardAxis(a))
		}
	}
}

// update reads the gamepads and appends the events their difference from the previous poll
// implies: connections first, then the changes on each gamepad, then the disconnections. The
// returned slice is the tracker's and is valid until the next call.
func (t *gamepadTracker) update() []Event {
	if t.entries == nil {
		t.entries = map[gamepad.ID]*gamepadEntry{}
	}
	t.events = t.events[:0]
	t.gen++

	t.ids = gamepad.AppendGamepadIDs(t.ids[:0])
	for _, id := range t.ids {
		g := gamepad.Get(id)
		if g == nil {
			continue
		}
		e, ok := t.entries[id]
		if !ok {
			e = &gamepadEntry{standard: g.IsStandardLayoutAvailable()}
			if e.standard {
				for b := range standardButtonCount {
					e.buttonsPresent[b] = g.IsStandardButtonAvailable(gamepaddb.StandardButton(b))
				}
				for a := range standardAxisCount {
					e.axesPresent[a] = g.IsStandardAxisAvailable(gamepaddb.StandardAxis(a))
				}
			}
			t.entries[id] = e
			t.events = append(t.events, GamepadConnectEvent{
				ID:       id,
				Name:     g.Name(),
				SDLID:    g.SDLID(),
				Standard: e.standard,
			})
		}
		e.gen = t.gen

		e.prev, e.cur = e.cur, e.prev
		e.read(&e.cur, g)
		if ok {
			// A gamepad's first poll is its resting state, not a change to report.
			t.events = e.appendChanges(t.events, id)
		}
	}

	for id, e := range t.entries {
		if e.gen == t.gen {
			continue
		}
		delete(t.entries, id)
		t.events = append(t.events, GamepadDisconnectEvent{ID: id})
	}
	return t.events
}

// moved reports whether an analog value changed enough to be worth an event.
func moved(prev, cur float64) bool {
	return math.Abs(prev-cur) >= gamepadAxisThreshold
}

// appendChanges appends the events for what changed between the gamepad's last two polls.
func (e *gamepadEntry) appendChanges(dst []Event, id gamepad.ID) []Event {
	for i, pressed := range e.cur.buttons {
		if i < len(e.prev.buttons) && e.prev.buttons[i] == pressed {
			continue
		}
		dst = append(dst, GamepadButtonEvent{ID: id, Button: i, Pressed: pressed})
	}
	for i, v := range e.cur.axes {
		if i < len(e.prev.axes) && !moved(e.prev.axes[i], v) {
			continue
		}
		dst = append(dst, GamepadAxisEvent{ID: id, Axis: i, Value: v})
	}

	for b := range standardButtonCount {
		if !e.buttonsPresent[b] {
			continue
		}
		pressed, v := e.cur.standardPressed[b], e.cur.standardValues[b]
		if e.prev.standardPressed[b] == pressed && !moved(e.prev.standardValues[b], v) {
			continue
		}
		dst = append(dst, GamepadStandardButtonEvent{
			ID:      id,
			Button:  gamepaddb.StandardButton(b),
			Pressed: pressed,
			Value:   v,
		})
	}
	for a := range standardAxisCount {
		if !e.axesPresent[a] {
			continue
		}
		if v := e.cur.standardAxes[a]; moved(e.prev.standardAxes[a], v) {
			dst = append(dst, GamepadStandardAxisEvent{ID: id, Axis: gamepaddb.StandardAxis(a), Value: v})
		}
	}
	return dst
}

// gamepadWaitTimeout is how long the idle loop may sleep before it has to read the gamepads again,
// or 0 when it can sleep until the window system wakes it.
func (u *UserInterface) gamepadWaitTimeout() float64 {
	if u.gamepads == nil {
		return 0
	}
	if u.gamepads.connected() {
		return gamepadPollInterval.Seconds()
	}
	return gamepadDetectInterval.Seconds()
}

// emitGamepadEvents queues what changed on the gamepads since the last poll.
func (u *UserInterface) emitGamepadEvents() {
	if u.gamepads == nil {
		return
	}
	u.pushEvents(u.gamepads.update())
}
