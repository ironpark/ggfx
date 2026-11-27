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

package packing

import "image"

func (p *Page) AllocatedRegion() image.Rectangle {
	var r image.Rectangle
	if p.root == nil {
		return r
	}
	_ = walk(p.root, func(n *Node) error {
		if n.region.In(r) {
			return errSkipChildren
		}
		if n.used {
			r = r.Union(n.region)
			return errSkipChildren
		}
		return nil
	})
	// A node at the right or the bottom edge can stick out of the page.
	return r.Intersect(image.Rect(0, 0, p.width, p.height))
}
