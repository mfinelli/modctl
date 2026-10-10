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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// the mode of the files that CopyFile writes
const copyFileMode = 0o644

// CopyWithContext copies bytes from src to dst using the provided buffer,
// periodically checking ctx for cancellation.
//
// It behaves similarly to io.CopyBuffer, but allows the caller to cancel
// long-running copy operations (e.g., very large archives) via context.
//
// The function:
//   - Reads into the provided reusable buffer (no allocations inside the loop)
//   - Writes each chunk fully before proceeding
//   - Returns the total number of bytes successfully written
//   - Stops early if ctx is canceled
//
// This is useful when ingesting large blobs where we want the CLI to remain
// interruptible (Ctrl+C, timeouts, etc.) without relying on OS-level signals
// to interrupt a blocking read.
func CopyWithContext(ctx context.Context, dst io.Writer, src io.Reader, buf []byte) (int64, error) {
	var total int64

	for {
		// Allow cancellation between read iterations.
		// We intentionally check before reading to avoid unnecessary work.
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		// Read up to len(buf) bytes.
		nr, er := src.Read(buf)
		if nr > 0 {
			// Write exactly what was read.
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				total += int64(nw)
			}
			if ew != nil {
				return total, ew
			}
			// Defensive check: partial writes should not happen for
			// well-behaved writers; treat as error.
			if nw != nr {
				return total, io.ErrShortWrite
			}
		}

		// Handle read result
		if er != nil {
			if errors.Is(er, io.EOF) {
				// Normal termination
				return total, nil
			}
			return total, er
		}
	}
}

// CopyFile copies the file at src to dst, replacing dst if it exists.
//
// The copy is atomic: the content is written to a temporary file in the
// directory of dst, synced, and then renamed into place. So dst is never seen
// half-written, and if the copy fails or ctx is canceled dst is left as it was
// and nothing is left behind. Because dst is replaced and not written to, it
// is also fine for src and dst to be the same file, and a dst that is a
// symlink is replaced by the copy and not followed.
//
// The new file has mode 0644, whatever the mode of src and the umask.
func CopyFile(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".modctl-copy-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // no-op once the rename has succeeded
	}()

	buf := make([]byte, bufferSizeOf(in))
	if _, err := CopyWithContext(ctx, tmp, in, buf); err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	// the temporary file is created private; this is not affected by the umask
	if err := tmp.Chmod(copyFileMode); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}

	return nil
}
