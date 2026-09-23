// Copyright 2022 The Ebiten Authors
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
	"time"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

var (
	NearestFilterShader = &Shader{shader: atlas.NearestFilterShader}
	LinearFilterShader  = &Shader{shader: atlas.LinearFilterShader}
)

// framePacer paces the loop when presenting does not wait for the display.
type framePacer struct {
	lastSwapBufferTime time.Time

	// vsyncIgnored reports whether swapping buffers does not wait for the display even though vsync
	// is enabled. The loop must then be paced explicitly.
	vsyncIgnored bool

	// vsyncIgnoredCount is the number of the successive frames that were swapped too early for the
	// display to have shown them.
	vsyncIgnoredCount int
}

func (c *framePacer) flushCommandsAndWait(needsSwapBuffers bool, graphicsDriver graphicsdriver.Graphics, refreshRate int) error {
	if err := atlas.FlushCommands(graphicsDriver, needsSwapBuffers); err != nil {
		return err
	}

	// Swapping buffers for an invisible screen returns without waiting for the display. Pace such a
	// frame like a skipped swap, or the loop would run as fast as the CPU allows (#2181).
	var occluded bool
	if o, ok := graphicsDriver.(interface{ IsOccluded() bool }); ok {
		occluded = o.IsOccluded()
	}

	now := time.Now()

	var waitTime time.Duration
	if !needsSwapBuffers || occluded {
		// A frame that is not swapped tells nothing about whether swapping buffers waits for the
		// display, and breaks the run of the frames that returned early.
		c.vsyncIgnoredCount = 0
		// Nothing waited for the display, so sleep to keep the loop off the CPU (#2890).
		waitTime = time.Second / 60
	} else {
		// In some environments, e.g. Linux on Parallels, SwapBuffers doesn't wait for the vsync (#2952).
		// In the case when the display has high refresh rates like 240 [Hz], the wait time should be small.
		waitTime = time.Millisecond

		// A graphics driver can be configured to force the vsync off, and then swapping buffers never
		// waits for the display (#3009). Pace the loop at the refresh rate, as nothing else does.
		if refreshRate > 0 {
			refreshInterval := time.Second / time.Duration(refreshRate)
			if c.updateVsyncIgnored(now.Sub(c.lastSwapBufferTime), refreshInterval) {
				waitTime = refreshInterval
			}
		}
	}

	// Pace with an absolute deadline to avoid drift.
	if waitTime > 0 {
		if next := c.lastSwapBufferTime.Add(waitTime); next.After(now) {
			time.Sleep(next.Sub(now))
			c.lastSwapBufferTime = next
			return nil
		}
	}
	c.lastSwapBufferTime = now

	return nil
}

// resetVsyncDetection makes whether swapping buffers waits for the display measured again.
func (c *framePacer) resetVsyncDetection() {
	c.vsyncIgnored = false
	c.vsyncIgnoredCount = 0
}

// updateVsyncIgnored records how long one frame took, including swapping its buffers, and reports
// whether swapping buffers turns out not to wait for the display.
func (c *framePacer) updateVsyncIgnored(frameTime, refreshInterval time.Duration) bool {
	if c.vsyncIgnored {
		return true
	}

	// A swap can return early while the graphics driver still has room to queue frames. Require a
	// long run of early frames to tell an environment that never waits from such a burst.
	const threshold = 30

	if frameTime >= refreshInterval/2 {
		c.vsyncIgnoredCount = 0
		return false
	}

	c.vsyncIgnoredCount++
	if c.vsyncIgnoredCount < threshold {
		return false
	}

	c.vsyncIgnored = true
	return true
}

// monitorDeviceScaleFactor returns the current monitor's device scale factor, or 1 when no monitor
// is available.
func (u *UserInterface) monitorDeviceScaleFactor() float64 {
	m := u.Monitor()
	if m == nil {
		return 1
	}
	return m.DeviceScaleFactor()
}

// LogicalPositionToClientPositionInNativePixels converts a logical position to a client-area
// position in native pixels. A logical position is already a client position in
// device-independent pixels: an app window has no letterboxing.
func (u *UserInterface) LogicalPositionToClientPositionInNativePixels(x, y float64) (float64, float64) {
	s := u.monitorDeviceScaleFactor()
	x = dipToNativePixels(x, s)
	y = dipToNativePixels(y, s)
	return x, y
}
