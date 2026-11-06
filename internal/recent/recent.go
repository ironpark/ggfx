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
// back the room a rare large use took. It also keeps the frame clock that it, and whatever else
// ages by frames, counts time in.
package recent

import "sync/atomic"

// now is the frame clock.
var now atomic.Int64

// Now returns the frame clock: how many frames have ended. It can be called from any goroutine.
func Now() int64 {
	return now.Load()
}

// Advance tells the clock that a frame ended, and returns the new time. Only the frame loop calls
// it, once a frame.
func Advance() int64 {
	return now.Add(1)
}

const (
	// Window is how many ticks a Peak covers at least; it covers up to twice as many.
	Window = 60

	// ShrinkFactor is how many times larger than its recent peak a reused buffer may be before it
	// is reallocated smaller. Sizes that alternate within the window keep the peak, so they never
	// reallocate.
	ShrinkFactor = 2
)

// Peak tracks the largest value observed over the last Window to 2*Window ticks, in the frame clock
// (ObserveNow) or a clock it is told the time of (AdvanceTo).
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

// ObserveNow records n at the frame clock's time: values are recorded as often as they come, and
// age by frames.
func (p *Peak) ObserveNow(n int) {
	if t := Now(); t > p.now {
		p.AdvanceTo(t)
	}
	p.Observe(n)
}

// Max reports the largest value observed over the last Window to 2*Window ticks.
func (p *Peak) Max() int {
	return max(p.current, p.previous)
}

// Capacity records n, the size a reused buffer must hold now, at the frame clock's time, and
// returns the capacity the buffer should have. It is capacity itself while that holds n and is no
// more than ShrinkFactor times the recent peak, or minimum if larger; otherwise it is the power of
// two that holds the recent peak, so a buffer grown for a rare large use gives its room back once
// the use is out of the window.
func (p *Peak) Capacity(capacity, n, minimum int) int {
	p.ObserveNow(n)
	limit := max(p.Max(), minimum)
	if capacity >= n && capacity <= ShrinkFactor*limit {
		return capacity
	}
	return NextPowerOf2(limit)
}

// NextPowerOf2 returns the least power of two that is x or more, or x itself if no power of two of
// its type is.
func NextPowerOf2[T ~int | ~uintptr](x T) T {
	p := T(1)
	for p < x {
		if p<<1 <= p {
			return x
		}
		p <<= 1
	}
	return p
}
