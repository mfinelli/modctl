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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// crossDevice is the error os.Rename gives when the two paths are on
// different filesystems.
func crossDevice(oldpath, newpath string) error {
	return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// newPair returns a directory with a source file in it and the path of a
// destination in a different directory (what the blob store has: a temp
// directory and the final one).
func newPair(t *testing.T, content string) (src, dstDir, dst string) {
	t.Helper()

	srcDir := t.TempDir()
	src = filepath.Join(srcDir, "src")
	require.NoError(t, os.WriteFile(src, []byte(content), 0o600))

	dstDir = t.TempDir()
	return src, dstDir, filepath.Join(dstDir, "dst")
}

func TestReplaceFileWith(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("rename that works moves the file", func(t *testing.T) {
		t.Parallel()

		src, dstDir, dst := newPair(t, "blob")
		calls := 0
		rename := func(oldpath, newpath string) error {
			calls++
			return os.Rename(oldpath, newpath)
		}

		require.NoError(t, replaceFileWith(ctx, rename, src, dst))

		assert.Equal(t, 1, calls)
		assert.Equal(t, "blob", read(t, dst))
		assert.NoFileExists(t, src)
		assert.Equal(t, []string{"dst"}, names(t, dstDir))
	})

	t.Run("between filesystems it copies and removes the source", func(t *testing.T) {
		t.Parallel()

		src, dstDir, dst := newPair(t, "blob")

		require.NoError(t, replaceFileWith(ctx, crossDevice, src, dst))

		assert.Equal(t, "blob", read(t, dst))
		assert.NoFileExists(t, src)
		// the copy was done atomically, so nothing else is left in there
		assert.Equal(t, []string{"dst"}, names(t, dstDir))

		info, err := os.Stat(dst)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	})

	t.Run("between filesystems with the error wrapped", func(t *testing.T) {
		t.Parallel()

		src, _, dst := newPair(t, "blob")
		rename := func(oldpath, newpath string) error {
			return fmt.Errorf("moving blob: %w", crossDevice(oldpath, newpath))
		}

		require.NoError(t, replaceFileWith(ctx, rename, src, dst))

		assert.Equal(t, "blob", read(t, dst))
		assert.NoFileExists(t, src)
	})

	t.Run("between filesystems it replaces what is already there", func(t *testing.T) {
		t.Parallel()

		src, _, dst := newPair(t, "new")
		require.NoError(t, os.WriteFile(dst, []byte("a much longer old content"), 0o644))

		require.NoError(t, replaceFileWith(ctx, crossDevice, src, dst))

		assert.Equal(t, "new", read(t, dst))
		assert.NoFileExists(t, src)
	})

	t.Run("any other rename error is returned without copying", func(t *testing.T) {
		t.Parallel()

		src, dstDir, dst := newPair(t, "blob")
		boom := &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EACCES}
		rename := func(oldpath, newpath string) error { return boom }

		err := replaceFileWith(ctx, rename, src, dst)
		require.Error(t, err)
		assert.ErrorIs(t, err, syscall.EACCES)

		assert.Equal(t, "blob", read(t, src), "the source is untouched")
		assert.Empty(t, names(t, dstDir), "nothing was copied")
	})

	t.Run("a plain error that is not a link error is returned without copying", func(t *testing.T) {
		t.Parallel()

		src, dstDir, dst := newPair(t, "blob")
		boom := errors.New("boom")

		err := replaceFileWith(ctx, func(string, string) error { return boom }, src, dst)
		require.ErrorIs(t, err, boom)

		assert.FileExists(t, src)
		assert.Empty(t, names(t, dstDir))
	})

	t.Run("canceled context leaves the source and creates nothing", func(t *testing.T) {
		t.Parallel()

		src, dstDir, dst := newPair(t, "blob")
		canceled, cancel := context.WithCancel(ctx)
		cancel()

		err := replaceFileWith(canceled, crossDevice, src, dst)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)

		assert.Equal(t, "blob", read(t, src))
		assert.Empty(t, names(t, dstDir))
	})

	t.Run("source that does not exist", func(t *testing.T) {
		t.Parallel()

		_, dstDir, dst := newPair(t, "blob")

		err := replaceFileWith(ctx, crossDevice, filepath.Join(t.TempDir(), "missing"), dst)
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Empty(t, names(t, dstDir))
	})

	t.Run("failing to remove the source is an error but the copy is in place", func(t *testing.T) {
		t.Parallel()

		if os.Geteuid() == 0 {
			t.Skip("root can remove a file whatever the permissions of its directory")
		}

		src, _, dst := newPair(t, "blob")
		srcDir := filepath.Dir(src)
		require.NoError(t, os.Chmod(srcDir, 0o500))
		// so that the temporary directory can be cleaned up
		t.Cleanup(func() { _ = os.Chmod(srcDir, 0o700) })

		err := replaceFileWith(ctx, crossDevice, src, dst)
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrPermission)

		// the blob is where it should be, so IngestFile can carry on from
		// here, finding it when it looks
		assert.Equal(t, "blob", read(t, dst))
	})
}

func TestReplaceFile(t *testing.T) {
	t.Parallel()

	t.Run("moves a file within a filesystem", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		dst := filepath.Join(dir, "dst")
		require.NoError(t, os.WriteFile(src, []byte("blob"), 0o600))

		require.NoError(t, replaceFile(context.Background(), src, dst))

		assert.Equal(t, "blob", read(t, dst))
		assert.NoFileExists(t, src)
	})
}

func TestIsExdev(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"rename between filesystems", crossDevice("a", "b"), true},
		{"the same, wrapped", fmt.Errorf("wrapped: %w", crossDevice("a", "b")), true},
		{"a link error for something else", &os.LinkError{Op: "rename", Err: syscall.EACCES}, false},
		{"a link error that does not exist", &os.LinkError{Op: "rename", Err: syscall.ENOENT}, false},
		{"EXDEV without a link error around it", syscall.EXDEV, false},
		{"a plain error", errors.New("boom"), false},
		{"no error", nil, false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, isExdev(tc.err))
		})
	}
}
