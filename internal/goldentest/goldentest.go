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

// Package goldentest pins drawings by a hash of their pixels in a file of
// lines "name sha256[:12]". The CPU rasterizers draw the same on every
// architecture, so a drawing has one hash.
//
// It imports nothing of the host or a GPU, so that the CPU rasterizers'
// tests can use it.
package goldentest

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"image"
	"image/png"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

var (
	update = flag.Bool("golden.update", false, "rewrite the goldens in testdata/golden.txt")
	dump   = flag.String("golden.dump", "", "write each mismatching drawing as a PNG into this directory")
)

// File is a golden file. Its Header is the comment line it is written
// with.
type File struct {
	Path, Header string

	mu      sync.Mutex
	read    bool
	hashes  map[string]string // by name
	changed bool
}

// Main runs a package's tests and, with -golden.update, writes f with
// what they drew. A package's TestMain calls it.
func (f *File) Main(m *testing.M) {
	flag.Parse()
	code := m.Run()
	if *update && f.changed {
		if err := f.write(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

// Check compares img with the golden drawing called name, or with
// -golden.update records it.
func (f *File) Check(t testing.TB, name string, img *image.RGBA) {
	t.Helper()
	got := Hash(img)
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.read {
		if err := f.load(); err != nil {
			t.Fatal(err)
		}
	}
	if *update {
		f.hashes[name] = got
		f.changed = true
		return
	}
	want, ok := f.hashes[name]
	switch {
	case !ok:
		t.Errorf("%s: no golden; add it with -golden.update", name)
	case got != want:
		t.Errorf("%s: drew %s; want %s", name, got, want)
		if *dump != "" {
			writePNG(t, filepath.Join(*dump, strings.ReplaceAll(name, "/", "_")+".png"), img)
		}
	}
}

// Hash is the hash of img's pixels, row by row, those in its Rect alone.
func Hash(img *image.RGBA) string {
	h := sha256.New()
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		i := img.PixOffset(img.Rect.Min.X, y)
		h.Write(img.Pix[i : i+4*img.Rect.Dx()])
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}

func (f *File) load() error {
	f.hashes, f.read = map[string]string{}, true
	r, err := os.Open(f.Path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer r.Close()
	s := bufio.NewScanner(r)
	for s.Scan() {
		fs := strings.Fields(s.Text())
		if len(fs) != 2 || strings.HasPrefix(fs[0], "#") {
			continue
		}
		f.hashes[fs[0]] = fs[1]
	}
	return s.Err()
}

func (f *File) write() error {
	var b strings.Builder
	b.WriteString(f.Header + "\n")
	for _, name := range slices.Sorted(maps.Keys(f.hashes)) {
		fmt.Fprintf(&b, "%s %s\n", name, f.hashes[name])
	}
	return os.WriteFile(f.Path, []byte(b.String()), 0o644)
}

func writePNG(t testing.TB, path string, img image.Image) {
	t.Helper()
	w, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := png.Encode(w, img); err != nil {
		t.Fatal(err)
	}
}
