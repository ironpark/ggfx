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

// Package recent tracks the most a reused buffer recently had to hold, so that the buffer can give
// back the room a rare large use took.
package recent

const (
	// Window is how many ticks a Peak covers at least; it covers up to twice as many.
	Window = 60

	// ShrinkFactor is how many times larger than its recent peak a reused buffer may be before it
	// is reallocated smaller. Sizes that alternate within the window keep the peak, so they never
	// reallocate.
	ShrinkFactor = 2
)

// Peak tracks the largest value observed over the last Window to 2*Window ticks, in a clock of its
// own (Add) or one it is told the time of (AdvanceTo).
type Peak struct {
	current  int
	previous int
	// start is the time the window of current began at, and now the time last told.
	start int64
	now   int64
}

// Observe records n at the current time.
func (p *Peak) Observe(n int) {
	p.current = max(p.current, n)
}

// AdvanceTo moves the time to now, which must not be before the last time told.
func (p *Peak) AdvanceTo(now int64) {
	p.now = now
	d := now - p.start
	if d < Window {
		return
	}
	if d < 2*Window {
		p.previous = p.current
	} else {
		// A whole window passed with nothing observed.
		p.previous = 0
	}
	p.current = 0
	p.start = now - d%Window
}

// Add records n, and then that one tick went by.
func (p *Peak) Add(n int) {
	p.Observe(n)
	p.AdvanceTo(p.now + 1)
}

// Max reports the largest value observed over the last Window to 2*Window ticks.
func (p *Peak) Max() int {
	return max(p.current, p.previous)
}
