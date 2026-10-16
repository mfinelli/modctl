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

package internal

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCandidateSteamRoots(t *testing.T) {
	t.Parallel()

	t.Run("they are in the directories of the user, the most preferred first", func(t *testing.T) {
		t.Parallel()

		got := candidateSteamRoots(UserDirs{Home: "/home/me", DataHome: "/data"})

		assert.Equal(t, []string{
			"/data/Steam",
			"/home/me/.local/share/Steam",
			"/home/me/.steam/steam",
			"/home/me/.var/app/com.valvesoftware.Steam/data/Steam",
		}, got)
	})

	t.Run("a data home that is not known is not a place", func(t *testing.T) {
		t.Parallel()

		got := candidateSteamRoots(UserDirs{Home: "/home/me"})

		assert.Equal(t, []string{
			"/home/me/.local/share/Steam",
			"/home/me/.steam/steam",
			"/home/me/.var/app/com.valvesoftware.Steam/data/Steam",
		}, got)
	})

	t.Run("a user with no home has only the data home, and not what is in the directory that it is run in", func(t *testing.T) {
		t.Parallel()

		got := candidateSteamRoots(UserDirs{DataHome: "/data"})

		assert.Equal(t, []string{"/data/Steam"}, got)
	})

	t.Run("a user that has nothing has no place to look in", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, candidateSteamRoots(UserDirs{}))
	})

	t.Run("the directories are cleaned", func(t *testing.T) {
		t.Parallel()

		got := candidateSteamRoots(UserDirs{DataHome: "/data/"})

		assert.Equal(t, []string{"/data/Steam"}, got)
	})
}

func TestExpandHome(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		path string
		home string
		want string
	}{
		{"nothing is nothing", "", "/home/me", ""},
		{"an absolute path is as it is", "/games/steam", "/home/me", "/games/steam"},
		{"a relative path is as it is", "games/steam", "/home/me", "games/steam"},
		{"a tilde is the home", "~", "/home/me", "/home/me"},
		{"a tilde and a path is a path in the home", "~/Games/Steam", "/home/me", "/home/me/Games/Steam"},
		{"the home of somebody else is not this one", "~other/Games", "/home/me", "~other/Games"},
		{"a tilde that is not at the start is a character", "games/~/steam", "/home/me", "games/~/steam"},
		{"a home that is not known leaves it as it is", "~/Games", "", "~/Games"},
		{"and so does a tilde alone", "~", "", "~"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, expandHome(tc.path, tc.home))
		})
	}
}

// canon is p as discovery has it, which is without the symlinks (the
// directories of a test can be in one).
func canon(t *testing.T, p string) string {
	t.Helper()

	c, err := canonicalizePathBestEffort(p)
	require.NoError(t, err)
	return c
}

func TestDiscoverSteamLibrariesInHome(t *testing.T) {
	t.Parallel()

	t.Run("a place that starts with a tilde is in the home that it is given", func(t *testing.T) {
		t.Parallel()

		home := t.TempDir()
		root := filepath.Join(home, "Games", "Steam")
		writeSteamRoot(t, root, root)

		got := discoverSteamLibrariesIn([]string{"~/Games/Steam"}, home)

		assert.Equal(t, []string{canon(t, root)}, got.Roots)
		assert.True(t, got.DidScan)
	})

	t.Run("a library that starts with a tilde is in the home that it is given", func(t *testing.T) {
		t.Parallel()

		home := t.TempDir()
		root := filepath.Join(t.TempDir(), "Steam")
		library := filepath.Join(home, "Games", "SteamLibrary")
		writeSteamRoot(t, root, root, "~/Games/SteamLibrary")

		got := discoverSteamLibrariesIn([]string{root}, home)

		assert.ElementsMatch(t, []string{canon(t, root), canon(t, library)}, got.Libs)
	})
}

