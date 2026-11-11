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

import (
	"slices"
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/pool"
)

// The usage callbacks run before an image is used. They are for ggfx's own packages, which reach
// them through internal/bridge: vector runs the fills pending for an image with one.

type usageCallback struct {
	fn func(image *Image)
}

var currentCallbackToken atomic.Int64

func originalImage(img *Image) *Image {
	if img.isSubImage() {
		return img.original
	}
	return img
}

func (i *Image) addUsageCallback(callback func(image *Image)) int64 {
	if i.isSubImage() {
		return i.original.addUsageCallback(callback)
	}
	token := currentCallbackToken.Add(1)

	i.usageCallbacksM.Lock()
	defer i.usageCallbacksM.Unlock()

	if i.usageCallbacks == nil {
		i.usageCallbacks = map[int64]usageCallback{}
	}
	i.usageCallbacks[token] = usageCallback{
		fn: callback,
	}
	i.usageCallbackCount.Store(int32(len(i.usageCallbacks)))
	return token
}

func (i *Image) removeUsageCallback(token int64) {
	if i.isSubImage() {
		i.original.removeUsageCallback(token)
		return
	}

	i.usageCallbacksM.Lock()
	defer i.usageCallbacksM.Unlock()
	delete(i.usageCallbacks, token)
	i.usageCallbackCount.Store(int32(len(i.usageCallbacks)))
}

var theTmpUsageCallbackSlicePool = pool.Pool[*[]usageCallback]{
	New: func() *[]usageCallback {
		slice := make([]usageCallback, 0, 16)
		return &slice
	},
}

func (i *Image) invokeUsageCallbacks() {
	if i.isSubImage() {
		i.original.invokeUsageCallbacks()
		return
	}

	if i.usageCallbackCount.Load() == 0 {
		return
	}

	// Do not allow recursive calls.
	if !i.inUsageCallbacks.CompareAndSwap(false, true) {
		return
	}
	defer i.inUsageCallbacks.Store(false)

	tmpUsageCallbackSlice := theTmpUsageCallbackSlicePool.Get()

	func() {
		i.usageCallbacksM.Lock()
		defer i.usageCallbacksM.Unlock()
		for _, cb := range i.usageCallbacks {
			*tmpUsageCallbackSlice = append(*tmpUsageCallbackSlice, cb)
		}
	}()

	for _, cb := range *tmpUsageCallbackSlice {
		cb.fn(i)
	}

	*tmpUsageCallbackSlice = slices.Delete(*tmpUsageCallbackSlice, 0, len(*tmpUsageCallbackSlice))
	theTmpUsageCallbackSlicePool.Put(tmpUsageCallbackSlice)
}
