// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package main

import (
	"fmt"
	"github.com/ironpark/ggfx"
)

func injectNative(*ggfx.Window) error {
	return fmt.Errorf("-smoke currently injects macOS native callbacks only")
}
