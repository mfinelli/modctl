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
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	modBody = `{"mod_id":1,"name":"SkyUI","summary":"An interface","author":"snakster","domain_name":"skyrim","version":"5.2","available":true}`

	filesBody = `{"files":[{"file_id":10,"name":"SkyUI","version":"5.1","category_name":"MAIN","is_primary":true,"file_name":"SkyUI-5.1.7z","size_in_bytes":1000,"uploaded_timestamp":1700000000},{"file_id":11,"name":"SkyUI","version":"5.2","category_name":"MAIN","is_primary":false,"file_name":"SkyUI-5.2.7z","size_in_bytes":2000,"uploaded_timestamp":1700001000}],"file_updates":[{"old_file_id":10,"new_file_id":11,"old_file_name":"a","new_file_name":"b","uploaded_timestamp":1700001000}]}`
)

// ageCache makes what is cached older, by d, as if it had been fetched then.
func ageCache(t *testing.T, c *Client, d time.Duration) {
	t.Helper()

	then := time.Now().Add(-d).UTC().Format(time.RFC3339)
	_, err := c.db.Exec(`UPDATE nexus_mod_info SET fetched_at = ?`, then)
	require.NoError(t, err)
	_, err = c.db.Exec(`UPDATE nexus_file_info SET fetched_at = ?`, then)
	require.NoError(t, err)
}

func respondWith(body string) func(*http.Request) (*http.Response, error) {
	return func(*http.Request) (*http.Response, error) {
		return answer(http.StatusOK, body, nil), nil
	}
}

func TestGetModCached(t *testing.T) {
	t.Parallel()

	t.Run("what is not cached is asked of the API, and cached", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(modBody))

		first, err := c.GetModCached("skyrim", 1)
		require.NoError(t, err)
		second, err := c.GetModCached("skyrim", 1)
		require.NoError(t, err)

		assert.Equal(t, 1, api.asked(), "the second time it is in the cache")
		assert.Equal(t, "/v1/games/skyrim/mods/1.json", api.last().URL.Path)
		assert.Equal(t, "SkyUI", first.Name)
		assert.Equal(t, first.Name, second.Name)
		assert.Equal(t, first.Author, second.Author)
		assert.Equal(t, "An interface", second.Summary)
		assert.True(t, second.IsAvailable)
		assert.Equal(t, "skyrim", second.DomainName)
	})

	t.Run("what is in the cache is the mod that the API said", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(modBody))

		fromAPI, err := c.GetModCached("skyrim", 1)
		require.NoError(t, err)
		fromCache, err := c.GetModCached("skyrim", 1)
		require.NoError(t, err)

		require.Equal(t, 1, api.asked(), "the second time it is in the cache")
		assert.Equal(t, fromAPI, fromCache)
		assert.Equal(t, "5.2", fromCache.Version)
	})

	t.Run("a cache that has no version for the mod has none", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, respondWith(`{"mod_id":1,"name":"SkyUI","domain_name":"skyrim"}`))
		_, err := c.GetModCached("skyrim", 1)
		require.NoError(t, err)

		got, err := c.GetModCached("skyrim", 1)

		require.NoError(t, err)
		assert.Empty(t, got.Version)
		assert.Equal(t, "SkyUI", got.Name)
	})

	t.Run("a cache that has nothing that can be read says nothing about the version", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, respondWith(modBody))
		_, err := c.GetModCached("skyrim", 1)
		require.NoError(t, err)
		_, err = c.db.Exec(`UPDATE nexus_mod_info SET raw_json = NULL`)
		require.NoError(t, err)

		got, err := c.GetModCached("skyrim", 1)

		require.NoError(t, err)
		assert.Empty(t, got.Version)
		assert.Equal(t, "SkyUI", got.Name, "and the rest is what the columns have")
	})

	t.Run("what is cached for less than a week is not asked of the API", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(modBody))
		_, err := c.GetModCached("skyrim", 1)
		require.NoError(t, err)
		ageCache(t, c, ModInfoTTL-time.Hour)

		_, err = c.GetModCached("skyrim", 1)

		require.NoError(t, err)
		assert.Equal(t, 1, api.asked())
	})

	t.Run("what has been cached for more than a week is asked again", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(modBody))
		_, err := c.GetModCached("skyrim", 1)
		require.NoError(t, err)
		ageCache(t, c, ModInfoTTL+time.Hour)

		_, err = c.GetModCached("skyrim", 1)

		require.NoError(t, err)
		assert.Equal(t, 2, api.asked())
	})

	t.Run("a time that can't be read is as old as it can be", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(modBody))
		_, err := c.GetModCached("skyrim", 1)
		require.NoError(t, err)
		_, err = c.db.Exec(`UPDATE nexus_mod_info SET fetched_at = 'last tuesday'`)
		require.NoError(t, err)

		_, err = c.GetModCached("skyrim", 1)

		require.NoError(t, err)
		assert.Equal(t, 2, api.asked())
	})

	t.Run("a mod is cached for the game that it is of", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(modBody))
		_, err := c.GetModCached("skyrim", 1)
		require.NoError(t, err)

		_, err = c.GetModCached("fallout4", 1)

		require.NoError(t, err)
		assert.Equal(t, 2, api.asked(), "the same id of another game is another mod")
	})

	t.Run("the error of the API is the error, and nothing is cached", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, func(*http.Request) (*http.Response, error) {
			return answer(http.StatusNotFound, "", nil), nil
		})

		_, err := c.GetModCached("skyrim", 1)
		require.Error(t, err)
		_, err = c.GetModCached("skyrim", 1)
		require.Error(t, err)

		assert.Equal(t, 2, api.asked())
		assert.Contains(t, err.Error(), "fetching mod 1 (skyrim)")
	})
}

