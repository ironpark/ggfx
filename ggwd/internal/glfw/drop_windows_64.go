// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows && (amd64 || arm64)

package glfw

import "syscall"

// POINTL is passed by value in one 64-bit argument on Win64.
func dropEnterCallback() uintptr {
	return syscall.NewCallback(func(self, data, keys, point uintptr, effect *uint32) uintptr {
		return dropEnter(self, data, int32(point), int32(point>>32), effect)
	})
}
func dropOverCallback() uintptr {
	return syscall.NewCallback(func(self, keys, point uintptr, effect *uint32) uintptr {
		return dropOver(self, int32(point), int32(point>>32), effect)
	})
}
func dropCallback() uintptr {
	return syscall.NewCallback(func(self, data, keys, point uintptr, effect *uint32) uintptr {
		return dropFiles(self, data, int32(point), int32(point>>32), effect)
	})
}
