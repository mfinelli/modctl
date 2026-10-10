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
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sumHex is the sha256 of data computed independently of HashFile.
func sumHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// patterned returns n bytes that don't repeat on a buffer boundary.
func patterned(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i % 251)
	}
	return data
}

func TestHashFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{
			"empty file",
			[]byte(""),
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			"short content",
			[]byte("hello"),
			"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			"content with a trailing newline",
			[]byte("hello\n"),
			"5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03",
		},
		{
			"exactly the smallest buffer",
			patterned(minBufferSize),
			sumHex(patterned(minBufferSize)),
		},
		{
			"one byte more than the smallest buffer",
			patterned(minBufferSize + 1),
			sumHex(patterned(minBufferSize + 1)),
		},
		{
			"exactly the largest buffer",
			patterned(maxBufferSize),
			sumHex(patterned(maxBufferSize)),
		},
		{
			"larger than the largest buffer",
			patterned(3*maxBufferSize + 17),
			sumHex(patterned(3*maxBufferSize + 17)),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "file")
			require.NoError(t, os.WriteFile(path, tc.content, 0o644))

			got, err := HashFile(context.Background(), path)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("canceled context", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		got, err := HashFile(ctx, path)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Contains(t, err.Error(), "hash file contents")
		assert.Empty(t, got)
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()

		got, err := HashFile(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"))
		require.Error(t, err)
		// callers rely on being able to tell a missing file apart
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "open file for hashing")
		assert.Empty(t, got)
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		got, err := HashFile(context.Background(), t.TempDir())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "hash file contents")
		assert.Empty(t, got)
	})

	t.Run("several goroutines at once", func(t *testing.T) {
		t.Parallel()

		// different sizes, so the goroutines use different buffer sizes too
		dir := t.TempDir()
		sizes := []int{0, 10, minBufferSize, minBufferSize + 1, 300 * 1024, 2*maxBufferSize + 3}
		paths := make([]string, len(sizes))
		want := make([]string, len(sizes))
		for i, size := range sizes {
			data := patterned(size)
			paths[i] = filepath.Join(dir, "f"+strconv.Itoa(i))
			want[i] = sumHex(data)
			require.NoError(t, os.WriteFile(paths[i], data, 0o644))
		}

		const rounds = 5
		got := make([][]string, len(sizes))
		errs := make([][]error, len(sizes))

		var (
			wg sync.WaitGroup
			mu sync.Mutex
		)
		for i := range paths {
			for r := 0; r < rounds; r++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					sum, err := HashFile(context.Background(), paths[i])
					mu.Lock()
					got[i] = append(got[i], sum)
					errs[i] = append(errs[i], err)
					mu.Unlock()
				}(i)
			}
		}
		wg.Wait()

		for i := range paths {
			require.Len(t, got[i], rounds)
			for r := 0; r < rounds; r++ {
				require.NoError(t, errs[i][r])
				assert.Equal(t, want[i], got[i][r], "file %d", i)
			}
		}
	})
}
