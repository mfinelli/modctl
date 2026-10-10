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

func TestSymlinkTarget(t *testing.T) {
	t.Parallel()

	t.Run("regular file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

		target, isLink, err := SymlinkTarget(path)
		require.NoError(t, err)
		assert.False(t, isLink)
		assert.Empty(t, target)
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		_, isLink, err := SymlinkTarget(t.TempDir())
		require.NoError(t, err)
		assert.False(t, isLink)
	})

	t.Run("path that is not there", func(t *testing.T) {
		t.Parallel()

		_, isLink, err := SymlinkTarget(filepath.Join(t.TempDir(), "gone"))
		require.NoError(t, err)
		assert.False(t, isLink)
	})

	t.Run("symlink to a file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		real := filepath.Join(dir, "real")
		link := filepath.Join(dir, "link")
		require.NoError(t, os.WriteFile(real, []byte("x"), 0o644))
		require.NoError(t, os.Symlink(real, link))

		target, isLink, err := SymlinkTarget(link)
		require.NoError(t, err)
		assert.True(t, isLink)
		assert.Equal(t, real, target)
	})

	t.Run("symlink to a directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(dir, link))

		target, isLink, err := SymlinkTarget(link)
		require.NoError(t, err)
		assert.True(t, isLink)
		assert.Equal(t, dir, target)
	})

	t.Run("symlink to something that is not there", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), link))

		target, isLink, err := SymlinkTarget(link)
		require.NoError(t, err)
		assert.True(t, isLink, "it is a symlink, even if it doesn't lead anywhere")
		assert.Equal(t, filepath.Join(dir, "gone"), target)
	})

	t.Run("a relative target is given as it was written", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink("../elsewhere/real.cfg", link))

		target, isLink, err := SymlinkTarget(link)
		require.NoError(t, err)
		assert.True(t, isLink)
		assert.Equal(t, "../elsewhere/real.cfg", target)
	})

	t.Run("a chain of symlinks gives the first step of it", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		real := filepath.Join(dir, "real")
		second := filepath.Join(dir, "second")
		first := filepath.Join(dir, "first")
		require.NoError(t, os.WriteFile(real, []byte("x"), 0o644))
		require.NoError(t, os.Symlink(real, second))
		require.NoError(t, os.Symlink(second, first))

		target, isLink, err := SymlinkTarget(first)
		require.NoError(t, err)
		assert.True(t, isLink)
		assert.Equal(t, second, target)
	})

	t.Run("a file in a directory that is a symlink is not one itself", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		realDir := filepath.Join(dir, "real-dir")
		require.NoError(t, os.Mkdir(realDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(realDir, "file"), []byte("x"), 0o644))
		linkDir := filepath.Join(dir, "link-dir")
		require.NoError(t, os.Symlink(realDir, linkDir))

		_, isLink, err := SymlinkTarget(filepath.Join(linkDir, "file"))
		require.NoError(t, err)
		assert.False(t, isLink)
	})

	t.Run("a path below a file is an error, not a no", func(t *testing.T) {
		t.Parallel()

		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

		_, isLink, err := SymlinkTarget(filepath.Join(file, "child"))
		require.Error(t, err)
		assert.NotErrorIs(t, err, fs.ErrNotExist)
		assert.False(t, isLink)
	})
}
