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

package extractor

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const helloSha = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func TestRemoveFromDisk(t *testing.T) {
	t.Parallel()

	const destPath = "bin/mod.dll"

	t.Run("removes the file and reports its hash and size", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "mod.dll")
		require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

		sha, size, err := removeFromDisk(context.Background(), path, destPath)
		require.NoError(t, err)
		assert.Equal(t, sql.NullString{String: helloSha, Valid: true}, sha)
		assert.Equal(t, sql.NullInt64{Int64: 5, Valid: true}, size)
		assert.NoFileExists(t, path)
	})

	t.Run("a file that is already gone is not an error", func(t *testing.T) {
		t.Parallel()

		sha, size, err := removeFromDisk(context.Background(),
			filepath.Join(t.TempDir(), "gone.dll"), destPath)
		require.NoError(t, err)
		assert.False(t, sha.Valid)
		assert.False(t, size.Valid)
	})

	t.Run("canceled context leaves the file where it is", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "mod.dll")
		require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		sha, size, err := removeFromDisk(ctx, path, destPath)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Contains(t, err.Error(), destPath)
		assert.False(t, sha.Valid)
		assert.False(t, size.Valid)
		assert.FileExists(t, path)
	})

	t.Run("a file that can't be hashed is still removed", func(t *testing.T) {
		t.Parallel()

		if os.Geteuid() == 0 {
			t.Skip("root can read a file whatever its permissions")
		}

		path := filepath.Join(t.TempDir(), "mod.dll")
		require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))
		require.NoError(t, os.Chmod(path, 0o000))

		sha, size, err := removeFromDisk(context.Background(), path, destPath)
		require.NoError(t, err)
		assert.False(t, sha.Valid)
		assert.False(t, size.Valid)
		assert.NoFileExists(t, path)
	})

	t.Run("failing to remove is an error", func(t *testing.T) {
		t.Parallel()

		// can't be hashed (it is a directory) and can't be removed (it is
		// not empty)
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "inside"), []byte("x"), 0o644))

		_, _, err := removeFromDisk(context.Background(), dir, destPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remove")
		assert.Contains(t, err.Error(), destPath)
		assert.DirExists(t, dir)
	})
}
