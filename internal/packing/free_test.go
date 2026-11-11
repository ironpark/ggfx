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

import (
	"math/rand/v2"
	"testing"
)

// findUnpruned is find as it was before nodes kept freeW and freeH: it walks every subtree.
func findUnpruned(n *Node, width, height int) *Node {
	if n.region.Dx() < width || n.region.Dy() < height || n.used {
		return nil
	}
	if n.child0 == nil {
		return n
	}
	if node := findUnpruned(n.child0, width, height); node != nil {
		return node
	}
	return findUnpruned(n.child1, width, height)
}

func checkFree(t *testing.T, n *Node) {
	t.Helper()
	if n.child0 != nil {
		checkFree(t, n.child0)
		checkFree(t, n.child1)
	}
	w, h := n.freeW, n.freeH
	n.updateFree()
	if n.freeW != w || n.freeH != h {
		t.Fatalf("node %v: free is (%d, %d), want (%d, %d)", n.region, w, h, n.freeW, n.freeH)
	}
}

// TestFreeBoundsPruneNothingThatFits allocates and frees at random, growing the page, and checks
// that the bounds alloc prunes by are current and that it takes the node a full walk would.
func TestFreeBoundsPruneNothingThatFits(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	p := NewPage(64, 64, 1024, 1)
	var nodes []*Node
	for k := range 20000 {
		if len(nodes) > 0 && r.IntN(5) < 2 {
			j := r.IntN(len(nodes))
			p.Free(nodes[j])
			nodes[j] = nodes[len(nodes)-1]
			nodes = nodes[:len(nodes)-1]
		} else {
			w, h := 1+r.IntN(24), 1+r.IntN(24)
			var want *Node
			if p.root != nil {
				want = findUnpruned(p.root, w, h)
			}
			n := p.Alloc(w, h)
			if n == nil {
				continue
			}
			if want != nil && !n.region.In(want.region) {
				t.Fatalf("Alloc(%d, %d) took %v, but a full walk finds room in %v first", w, h, n.region, want.region)
			}
			nodes = append(nodes, n)
		}
		if k%97 == 0 {
			checkFree(t, p.root)
		}
	}
	checkFree(t, p.root)
}
