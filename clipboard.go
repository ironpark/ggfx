// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package ggfx

import "github.com/ironpark/ggfx/internal/ui"

// ClipboardText returns the text on the system clipboard: the CLIPBOARD
// selection on X11 and the general pasteboard on macOS, or "" when it holds
// none. It needs Run to have started. On Windows and in the browser it returns an error that wraps
// errors.ErrUnsupported.
//
// ClipboardText can be called from any goroutine but the main thread's.
func ClipboardText() (string, error) {
	return ui.Get().ClipboardText()
}

// SetClipboardText puts text on the system clipboard; see ClipboardText. On
// X11 the app owns what it puts there, so it is gone once the app ends unless
// a clipboard manager took a copy, as desktops' usually do.
//
// SetClipboardText can be called from any goroutine but the main thread's.
func SetClipboardText(text string) error {
	return ui.Get().SetClipboardText(text)
}
