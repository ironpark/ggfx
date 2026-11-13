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

// Package goldentest pins drawings by a hash of their pixels, per GOARCH,
// in a file of lines "name GOARCH sha256[:12]". The hashes differ by
// GOARCH: arm64 fuses multiplies and adds that amd64 and wasm do not, and
// x/image/vector's floating-point accumulator, which the rasterizer
// follows, sums in a different order on amd64.
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
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

var (
	update = flag.Bool("golden.update", false, "rewrite this GOARCH's goldens in testdata/golden.txt")
	dump   = flag.String("golden.dump", "", "write each mismatching drawing as a PNG into this directory")
)

// File is a golden file. Its Header is the comment line it is written
// with.
type File struct {
	Path, Header string

	mu      sync.Mutex
	read    bool
	hashes  map[string]map[string]string // by name, then GOARCH
	archs   map[string]bool              // the GOARCHes the file has goldens for
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
// -golden.update records it. A GOARCH the file has no goldens for skips
// t; one it has, but not for name, fails it.
func (f *File) Check(t testing.TB, name string, img *image.RGBA) {
	t.Helper()
	got := Hash(img)
	arch := runtime.GOARCH
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.read {
		if err := f.load(); err != nil {
			t.Fatal(err)
		}
	}
	if *update {
		if f.hashes[name] == nil {
			f.hashes[name] = map[string]string{}
		}
		f.hashes[name][arch] = got
		f.changed = true
		return
	}
	want, ok := f.hashes[name][arch]
	switch {
	case !ok && !f.archs[arch]:
		t.Skipf("%s: no goldens for GOARCH=%s; add them with -golden.update", name, arch)
	case !ok:
		t.Errorf("%s: no golden for GOARCH=%s; add it with -golden.update", name, arch)
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
	f.hashes, f.archs, f.read = map[string]map[string]string{}, map[string]bool{}, true
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
		if len(fs) != 3 || strings.HasPrefix(fs[0], "#") {
			continue
		}
		if f.hashes[fs[0]] == nil {
			f.hashes[fs[0]] = map[string]string{}
		}
		f.hashes[fs[0]][fs[1]] = fs[2]
		f.archs[fs[1]] = true
	}
	return s.Err()
}

func (f *File) write() error {
	var b strings.Builder
	b.WriteString(f.Header + "\n")
	for _, name := range slices.Sorted(maps.Keys(f.hashes)) {
		for _, arch := range slices.Sorted(maps.Keys(f.hashes[name])) {
			fmt.Fprintf(&b, "%s %s %s\n", name, arch, f.hashes[name][arch])
		}
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