func TestScanStores(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// user is a user with a home, whose data home is where Steam is by default
	type user struct {
		dirs     UserDirs
		dataHome string
	}
	newUser := func(t *testing.T) user {
		t.Helper()

		home := t.TempDir()
		data := filepath.Join(home, ".local", "share")
		return user{dirs: UserDirs{Home: home, DataHome: data}, dataHome: data}
	}

	// installSteam makes a Steam installation at root, that is a library
	// itself, with a game in it.
	installSteam := func(t *testing.T, root, appid, name string) {
		t.Helper()

		writeSteamRoot(t, root, root)
		writeSteamGame(t, root, appid, name)
	}

	t.Run("the games of the Steam of the user are found", func(t *testing.T) {
		t.Parallel()

		u := newUser(t)
		root := filepath.Join(u.dataHome, "Steam")
		installSteam(t, root, "72850", "Skyrim")
		db := testbuilder.SetupDB(t)

		result, err := ScanStores(ctx, db, u.dirs)

		require.NoError(t, err)
		assert.Equal(t, []string{"Skyrim"}, result.New)
		state := readSteamState(t, dbq.New(db))
		require.Contains(t, state, "72850")
		assert.Equal(t, filepath.Join(canon(t, root), "steamapps", "common", "Skyrim"), state["72850"].InstallRoot)
	})

	t.Run("a Steam that is only in a place under the home is found, too", func(t *testing.T) {
		t.Parallel()

		for name, rel := range map[string]string{
			"the old one":        filepath.Join(".steam", "steam"),
			"the one of Flatpak": filepath.Join(".var", "app", "com.valvesoftware.Steam", "data", "Steam"),
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				u := newUser(t)
				installSteam(t, filepath.Join(u.dirs.Home, rel), "377160", "Fallout 4")
				db := testbuilder.SetupDB(t)

				result, err := ScanStores(ctx, db, u.dirs)

				require.NoError(t, err)
				assert.Equal(t, []string{"Fallout 4"}, result.New)
			})
		}
	})

	t.Run("the Steam of another user is not looked at", func(t *testing.T) {
		t.Parallel()

		other := newUser(t)
		installSteam(t, filepath.Join(other.dataHome, "Steam"), "72850", "Skyrim")
		mine := newUser(t)
		db := testbuilder.SetupDB(t)

		result, err := ScanStores(ctx, db, mine.dirs)

		require.NoError(t, err)
		assert.Empty(t, result.New)
		assert.Empty(t, readSteamState(t, dbq.New(db)))
	})

	t.Run("a user that has no directories has no games, and it is not an error", func(t *testing.T) {
		t.Parallel()

		db := testbuilder.SetupDB(t)

		result, err := ScanStores(ctx, db, UserDirs{})

		require.NoError(t, err)
		assert.Empty(t, result.New)
		assert.Empty(t, readSteamState(t, dbq.New(db)))
	})

	t.Run("a library in the home that is told with a tilde is scanned", func(t *testing.T) {
		t.Parallel()

		u := newUser(t)
		root := filepath.Join(u.dataHome, "Steam")
		writeSteamRoot(t, root, root, "~/Games/SteamLibrary")
		writeSteamGame(t, filepath.Join(u.dirs.Home, "Games", "SteamLibrary"), "1091500", "Cyberpunk 2077")
		db := testbuilder.SetupDB(t)

		result, err := ScanStores(ctx, db, u.dirs)

		require.NoError(t, err)
		assert.Equal(t, []string{"Cyberpunk 2077"}, result.New)
	})

	t.Run("a game that is there again is not new, and one that is gone is missing", func(t *testing.T) {
		t.Parallel()

		u := newUser(t)
		root := filepath.Join(u.dataHome, "Steam")
		installSteam(t, root, "72850", "Skyrim")
		writeSteamGame(t, root, "377160", "Fallout 4")
		db := testbuilder.SetupDB(t)
		_, err := ScanStores(ctx, db, u.dirs)
		require.NoError(t, err)
		require.NoError(t, os.Remove(filepath.Join(root, "steamapps", "appmanifest_377160.acf")))

		result, err := ScanStores(ctx, db, u.dirs)

		require.NoError(t, err)
		assert.Empty(t, result.New)
		assert.Equal(t, []string{"Skyrim"}, result.Updated)
		assert.Equal(t, []string{"Fallout 4"}, result.Missing)
	})

	t.Run("a Steam that can't be found does not make the games that were found missing", func(t *testing.T) {
		t.Parallel()

		u := newUser(t)
		root := filepath.Join(u.dataHome, "Steam")
		installSteam(t, root, "72850", "Skyrim")
		db := testbuilder.SetupDB(t)
		_, err := ScanStores(ctx, db, u.dirs)
		require.NoError(t, err)
		require.NoError(t, os.RemoveAll(root)) // an unmounted disk, say

		result, err := ScanStores(ctx, db, u.dirs)

		require.NoError(t, err)
		assert.Empty(t, result.Missing, "nothing was scanned, so nothing is known to be gone")
		assert.Equal(t, int64(1), readSteamState(t, dbq.New(db))["72850"].IsPresent)
	})

	t.Run("a store that is not enabled is not scanned", func(t *testing.T) {
		t.Parallel()

		u := newUser(t)
		installSteam(t, filepath.Join(u.dataHome, "Steam"), "72850", "Skyrim")
		db := testbuilder.SetupDB(t)
		_, err := db.Exec(`UPDATE stores SET enabled = FALSE WHERE id = 'steam'`)
		require.NoError(t, err)

		result, err := ScanStores(ctx, db, u.dirs)

		require.NoError(t, err)
		assert.Empty(t, result.New)
		assert.Empty(t, readSteamState(t, dbq.New(db)))
	})
}
