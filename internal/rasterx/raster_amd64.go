// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !appengine && gc && !noasm

package rasterx

import "golang.org/x/sys/cpu"

// haveAccumulateSIMD is whether golang.org/x/image/vector sums its mask with
// its SSE4.1 assembly, in an order of its own.
var haveAccumulateSIMD = cpu.X86.HasSSE41
