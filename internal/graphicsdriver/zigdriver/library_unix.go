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

//go:build ((darwin && !ios) || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"fmt"

	"github.com/ebitengine/purego"
)

func openLibrary(path string) (*library, error) {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	lib := &library{
		path:   path,
		handle: h,
	}
	for _, sym := range lib.symbols() {
		addr, err := purego.Dlsym(h, sym.name)
		if err != nil {
			_ = purego.Dlclose(h)
			return nil, fmt.Errorf("missing symbol %s: %w", sym.name, err)
		}
		*sym.dst = addr
	}
	return lib, nil
}

func closeLibrary(handle uintptr) {
	if handle != 0 {
		_ = purego.Dlclose(handle)
	}
}
