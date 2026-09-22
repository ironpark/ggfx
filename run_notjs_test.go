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

//go:build !js

package ggfx_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/file"
)

// TestDroppedFilesAbsPather ensures that the file system a desktop DropEvent carries provides
// directory entries and files implementing [ggfx.AbsPather].
func TestDroppedFilesAbsPather(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "foo.txt")
	if err := os.WriteFile(path, []byte("foo"), 0644); err != nil {
		t.Fatal(err)
	}

	vfs, err := file.NewVirtualFS([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	ents, err := fs.ReadDir(vfs, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Fatalf("len(ents): got: %d, want: %d", len(ents), 1)
	}
	e, ok := ents[0].(ggfx.AbsPather)
	if !ok {
		t.Fatalf("%T must implement ggfx.AbsPather", ents[0])
	}
	if got, want := e.AbsPath(), path; got != want {
		t.Errorf("AbsPath(): got: %s, want: %s", got, want)
	}

	f, err := vfs.Open("foo.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = f.Close()
	}()
	a, ok := f.(ggfx.AbsPather)
	if !ok {
		t.Fatalf("%T must implement ggfx.AbsPather", f)
	}
	if got, want := a.AbsPath(), path; got != want {
		t.Errorf("AbsPath(): got: %s, want: %s", got, want)
	}
}