func TestGetModFilesCached(t *testing.T) {
	t.Parallel()

	t.Run("what is not cached is asked of the API, and cached with its updates", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(filesBody))

		first, err := c.GetModFilesCached("skyrim", 1)
		require.NoError(t, err)
		second, err := c.GetModFilesCached("skyrim", 1)
		require.NoError(t, err)

		assert.Equal(t, 1, api.asked(), "the second time it is in the cache")
		assert.Equal(t, "/v1/games/skyrim/mods/1/files.json", api.last().URL.Path)
		require.Len(t, first.Files, 2)
		require.Len(t, second.Files, 2)
		got := map[int64]ModFileInfo{}
		for _, f := range second.Files {
			got[f.FileID] = f
		}
		assert.Equal(t, ModFileInfo{
			FileID: 10, Name: "SkyUI", Version: "5.1", CategoryName: "MAIN", IsPrimary: true,
			FileName: "SkyUI-5.1.7z", SizeInBytes: 1000, UploadedTimestamp: 1700000000,
		}, got[10])
		assert.False(t, got[11].IsPrimary)
		require.Len(t, second.FileUpdates, 1)
		assert.Equal(t, int64(10), second.FileUpdates[0].OldFileID)
		assert.Equal(t, int64(11), second.FileUpdates[0].NewFileID)
	})

	t.Run("what has been cached for more than a day is asked again", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(filesBody))
		_, err := c.GetModFilesCached("skyrim", 1)
		require.NoError(t, err)
		ageCache(t, c, ModFilesTTL+time.Hour)

		_, err = c.GetModFilesCached("skyrim", 1)

		require.NoError(t, err)
		assert.Equal(t, 2, api.asked())
	})

	t.Run("what is cached for less than a day is not", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(filesBody))
		_, err := c.GetModFilesCached("skyrim", 1)
		require.NoError(t, err)
		ageCache(t, c, ModFilesTTL-time.Hour)

		_, err = c.GetModFilesCached("skyrim", 1)

		require.NoError(t, err)
		assert.Equal(t, 1, api.asked())
	})

	t.Run("the files that were cached are replaced by the ones that there are now", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, nil)
		require.NoError(t, c.cacheModFiles("skyrim", 1, &ModFilesResponse{
			Files:       []ModFileInfo{{FileID: 10, Name: "old"}, {FileID: 11, Name: "old too"}},
			FileUpdates: []FileUpdateInfo{{OldFileID: 10, NewFileID: 11, UploadedTimestamp: 1}},
		}))
		require.NoError(t, c.cacheModFiles("skyrim", 2, &ModFilesResponse{
			Files: []ModFileInfo{{FileID: 20, Name: "of another mod"}},
		}))

		require.NoError(t, c.cacheModFiles("skyrim", 1, &ModFilesResponse{
			Files: []ModFileInfo{{FileID: 12, Name: "new"}},
		}))

		got, err := c.GetModFilesCached("skyrim", 1)
		require.NoError(t, err)
		require.Len(t, got.Files, 1)
		assert.Equal(t, int64(12), got.Files[0].FileID)
		assert.Empty(t, got.FileUpdates, "and its updates")
		other, err := c.GetModFilesCached("skyrim", 2)
		require.NoError(t, err)
		require.Len(t, other.Files, 1, "the files of the other mods are kept")
		assert.Equal(t, int64(20), other.Files[0].FileID)
	})
}

func TestCacheReaderGetUpdateChain(t *testing.T) {
	t.Parallel()

	t.Run("a mod that has nothing cached has a chain in which every file is the latest", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, nil)

		chain, err := c.GetUpdateChain("skyrim", 1)

		require.NoError(t, err)
		assert.False(t, chain.HasSuccessor(10))
		assert.Equal(t, int64(10), chain.Head(10))
	})

	t.Run("the updates that are cached are the chain", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, nil)
		require.NoError(t, c.cacheModFiles("skyrim", 1, &ModFilesResponse{
			Files: []ModFileInfo{{FileID: 10}, {FileID: 11}, {FileID: 12}},
			FileUpdates: []FileUpdateInfo{
				{OldFileID: 10, NewFileID: 11, UploadedTimestamp: 1},
				{OldFileID: 11, NewFileID: 12, UploadedTimestamp: 2},
			},
		}))

		chain, err := c.GetUpdateChain("skyrim", 1)

		require.NoError(t, err)
		assert.True(t, chain.HasSuccessor(10))
		assert.Equal(t, int64(12), chain.Head(10))
		assert.False(t, chain.HasSuccessor(12))
	})

	t.Run("the chain of a mod is not the chain of another", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, nil)
		require.NoError(t, c.cacheModFiles("skyrim", 1, &ModFilesResponse{
			FileUpdates: []FileUpdateInfo{{OldFileID: 10, NewFileID: 11, UploadedTimestamp: 1}},
		}))

		chain, err := c.GetUpdateChain("skyrim", 2)

		require.NoError(t, err)
		assert.False(t, chain.HasSuccessor(10))
	})
}
