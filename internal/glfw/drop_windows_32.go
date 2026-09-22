// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows && 386

package glfw

import "syscall"

func dropEnterCallback() uintptr {
	return syscall.NewCallback(func(self, data, keys uintptr, x, y int32, effect *uint32) uintptr {
		return dropEnter(self, data, x, y, effect)
	})
}
func dropOverCallback() uintptr {
	return syscall.NewCallback(func(self, keys uintptr, x, y int32, effect *uint32) uintptr { return dropOver(self, x, y, effect) })
}
func dropCallback() uintptr {
	return syscall.NewCallback(func(self, data, keys uintptr, x, y int32, effect *uint32) uintptr {
		return dropFiles(self, data, x, y, effect)
	})
}
