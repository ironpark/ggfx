// Copyright 2026 The ggfx Authors
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

//go:build js

package host_test

import (
	"syscall/js"

	"github.com/ironpark/ggfx/host"
)

func newCanvasSurface() (ok bool, err error) {
	doc := js.Global().Get("document")
	if doc.IsUndefined() || doc.IsNull() {
		return false, nil
	}
	c := doc.Call("createElement", "canvas")
	doc.Get("body").Call("appendChild", c)
	if _, err := host.NewSurface(host.Canvas(c), false); err != nil {
		return false, err
	}
	return true, nil
}
