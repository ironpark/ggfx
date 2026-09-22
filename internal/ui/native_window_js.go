// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package ui

func (w *jsWindow) SetTextInputEnabled(bool)                                            {}
func (w *jsWindow) SetTextInputRect(x, y, width, height float64)                        {}
func (w *jsWindow) AccessibilityView() uintptr                                          { return 0 }
func (w *jsWindow) SetAccessibilityHandlers(func() uintptr, func(x, y float64) uintptr) {}
func (w *jsWindow) SetGetObjectHandler(func(wparam, lparam uintptr) (uintptr, bool))    {}

func (w *jsWindow) SetTextInputContext(before, after string) {}
