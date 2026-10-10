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

func TestHashFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			"empty file",
			"",
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			"short content",
			"hello",
			"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			"content with a trailing newline",
			"hello\n",
			"5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "file")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o644))

			got, err := HashFile(path)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("file larger than the copy buffer", func(t *testing.T) {
		t.Parallel()

		// The same content must hash identically no matter how io.Copy
		// chunks it, so compare against a second file with the same bytes.
		data := make([]byte, 3*1024*1024+17)
		for i := range data {
			data[i] = byte(i % 251)
		}

		dir := t.TempDir()
		a := filepath.Join(dir, "a")
		b := filepath.Join(dir, "b")
		require.NoError(t, os.WriteFile(a, data, 0o644))
		require.NoError(t, os.WriteFile(b, data, 0o644))

		gotA, err := HashFile(a)
		require.NoError(t, err)
		gotB, err := HashFile(b)
		require.NoError(t, err)

		assert.Equal(t, gotA, gotB)
		assert.Len(t, gotA, 64)
	})

	t.Run("different content gives a different hash", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		a := filepath.Join(dir, "a")
		b := filepath.Join(dir, "b")
		require.NoError(t, os.WriteFile(a, []byte("one"), 0o644))
		require.NoError(t, os.WriteFile(b, []byte("two"), 0o644))

		gotA, err := HashFile(a)
		require.NoError(t, err)
		gotB, err := HashFile(b)
		require.NoError(t, err)

		assert.NotEqual(t, gotA, gotB)
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()

		got, err := HashFile(filepath.Join(t.TempDir(), "does-not-exist"))
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "open file for hashing")
		assert.Empty(t, got)
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		got, err := HashFile(t.TempDir())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "hash file contents")
		assert.Empty(t, got)
	})
}
