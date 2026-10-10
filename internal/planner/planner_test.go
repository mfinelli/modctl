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

package planner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const helloSha = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func TestRecheckHash(t *testing.T) {
	t.Parallel()

	const destPath = "bin/mod.dll"

	writeHello := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "mod.dll")
		require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))
		return path
	}

	t.Run("file that can be hashed", func(t *testing.T) {
		t.Parallel()

		sha, warning, err := recheckHash(context.Background(), writeHello(t), destPath)
		require.NoError(t, err)
		assert.Equal(t, helloSha, sha)
		assert.Empty(t, warning)
	})

	t.Run("file that does not exist is only a warning", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "gone.dll")

		sha, warning, err := recheckHash(context.Background(), missing, destPath)
		require.NoError(t, err)
		assert.Empty(t, sha)
		assert.Contains(t, warning, "could not hash")
		assert.Contains(t, warning, destPath)
	})

	t.Run("file that can't be read is only a warning", func(t *testing.T) {
		t.Parallel()

		// a directory can be opened but not read, whoever is running the test
		sha, warning, err := recheckHash(context.Background(), t.TempDir(), destPath)
		require.NoError(t, err)
		assert.Empty(t, sha)
		assert.Contains(t, warning, "could not hash")
		assert.Contains(t, warning, destPath)
	})

	t.Run("canceled context is an error and not a warning", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		sha, warning, err := recheckHash(ctx, writeHello(t), destPath)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Contains(t, err.Error(), destPath)
		assert.Empty(t, sha)
		assert.Empty(t, warning)
	})

	t.Run("canceled context wins over a missing file", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, warning, err := recheckHash(ctx, filepath.Join(t.TempDir(), "gone.dll"), destPath)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, warning)
	})

	t.Run("expired deadline is an error too", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		_, warning, err := recheckHash(ctx, writeHello(t), destPath)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Empty(t, warning)
	})
}
