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

package nexusclient

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRateLimitState(t *testing.T) {
	t.Parallel()

	t.Run("there are none until a response says what they are", func(t *testing.T) {
		t.Parallel()

		got, err := LoadRateLimitState(t.TempDir())

		require.NoError(t, err)
		assert.Equal(t, &RateLimitState{}, got)
	})

	t.Run("looking does not make anything", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "not", "there", "yet")

		_, err := LoadRateLimitState(dir)

		require.NoError(t, err)
		assert.NoDirExists(t, dir)
		assert.NoDirExists(t, filepath.Join(dir, ".."))
	})

	t.Run("what is saved is what is read", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		saved := &RateLimitState{
			HourlyLimit:     100,
			HourlyRemaining: 87,
			HourlyReset:     time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC),
			DailyLimit:      2500,
			DailyRemaining:  2400,
			DailyReset:      time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC),
		}
		require.NoError(t, SaveRateLimitState(dir, saved))

		got, err := LoadRateLimitState(dir)

		require.NoError(t, err)
		assert.Equal(t, 87, got.HourlyRemaining)
		assert.Equal(t, 2400, got.DailyRemaining)
		assert.True(t, saved.HourlyReset.Equal(got.HourlyReset))
		assert.True(t, saved.DailyReset.Equal(got.DailyReset))
	})

	t.Run("a file that is not json is no state, and not an error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "nexus.json"), []byte("{ not json"), 0o644))

		got, err := LoadRateLimitState(dir)

		require.NoError(t, err)
		assert.Equal(t, &RateLimitState{}, got)
	})

	t.Run("a file that can't be read is an error", func(t *testing.T) {
		t.Parallel()

		// a directory where the file should be is not something that can be read
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "nexus.json"), 0o755))

		got, err := LoadRateLimitState(dir)

		require.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("each directory has its own", func(t *testing.T) {
		t.Parallel()

		first, second := t.TempDir(), t.TempDir()
		require.NoError(t, SaveRateLimitState(first, &RateLimitState{HourlyLimit: 1}))
		require.NoError(t, SaveRateLimitState(second, &RateLimitState{HourlyLimit: 2}))

		a, err := LoadRateLimitState(first)
		require.NoError(t, err)
		b, err := LoadRateLimitState(second)
		require.NoError(t, err)

		assert.Equal(t, 1, a.HourlyLimit)
		assert.Equal(t, 2, b.HourlyLimit)
	})
}

func TestSaveRateLimitState(t *testing.T) {
	t.Parallel()

	t.Run("the directory is made if it is not there", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "state", "modctl")

		require.NoError(t, SaveRateLimitState(dir, &RateLimitState{HourlyLimit: 100}))

		assert.FileExists(t, filepath.Join(dir, "nexus.json"))
	})

	t.Run("it is saved over what was there", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, SaveRateLimitState(dir, &RateLimitState{HourlyRemaining: 50, DailyRemaining: 500}))

		require.NoError(t, SaveRateLimitState(dir, &RateLimitState{HourlyRemaining: 49}))

		got, err := LoadRateLimitState(dir)
		require.NoError(t, err)
		assert.Equal(t, 49, got.HourlyRemaining)
		assert.Equal(t, 0, got.DailyRemaining, "what is not in it any more is gone")
	})

	t.Run("a directory that can't be made is an error", func(t *testing.T) {
		t.Parallel()

		blocker := filepath.Join(t.TempDir(), "state")
		require.NoError(t, os.WriteFile(blocker, nil, 0o644))

		err := SaveRateLimitState(filepath.Join(blocker, "modctl"), &RateLimitState{})

		assert.Error(t, err)
	})
}

func TestRateLimitStateEffectiveRemaining(t *testing.T) {
	t.Parallel()

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	for _, tc := range []struct {
		name   string
		state  RateLimitState
		hourly int
		daily  int
	}{
		{
			"windows that are not over have what is remaining",
			RateLimitState{HourlyLimit: 100, HourlyRemaining: 7, HourlyReset: future, DailyLimit: 2500, DailyRemaining: 70, DailyReset: future},
			7, 70,
		},
		{
			"windows that are over have all of their limit",
			RateLimitState{HourlyLimit: 100, HourlyRemaining: 7, HourlyReset: past, DailyLimit: 2500, DailyRemaining: 70, DailyReset: past},
			100, 2500,
		},
		{
			"each window is its own",
			RateLimitState{HourlyLimit: 100, HourlyRemaining: 7, HourlyReset: past, DailyLimit: 2500, DailyRemaining: 70, DailyReset: future},
			100, 70,
		},
		{
			"a state that was never saved is out of both windows, with nothing in them",
			RateLimitState{},
			0, 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hourly, daily := tc.state.EffectiveRemaining()

			assert.Equal(t, tc.hourly, hourly)
			assert.Equal(t, tc.daily, daily)
		})
	}
}
