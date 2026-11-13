// Copyright 2026 The Ebiten Authors
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

// Package bridge carries to ggfx's other packages, host and vector, what they need of ggfx that
// ggfx does not export. It cannot name ggfx's types, which it would import in a cycle, so they
// are its type parameters.
package bridge

import "github.com/ironpark/ggfx/internal/render"

// ImageHooks are the functions of ggfx's Image, I being *ggfx.Image.
type ImageHooks[I any] struct {
	// ScreenImage returns the image of a surface's screen img: cur, which may be nil, when it
	// already is img's, and a new one when the surface replaced its screen.
	ScreenImage func(cur I, img *render.Image) I

	// Original returns the image img is a sub-image of, or img itself.
	Original func(img I) I

	// SetPending puts p in the pending slot of img, shared by img and its sub-images, replacing
	// what was there. Before the next use of any of them, the image runs the slot's FlushPending
	// with the original image.
	SetPending func(img I, p Pending[I])

	// Pending returns what is in the pending slot of img, or nil.
	Pending func(img I) Pending[I]

	// ClearPending empties the pending slot of img.
	ClearPending func(img I)
}

// Pending is work a package has deferred on an image, done before the image is next used. vector
// keeps its pending fills there, as it cannot add a field to the image.
type Pending[I any] interface {
	// FlushPending does the work on img, the original image. The image does not empty its slot:
	// FlushPending does, before it uses img, as the use would run it again. It may find the
	// slot holding something else, or nothing, when another use of img flushed it first.
	FlushPending(img I)
}

// hooks is the *ImageHooks[*ggfx.Image] ggfx set.
var hooks any

// SetImageHooks sets the hooks. ggfx calls it when it is initialized, which is before any
// package that imports it is.
func SetImageHooks[I any](h *ImageHooks[I]) {
	hooks = h
}

// Image returns the hooks ggfx set. I must be *ggfx.Image.
func Image[I any]() *ImageHooks[I] {
	return hooks.(*ImageHooks[I])
}
