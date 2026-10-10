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
	"syscall"

	"github.com/mfinelli/modctl/internal/fsutil"
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
	n, err := fsutil.CopyWithContext(ctx, w, src, buf)
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
	if err := replaceFile(ctx, tmpName, finalPath); err != nil {
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
	_ = fsutil.SyncDir(finalDir)

	return IngestResult{SHA256Hex: shaHex, SizeBytes: n, Existed: false}, nil
}

// replaceFile atomically moves src to dst using rename. If src and dst are on
// different filesystems (EXDEV), it falls back to an atomic copy (so a partly
// copied blob never shows up under its final name) and then deletes src.
func replaceFile(ctx context.Context, src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !isExdev(err) {
		return err
	}
	// Cross-device fallback: copy then remove src.
	if err := fsutil.CopyFile(ctx, src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func isExdev(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return false
}
