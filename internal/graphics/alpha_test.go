// Copyright 2026 The Ebitengine Authors
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

package graphics_test

import (
	"bytes"
	"testing"

	"github.com/ironpark/ggfx/internal/graphics"
)

func TestExpandAlpha(t *testing.T) {
	src := []byte{0, 0x40, 0xff}
	dst := make([]byte, 12)
	graphics.ExpandAlpha(dst, src)
	want := []byte{0, 0, 0, 0, 0x40, 0x40, 0x40, 0x40, 0xff, 0xff, 0xff, 0xff}
	if !bytes.Equal(dst, want) {
		t.Errorf("got %v, want %v", dst, want)
	}
}

func TestExtractChannel(t *testing.T) {
	src := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	for c, want := range [][]byte{{1, 5}, {2, 6}, {3, 7}, {4, 8}} {
		dst := make([]byte, 2)
		graphics.ExtractChannel(dst, src, c)
		if !bytes.Equal(dst, want) {
			t.Errorf("channel %d: got %v, want %v", c, dst, want)
		}
	}
}

func TestAlphaConversionLengths(t *testing.T) {
	for name, f := range map[string]func(){
		"ExpandAlpha":    func() { graphics.ExpandAlpha(make([]byte, 3), make([]byte, 1)) },
		"ExtractChannel": func() { graphics.ExtractChannel(make([]byte, 1), make([]byte, 3), 0) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s with a wrong length did not panic", name)
				}
			}()
			f()
		}()
	}
}
