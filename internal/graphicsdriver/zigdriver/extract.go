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
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//go:generate bash ../../../scripts/build-zig-prebuilt.sh

// extractEmbeddedLibrary writes the embedded driver library to a file and returns its path,
// since the dynamic loader opens files only.
//
// The file goes under the user's cache directory, in a directory named after the hash of the
// library, so each version is written once and one process never replaces a file another has
// loaded. Without a usable cache directory, the file goes in a fresh temporary directory.
func extractEmbeddedLibrary() (string, error) {
	zr, err := gzip.NewReader(bytes.NewReader(embeddedLib))
	if err != nil {
		return "", fmt.Errorf("embedded library: %w", err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		return "", fmt.Errorf("embedded library: %w", err)
	}

	base, cacheErr := os.UserCacheDir()
	if cacheErr == nil {
		var path string
		if path, cacheErr = writeLibraryOnce(filepath.Join(base, "ggfx"), data); cacheErr == nil {
			return path, nil
		}
	}

	dir, err := os.MkdirTemp("", "ggfx-driver-")
	if err != nil {
		return "", fmt.Errorf("embedded library: %w", errors.Join(cacheErr, err))
	}
	path, err := writeLibraryOnce(dir, data)
	if err != nil {
		return "", fmt.Errorf("embedded library: %w", errors.Join(cacheErr, err))
	}
	return path, nil
}

// writeLibraryOnce makes sure base/driver-<hash>/<library name> holds exactly data and returns
// its path. A file already holding data is left alone; anything else is replaced by a rename,
// so a concurrent reader sees the old file or the new one, never a partial write.
func writeLibraryOnce(base string, data []byte) (string, error) {
	sum := sha256.Sum256(data)
	dir := filepath.Join(base, "driver-"+hex.EncodeToString(sum[:8]))
	path := filepath.Join(dir, libFileName())

	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, data) {
		return path, nil
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, libFileName()+".tmp-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(0o755)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		// Windows refuses to replace a library another process has loaded. That process
		// may have just written the same file, in which case there is nothing left to do.
		if cur, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(cur, data) {
			return path, nil
		}
		return "", err
	}
	return path, nil
}
