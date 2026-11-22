// Copyright 2025 The Ebitengine Authors
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

//go:build darwin && !ios

package metal

import (
	"time"

	"github.com/ebitengine/purego"

	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/ca"
)

func (v *view) initDisplayLink() error {
	return v.initCADisplayLink()
}

var (
	cvDisplayLinkCreateWithActiveCGDisplays func(displayLinkOut *uintptr) int32
	cvDisplayLinkSetOutputCallback          func(displayLink uintptr, callback uintptr, userInfo uintptr) int32
	cvDisplayLinkStart                      func(displayLink uintptr) int32
	cvDisplayLinkStop                       func(displayLink uintptr) int32
	cvDisplayLinkRelease                    func(displayLink uintptr)
)

func init() {
	coreVideo, err := purego.Dlopen("/System/Library/Frameworks/CoreVideo.framework/CoreVideo", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&cvDisplayLinkCreateWithActiveCGDisplays, coreVideo, "CVDisplayLinkCreateWithActiveCGDisplays")
	purego.RegisterLibFunc(&cvDisplayLinkSetOutputCallback, coreVideo, "CVDisplayLinkSetOutputCallback")
	purego.RegisterLibFunc(&cvDisplayLinkStart, coreVideo, "CVDisplayLinkStart")
	purego.RegisterLibFunc(&cvDisplayLinkStop, coreVideo, "CVDisplayLinkStop")
	purego.RegisterLibFunc(&cvDisplayLinkRelease, coreVideo, "CVDisplayLinkRelease")
}

var displayLinkOutputCallbackPtr = purego.NewCallback(displayLinkOutputCallback)

func (v *view) initCADisplayLink() error {
	v.fence = newFence()

	// TODO: CVDisplayLink APIs are deprecated in macOS 10.15 and later.
	// Use new APIs like NSView.displayLink(target:selector:).
	var displayLinkRef uintptr
	if ret := cvDisplayLinkCreateWithActiveCGDisplays(&displayLinkRef); ret != kCVReturnSuccess {
		// Failed to get the display link, so proceed without it.
		return nil
	}
	v.handleToSelf = newViewHandle(v)
	cvDisplayLinkSetOutputCallback(displayLinkRef, displayLinkOutputCallbackPtr, uintptr(v.handleToSelf))

	// The display link starts with the first frame that waits for it.
	v.linkMu.Lock()
	v.caDisplayLink, v.linkStopped = displayLinkRef, true
	v.linkMu.Unlock()
	return nil
}

// releaseDisplayLink releases the display link created at initCADisplayLink.
func (v *view) releaseDisplayLink() {
	v.linkMu.Lock()
	defer v.linkMu.Unlock()
	if v.linkIdleTimer != nil {
		v.linkIdleTimer.Stop()
	}
	if v.caDisplayLink == 0 {
		return
	}

	// CVDisplayLinkStop returns after the output callback finishes, so the view handle is no
	// longer used by the callback after this.
	cvDisplayLinkStop(v.caDisplayLink)
	cvDisplayLinkRelease(v.caDisplayLink)
	v.caDisplayLink = 0

	deleteViewHandle(v.handleToSelf)
	v.handleToSelf = 0
}

// displayLinkOutputCallback is the callback function for CVDisplayLink.
// The signature matches CVDisplayLinkOutputCallback:
// CVReturn (*CVDisplayLinkOutputCallback)(CVDisplayLinkRef displayLink, const CVTimeStamp *inNow, const CVTimeStamp *inOutputTime, CVOptionFlags flagsIn, CVOptionFlags *flagsOut, void *displayLinkContext)
func displayLinkOutputCallback(displayLink uintptr, inNow, inOutputTime uintptr, flagsIn uint64, flagsOut *uint64, displayLinkContext uintptr) int32 {
	h := viewHandle(displayLinkContext)
	view := h.Value()
	if view == nil {
		return 0
	}
	view.fence.advance()
	return 0
}

// updatePresentationState takes the window state the window system last set (see setWindowState)
// for the frame. This must be called at the beginning of a frame.
func (v *view) updatePresentationState() {
	v.liveResizing.Store(v.windowLiveResizing.Load())

	if v.chain != nil {
		return
	}

	// presentsWithTransaction is enabled unless the window is in the fullscreen mode (#1745,
	// #1974). Toggling presentsWithTransaction drops the current layer content immediately and
	// irrecoverably, showing the window background for a moment (#3478). The property must not
	// be toggled at the beginning of window resizing, so it is kept enabled during the entire
	// windowed lifetime instead.
	presentsWithTransaction := !v.windowFullscreen.Load()
	if v.presentsWithTransaction != presentsWithTransaction {
		if presentsWithTransaction {
			// Wait until all the drawables queued for asynchronous presentation are presented.
			// Otherwise, a pending presentation can replace the layer content after the first
			// transaction-synced presentation, and a stale frame is shown for a moment.
			for start := time.Now(); v.queuedPresents.Load() > 0 && time.Since(start) < 100*time.Millisecond; {
				time.Sleep(time.Millisecond)
			}
		}
		set := func() {
			v.ml.SetPresentsWithTransaction(presentsWithTransaction)
		}
		if v.runOnMainThread != nil {
			v.runOnMainThread(set)
		} else {
			set()
		}
		v.presentsWithTransaction = presentsWithTransaction
	}
}

func (v *view) nextDrawable() ca.MetalDrawable {
	v.applyDrawableSizeIfNeeded()
	v.waitForDisplayLinkOutputCallback()

	d, err := v.ml.NextDrawable()
	if err != nil {
		// Drawable is nil. This can happen at the initial state. Let's wait and see.
		return ca.MetalDrawable{}
	}
	return d
}
