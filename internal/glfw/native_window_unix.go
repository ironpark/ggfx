// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

//go:build freebsd || linux || netbsd

package glfw

func (w *Window) platformEnableDrag() error          { return nil }
func (w *Window) platformSetTextInputEnabled(bool)   {}
func (w *Window) platformSetTextInputRect()          {}
func (w *Window) platformAccessibilityView() uintptr { return 0 }
