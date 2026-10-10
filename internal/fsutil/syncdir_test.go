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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncDir(t *testing.T) {
	t.Parallel()

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, SyncDir(t.TempDir()))
	})

	t.Run("directory with files in it", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))

		assert.NoError(t, SyncDir(dir))
	})

	t.Run("leaves the directory as it was", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644))

		require.NoError(t, SyncDir(dir))

		assert.Equal(t, []string{"file"}, dirNames(t, dir))
		assert.Equal(t, []byte("x"), mustRead(t, filepath.Join(dir, "file")))
	})

	t.Run("directory that does not exist", func(t *testing.T) {
		t.Parallel()

		err := SyncDir(filepath.Join(t.TempDir(), "gone"))
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("empty path", func(t *testing.T) {
		t.Parallel()

		assert.Error(t, SyncDir(""))
	})
}
