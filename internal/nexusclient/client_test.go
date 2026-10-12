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
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mfinelli/modctl/internal/nexusclient/dbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheDBPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, filepath.Join("/some/cache", "nexus_cache.db"), CacheDBPath("/some/cache"))
}

func TestNewCacheReader(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("the cache is made in the directory that it is given", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		r, err := NewCacheReader(ctx, quietLogger(), dir)
		require.NoError(t, err)
		defer r.Close()

		assert.FileExists(t, CacheDBPath(dir))
		_, err = r.GetNexusModInfo("skyrim", 1)
		assert.ErrorIs(t, err, sql.ErrNoRows, "and it has nothing in it")
	})

	t.Run("each directory has its own cache", func(t *testing.T) {
		t.Parallel()

		first, err := NewCacheReader(ctx, quietLogger(), t.TempDir())
		require.NoError(t, err)
		defer first.Close()
		second, err := NewCacheReader(ctx, quietLogger(), t.TempDir())
		require.NoError(t, err)
		defer second.Close()

		require.NoError(t, dbc.New(first.db).UpsertNexusModInfo(ctx, dbc.UpsertNexusModInfoParams{
			NexusGameDomain: "skyrim", NexusModID: 1, FetchedAt: time.Now().UTC().Format(time.RFC3339),
		}))

		_, err = first.GetNexusModInfo("skyrim", 1)
		assert.NoError(t, err)
		_, err = second.GetNexusModInfo("skyrim", 1)
		assert.ErrorIs(t, err, sql.ErrNoRows)
	})

	t.Run("a directory that is not there is an error that says so", func(t *testing.T) {
		t.Parallel()

		r, err := NewCacheReader(ctx, quietLogger(), filepath.Join(t.TempDir(), "not-there"))

		require.Error(t, err)
		assert.Nil(t, r)
		assert.Contains(t, err.Error(), "opening nexus cache db")
	})

	t.Run("a cache that is of another version of the schema is started over", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		first, err := NewCacheReader(ctx, quietLogger(), dir)
		require.NoError(t, err)
		require.NoError(t, dbc.New(first.db).UpsertNexusModInfo(ctx, dbc.UpsertNexusModInfoParams{
			NexusGameDomain: "skyrim", NexusModID: 1, FetchedAt: time.Now().UTC().Format(time.RFC3339),
		}))
		_, err = first.db.Exec(`UPDATE cache_meta SET value = '0' WHERE key = 'schema_version'`)
		require.NoError(t, err)
		require.NoError(t, first.Close())

		second, err := NewCacheReader(ctx, quietLogger(), dir)
		require.NoError(t, err)
		defer second.Close()

		_, err = second.GetNexusModInfo("skyrim", 1)
		assert.ErrorIs(t, err, sql.ErrNoRows, "what was cached is not trusted")
		version, err := dbc.New(second.db).ReadSchemaVersion(ctx)
		require.NoError(t, err)
		assert.Equal(t, cacheSchemaVersion, version)
	})

	t.Run("a cache that is of this version is kept", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		first, err := NewCacheReader(ctx, quietLogger(), dir)
		require.NoError(t, err)
		require.NoError(t, dbc.New(first.db).UpsertNexusModInfo(ctx, dbc.UpsertNexusModInfoParams{
			NexusGameDomain: "skyrim", NexusModID: 1, FetchedAt: time.Now().UTC().Format(time.RFC3339),
		}))
		require.NoError(t, first.Close())

		second, err := NewCacheReader(ctx, quietLogger(), dir)
		require.NoError(t, err)
		defer second.Close()

		_, err = second.GetNexusModInfo("skyrim", 1)
		assert.NoError(t, err)
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("it is made with the directories that it is given", func(t *testing.T) {
		t.Parallel()

		cacheDir, stateDir := t.TempDir(), t.TempDir()

		c, err := New(context.Background(), "the-key", quietLogger(), "1.2.3", cacheDir, stateDir)
		require.NoError(t, err)
		defer c.Close()

		assert.FileExists(t, CacheDBPath(cacheDir))
		assert.Equal(t, stateDir, c.stateDir)
		assert.Equal(t, "the-key", c.apiKey)
	})

	t.Run("a cache directory that is not there is an error that says so", func(t *testing.T) {
		t.Parallel()

		c, err := New(context.Background(), "k", quietLogger(), "1", filepath.Join(t.TempDir(), "not-there"), t.TempDir())

		require.Error(t, err)
		assert.Nil(t, c)
		assert.Contains(t, err.Error(), "opening nexus cache db")
	})
}

func TestBuildUserAgent(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		"modctl/1.2.3 ("+runtime.GOOS+"; "+runtime.GOARCH+") https://github.com/mfinelli/modctl",
		buildUserAgent("1.2.3"))
}

func TestClientRateLimitState(t *testing.T) {
	t.Parallel()

	t.Run("it is the one of the state directory of the client", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, nil)
		require.NoError(t, SaveRateLimitState(c.stateDir, &RateLimitState{HourlyRemaining: 12}))
		other, _ := newTestClient(t, nil)

		got, err := c.RateLimitState()
		require.NoError(t, err)
		gotOther, err := other.RateLimitState()
		require.NoError(t, err)

		assert.Equal(t, 12, got.HourlyRemaining)
		assert.Equal(t, 0, gotOther.HourlyRemaining, "and not the one of another client")
	})
}

// the fake API is what a client is made to talk to by newTestClient: a client
// that is not given an answer has an error for every request
func TestNewTestClientHasNoNetwork(t *testing.T) {
	t.Parallel()

	c, api := newTestClient(t, nil)

	_, err := c.GetMod("skyrim", 1)

	require.Error(t, err)
	assert.Equal(t, 1, api.asked())
	assert.Equal(t, http.MethodGet, api.last().Method)
}
