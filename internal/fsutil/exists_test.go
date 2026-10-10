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
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExists(t *testing.T) {
	t.Parallel()

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

		got, err := Exists(path)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		got, err := Exists(t.TempDir())
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("empty file counts", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "empty")
		require.NoError(t, os.WriteFile(path, nil, 0o644))

		got, err := Exists(path)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("path that is not there", func(t *testing.T) {
		t.Parallel()

		got, err := Exists(filepath.Join(t.TempDir(), "gone"))
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("parent directory that is not there", func(t *testing.T) {
		t.Parallel()

		got, err := Exists(filepath.Join(t.TempDir(), "no", "such", "dir", "file"))
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("symlink to something that exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		link := filepath.Join(dir, "link")
		require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
		require.NoError(t, os.Symlink(target, link))

		got, err := Exists(link)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("symlink whose target is missing does not exist", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), link))

		got, err := Exists(link)
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("path below a file is an error, not a no", func(t *testing.T) {
		t.Parallel()

		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

		got, err := Exists(filepath.Join(file, "child"))
		require.Error(t, err)
		assert.NotErrorIs(t, err, fs.ErrNotExist)
		assert.False(t, got)
	})

	t.Run("directory that can't be searched is an error, not a no", func(t *testing.T) {
		t.Parallel()

		if os.Geteuid() == 0 {
			t.Skip("root can search a directory whatever its permissions")
		}

		dir := t.TempDir()
		locked := filepath.Join(dir, "locked")
		require.NoError(t, os.Mkdir(locked, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(locked, "file"), []byte("x"), 0o644))
		require.NoError(t, os.Chmod(locked, 0o000))
		// so that the temporary directory can be cleaned up
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

		got, err := Exists(filepath.Join(locked, "file"))
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrPermission)
		assert.False(t, got)
	})
}
