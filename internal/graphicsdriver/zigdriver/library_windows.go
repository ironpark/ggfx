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

//go:build windows && (amd64 || arm64)

package zigdriver

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func openLibrary(path string) (*library, error) {
	var flags uintptr
	loadPath := path
	if strings.ContainsAny(path, `\/`) {
		abs, err := filepath.Abs(path)
		if err == nil {
			loadPath = abs
		}
		flags = windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR | windows.LOAD_LIBRARY_SEARCH_DEFAULT_DIRS
	} else {
		flags = windows.LOAD_LIBRARY_SEARCH_APPLICATION_DIR | windows.LOAD_LIBRARY_SEARCH_SYSTEM32
	}

	h, err := windows.LoadLibraryEx(loadPath, 0, flags)
	if err != nil {
		return nil, err
	}
	lib := &library{
		path:   loadPath,
		handle: uintptr(h),
	}
	for _, sym := range lib.symbols() {
		addr, err := windows.GetProcAddress(h, sym.name)
		if err != nil {
			_ = windows.FreeLibrary(h)
			return nil, fmt.Errorf("missing symbol %s: %w", sym.name, err)
		}
		*sym.dst = addr
	}
	return lib, nil
}

func closeLibrary(handle uintptr) {
	if handle != 0 {
		_ = windows.FreeLibrary(windows.Handle(handle))
	}
}
