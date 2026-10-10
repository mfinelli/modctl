/*
 * mod control (modctl): command-line mod manager
 * Copyright © 2026 Mario Finelli
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

package fsutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// HashFile computes the sha256 digest of the file at path and returns it as
// a lowercase hex string.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for hashing: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file contents: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashFileContext is like HashFile but stops early if ctx is canceled, which
// matters when hashing very large files (e.g. archives) in a command that
// has to stay interruptible.
//
// The file is read through buf. A caller that hashes many files can allocate
// one buffer and pass it to every call; a nil or empty buf makes
// HashFileContext allocate a 1 MiB buffer for the call.
func HashFileContext(ctx context.Context, path string, buf []byte) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for hashing: %w", err)
	}
	defer f.Close()

	if len(buf) == 0 {
		buf = make([]byte, 1024*1024)
	}

	h := sha256.New()
	if _, err := CopyWithContext(ctx, h, f, buf); err != nil {
		return "", fmt.Errorf("hash file contents: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
