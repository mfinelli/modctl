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

package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadActive(t *testing.T) {
	t.Parallel()

	t.Run("there is none until one is saved", func(t *testing.T) {
		t.Parallel()

		got, err := LoadActive(t.TempDir())

		require.NoError(t, err)
		assert.Equal(t, Active{}, got)
	})

	t.Run("looking does not make anything", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "not", "there", "yet")

		got, err := LoadActive(dir)

		require.NoError(t, err)
		assert.Equal(t, Active{}, got)
		assert.NoDirExists(t, filepath.Join(dir, ".."), "not the directory, or any of the ones that lead to it")
		assert.NoDirExists(t, dir)
	})

	t.Run("what is saved is what is read", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		saved := Active{
			ActiveStoreID:             "steam",
			ActiveGameInstallID:       7,
			ActiveGameInstallSelector: "steam:123#default",
		}
		require.NoError(t, SaveActive(dir, saved))

		got, err := LoadActive(dir)

		require.NoError(t, err)
		saved.UpdatedAt = got.UpdatedAt // which is when it was saved
		assert.Equal(t, saved, got)
		assert.NotEmpty(t, got.UpdatedAt)
	})

	t.Run("a file that is not json is an error that says which", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "active.json")
		require.NoError(t, os.WriteFile(path, []byte("{ not json"), 0o644))

		_, err := LoadActive(dir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse "+path)
	})

	t.Run("a file that can't be read is an error that says which", func(t *testing.T) {
		t.Parallel()

		// a directory where the file should be is not something that can be read
		dir := t.TempDir()
		path := filepath.Join(dir, "active.json")
		require.NoError(t, os.Mkdir(path, 0o755))

		_, err := LoadActive(dir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read "+path)
	})

	t.Run("each directory has its own selection", func(t *testing.T) {
		t.Parallel()

		first, second := t.TempDir(), t.TempDir()
		require.NoError(t, SaveActive(first, Active{ActiveGameInstallID: 1}))
		require.NoError(t, SaveActive(second, Active{ActiveGameInstallID: 2}))

		a, err := LoadActive(first)
		require.NoError(t, err)
		b, err := LoadActive(second)
		require.NoError(t, err)

		assert.Equal(t, int64(1), a.ActiveGameInstallID)
		assert.Equal(t, int64(2), b.ActiveGameInstallID)
	})
}

func TestSaveActive(t *testing.T) {
	t.Parallel()

	t.Run("the directory is made if it is not there", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "state", "modctl")

		require.NoError(t, SaveActive(dir, Active{ActiveGameInstallID: 3}))

		assert.FileExists(t, filepath.Join(dir, "active.json"))
	})

	t.Run("it is saved over what was there", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, SaveActive(dir, Active{ActiveGameInstallID: 1, ActiveStoreID: "steam"}))

		require.NoError(t, SaveActive(dir, Active{ActiveGameInstallID: 2}))

		got, err := LoadActive(dir)
		require.NoError(t, err)
		assert.Equal(t, int64(2), got.ActiveGameInstallID)
		assert.Empty(t, got.ActiveStoreID, "what is not in it any more is gone")
	})

	t.Run("it is saved with when it was, whatever it is given", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		before := time.Now().UTC().Add(-time.Second)

		require.NoError(t, SaveActive(dir, Active{UpdatedAt: "1999-01-01T00:00:00.000Z"}))

		got, err := LoadActive(dir)
		require.NoError(t, err)
		at, err := time.Parse("2006-01-02T15:04:05.000Z", got.UpdatedAt)
		require.NoError(t, err)
		assert.True(t, at.After(before), "it is now and not 1999: %s", got.UpdatedAt)
	})

	t.Run("what is written is readable, and ends with a newline", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, SaveActive(dir, Active{ActiveGameInstallID: 7}))

		b, err := os.ReadFile(filepath.Join(dir, "active.json"))

		require.NoError(t, err)
		assert.Contains(t, string(b), "\n  \"active_game_install_id\": 7")
		assert.NotContains(t, string(b), "active_store_id", "what is empty is left out")
		assert.Equal(t, byte('\n'), b[len(b)-1])
	})

	t.Run("nothing but the file is left in the directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		require.NoError(t, SaveActive(dir, Active{ActiveGameInstallID: 7}))
		require.NoError(t, SaveActive(dir, Active{ActiveGameInstallID: 8}))

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, entries, 1, "not the file that it is written to first")
		assert.Equal(t, "active.json", entries[0].Name())
	})

	t.Run("a directory that can't be made is an error that says which", func(t *testing.T) {
		t.Parallel()

		// there is a file where the directory should be
		parent := t.TempDir()
		blocker := filepath.Join(parent, "state")
		require.NoError(t, os.WriteFile(blocker, nil, 0o644))
		dir := filepath.Join(blocker, "modctl")

		err := SaveActive(dir, Active{ActiveGameInstallID: 1})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "make "+dir)
	})
}
