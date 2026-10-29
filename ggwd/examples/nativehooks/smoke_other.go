// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package main

import (
	"fmt"
	"github.com/ironpark/ggfx/ggwd"
)

func injectNative(*ggwd.Window) error {
	return fmt.Errorf("-smoke currently injects macOS native callbacks only")
}
