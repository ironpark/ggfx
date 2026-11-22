// Copyright 2023 The Ebitengine Authors
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

package atlas

// imageSmallSet is an O(1) intrusive set of images keyed by a fixed slot id on Image.setPos.
type imageSmallSet struct {
	id  uint8
	s   []*Image
	tmp []*Image
}

func (s *imageSmallSet) add(image *Image) {
	if image == nil {
		panic("atlas: nil image cannot be added")
	}
	if image.setPos[s.id] != 0 {
		return
	}
	s.s = append(s.s, image)
	image.setPos[s.id] = len(s.s)
}

func (s *imageSmallSet) remove(image *Image) {
	pos := image.setPos[s.id]
	if pos == 0 {
		return
	}
	idx := pos - 1
	last := len(s.s) - 1
	if idx != last {
		moved := s.s[last]
		s.s[idx] = moved
		moved.setPos[s.id] = pos
	}
	s.s[last] = nil
	s.s = s.s[:last]
	image.setPos[s.id] = 0
}

func (s *imageSmallSet) forEach(f func(*Image)) {
	// Copy images to a temporary buffer since f might modify the original slice s.s (#2729).
	s.tmp = append(s.tmp, s.s...)
	for _, img := range s.tmp {
		f(img)
	}
	clear(s.tmp)
	s.tmp = s.tmp[:0]
}

func (s *imageSmallSet) clear() {
	for i, img := range s.s {
		img.setPos[s.id] = 0
		s.s[i] = nil
	}
	s.s = s.s[:0]
}
