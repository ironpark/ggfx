// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package ui

import "errors"

// ClipboardText returns the text on the system clipboard, which the browser
// hands out only asynchronously.
func (u *UserInterface) ClipboardText() (string, error) { return "", errors.ErrUnsupported }

// SetClipboardText puts text on the system clipboard, which the browser takes
// only asynchronously.
func (u *UserInterface) SetClipboardText(string) error { return errors.ErrUnsupported }
