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

func TestCheckDrift(t *testing.T) {
	t.Parallel()

	writeHello := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "mod.dll")
		require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))
		return path
	}

	t.Run("file that matches what was installed", func(t *testing.T) {
		t.Parallel()

		got, err := CheckDrift(context.Background(), writeHello(t), helloSha)
		require.NoError(t, err)
		assert.Equal(t, DriftResult{State: DriftNone}, got)
	})

	t.Run("file that differs from what was installed", func(t *testing.T) {
		t.Parallel()

		for _, installed := range []string{"something else", ""} {
			got, err := CheckDrift(context.Background(), writeHello(t), installed)
			require.NoError(t, err)
			assert.Equal(t, DriftResult{State: DriftModified}, got, "installed %q", installed)
		}
	})

	t.Run("file that can't be read is unknown, not an error", func(t *testing.T) {
		t.Parallel()

		// a directory can be opened but not read, whoever is running the test
		got, err := CheckDrift(context.Background(), t.TempDir(), helloSha)
		require.NoError(t, err)
		assert.Equal(t, DriftUnknown, got.State)
		assert.Error(t, got.Err)
	})

	t.Run("file that does not exist is unknown, not an error", func(t *testing.T) {
		t.Parallel()

		got, err := CheckDrift(context.Background(), filepath.Join(t.TempDir(), "gone.dll"), helloSha)
		require.NoError(t, err)
		assert.Equal(t, DriftUnknown, got.State)
		assert.ErrorIs(t, got.Err, os.ErrNotExist)
	})

	t.Run("canceled context is an error whether or not the file matches", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		for _, installed := range []string{helloSha, "something else"} {
			got, err := CheckDrift(ctx, writeHello(t), installed)
			require.Error(t, err)
			assert.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, DriftResult{}, got)
		}
	})

	t.Run("canceled context wins over a file that does not exist", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := CheckDrift(ctx, filepath.Join(t.TempDir(), "gone.dll"), helloSha)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("expired deadline is an error too", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		_, err := CheckDrift(ctx, writeHello(t), helloSha)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}
