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

package blobstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Kind string

const (
	KindArchive  Kind = "archive"
	KindBackup   Kind = "backup"
	KindOverride Kind = "override"
)

type Store struct {
	ArchivesDir  string
	BackupsDir   string
	OverridesDir string
	TmpDir       string
}

func (s Store) RootFor(kind Kind) (string, error) {
	switch kind {
	case KindArchive:
		return s.ArchivesDir, nil
	case KindBackup:
		return s.BackupsDir, nil
	case KindOverride:
		return s.OverridesDir, nil
	default:
		return "", fmt.Errorf("unknown blob kind: %q", string(kind))
	}
}

// PathFor returns: <root>/ab/<fullhash>
func (s Store) PathFor(kind Kind, shaHex string) (string, error) {
	if len(shaHex) != 64 {
		return "", fmt.Errorf("invalid sha256 length: %d", len(shaHex))
	}
	root, err := s.RootFor(kind)
	if err != nil {
		return "", err
	}
	fan := shaHex[:2]
	return filepath.Join(root, fan, shaHex), nil
}

type IngestResult struct {
	SHA256Hex string
	SizeBytes int64
	Existed   bool
}

// IngestFile streams srcPath into the blob store, addressed by sha256.
// Writes a temp file in the destination directory and renames into place atomically.
func (s Store) IngestFile(ctx context.Context, kind Kind, srcPath string) (IngestResult, error) {
	var res IngestResult

	finalTmpKey := "" // helps error messages if we get far enough

	src, err := os.Open(srcPath)
	if err != nil {
		return res, fmt.Errorf("open src: %w", err)
	}
	defer src.Close()

	h := sha256.New()

	// We can’t derive the final path until we’ve hashed.
	// So we stream into a temp file in a stable "incoming" directory
	// under the tmp root
	incomingDir := filepath.Join(s.TmpDir, "incoming")
	if err := os.MkdirAll(incomingDir, 0o755); err != nil {
		return res, fmt.Errorf("mkdir incoming: %w", err)
	}

	tmp, err := os.CreateTemp(incomingDir, ".ingest-*")
	if err != nil {
		return res, fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // no-op if rename succeeded
	}()

	// Stream copy: write bytes to tmp while hashing.
	w := io.MultiWriter(tmp, h)

	buf := make([]byte, 1024*1024) // 1MiB buffer; fine for big archives
	n, err := copyWithContext(ctx, w, src, buf)
	if err != nil {
		return res, fmt.Errorf("copy: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		return res, fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return res, fmt.Errorf("close temp: %w", err)
	}

	sum := h.Sum(nil)
	shaHex := hex.EncodeToString(sum)

	finalPath, err := s.PathFor(kind, shaHex)
	if err != nil {
		return res, err
	}
	finalTmpKey = finalPath

	finalDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		return res, fmt.Errorf("mkdir final dir: %w", err)
	}

	// If blob already exists, dedupe.
	if st, statErr := os.Stat(finalPath); statErr == nil {
		// Sanity check: if the blob already exists, its on-disk size should match
		// what we just ingested. A mismatch indicates corruption or tampering.
		if st.Size() != n {
			return res, fmt.Errorf(
				"blob collision/corruption: %s exists with size=%d, ingest size=%d",
				finalPath, st.Size(), n,
			)
		}
		return IngestResult{SHA256Hex: shaHex, SizeBytes: n, Existed: true}, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return res, fmt.Errorf("stat final: %w", statErr)
	}

	// Move into place.
	if err := os.Rename(tmpName, finalPath); err != nil {
		// If we raced and it appeared, treat as dedupe.
		if st, statErr := os.Stat(finalPath); statErr == nil {
			if st.Size() != n {
				return res, fmt.Errorf(
					"blob collision/corruption after rename race: %s exists with size=%d, ingest size=%d",
					finalPath, st.Size(), n,
				)
			}
			return IngestResult{SHA256Hex: shaHex, SizeBytes: n, Existed: true}, nil
		}
		return res, fmt.Errorf("rename temp into place (%s): %w", finalTmpKey, err)
	}

	// Best-effort: fsync the directory so rename is durable.
	_ = fsyncDir(finalDir)

	return IngestResult{SHA256Hex: shaHex, SizeBytes: n, Existed: false}, nil
}

// copyWithContext copies bytes from src to dst using the provided buffer,
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
func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader, buf []byte) (int64, error) {
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

// fsyncDir calls fsync(2) on a directory to ensure that metadata changes
// within that directory are durably persisted to disk.
//
// Why this is needed:
// After renaming a blob into its final location (os.Rename), the file’s
// contents are durable (because we fsync’d the temp file), but the directory
// entry itself may still be sitting in the kernel’s metadata buffers.
// If the system crashes at that exact moment, the file could theoretically
// disappear after reboot even though the rename returned successfully.
//
// By opening the directory and calling Sync() on it, we force the directory
// metadata (including the new filename entry) to be flushed to stable storage.
//
// This is best-effort: some filesystems may ignore directory fsync or relax
// guarantees, but on modern Linux filesystems (ext4, xfs, btrfs) this provides
// proper crash-consistency for atomic rename patterns.
//
// It is intentionally non-fatal in callers because durability is strongly
// desired but not worth aborting the operation if unsupported.
func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
