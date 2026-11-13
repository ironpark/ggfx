// Copyright 2014 Hajime Hoshi
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

package ggfx

import "github.com/ironpark/ggfx/internal/bridge"

// The pending slot holds the work ggfx's own packages deferred on an image, which runs before the
// image is used. They reach it through internal/bridge: vector keeps the fills pending for an image
// there. An image and its sub-images share the original image's slot.

func originalImage(img *Image) *Image {
	if img.isSubImage() {
		return img.original
	}
	return img
}

func (i *Image) setPending(p bridge.Pending[*Image]) {
	i = originalImage(i)
	i.pendingM.Lock()
	defer i.pendingM.Unlock()
	i.pending = p
	i.hasPending.Store(p != nil)
}

func (i *Image) pendingWork() bridge.Pending[*Image] {
	i = originalImage(i)
	if !i.hasPending.Load() {
		return nil
	}
	i.pendingM.Lock()
	defer i.pendingM.Unlock()
	return i.pending
}

func (i *Image) clearPending() {
	i.setPending(nil)
}

// flushPending runs the image's pending work, if any, before a use. The work empties the slot
// itself, under its own lock, so that a use racing with it on another goroutine waits for it
// rather than skipping it.
func (i *Image) flushPending() {
	i = originalImage(i)
	if p := i.pendingWork(); p != nil {
		p.FlushPending(i)
	}
}
