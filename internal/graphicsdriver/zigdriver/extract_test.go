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

//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLibraryOnce(t *testing.T) {
	base := t.TempDir()
	data := []byte("not really a library")

	path, err := writeLibraryOnce(base, data)
	if err != nil {
		t.Fatalf("writeLibraryOnce: %v", err)
	}
	if got := filepath.Base(path); got != libFileName() {
		t.Errorf("file name = %q, want %q", got, libFileName())
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("content = %q, %v; want %q", got, err, data)
	}

	// A second call finds the file and leaves it alone.
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := writeLibraryOnce(base, data)
	if err != nil || again != path {
		t.Fatalf("second writeLibraryOnce = %q, %v; want %q", again, err, path)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Errorf("an intact file was replaced")
	}

	// A damaged file is replaced.
	if err := os.WriteFile(path, data[:5], 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := writeLibraryOnce(base, data); err != nil {
		t.Fatalf("writeLibraryOnce over a damaged file: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("content after repair = %q, %v; want %q", got, err, data)
	}

	// Other content goes to another directory, and nothing temporary is left behind.
	other, err := writeLibraryOnce(base, []byte("another library"))
	if err != nil {
		t.Fatalf("writeLibraryOnce: %v", err)
	}
	if filepath.Dir(other) == filepath.Dir(path) {
		t.Errorf("different contents share the directory %q", filepath.Dir(path))
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d files next to the library, want only the library", len(entries))
	}
}

func TestLinkedLibraryPath(t *testing.T) {
	prev := LibraryPath
	t.Cleanup(func() { LibraryPath = prev })

	LibraryPath = ""
	if got := linkedLibraryPath(); got != "" {
		t.Errorf("linkedLibraryPath() = %q with no LibraryPath, want \"\"", got)
	}

	abs := filepath.Join(t.TempDir(), libFileName())
	LibraryPath = abs
	if got := linkedLibraryPath(); got != abs {
		t.Errorf("linkedLibraryPath() = %q, want %q", got, abs)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable: %v", err)
	}
	LibraryPath = filepath.Join("lib", libFileName())
	if got, want := linkedLibraryPath(), filepath.Join(filepath.Dir(exe), "lib", libFileName()); got != want {
		t.Errorf("linkedLibraryPath() = %q, want %q", got, want)
	}
}

func TestEmbeddedLibraryExtracts(t *testing.T) {
	if !HasEmbeddedLibrary() {
		t.Skip("no embedded driver library (build with -tags ggfxzig)")
	}
	path, err := extractEmbeddedLibrary()
	if err != nil {
		t.Fatalf("extractEmbeddedLibrary: %v", err)
	}
	lib, err := openAndVerifyLibrary(path)
	if err != nil {
		t.Fatalf("the embedded library does not load: %v", err)
	}
	closeLibrary(lib.handle)
}
