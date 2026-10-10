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
	"os"
)

// HashFile computes the sha256 digest of the file at path and returns it as
// a lowercase hex string. It stops early if ctx is canceled, which matters
// when hashing very large files (such as archives) in a command that has to
// stay interruptible.
//
// HashFile is safe to call from several goroutines at once: every call reads
// through a buffer of its own, sized to the file, and shares no state with any
// other call. Please keep it that way (for instance, don't cache a buffer
// between calls to save allocations).
func HashFile(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for hashing: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	buf := make([]byte, bufferSizeOf(f))
	if _, err := CopyWithContext(ctx, h, f, buf); err != nil {
		return "", fmt.Errorf("hash file contents: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
