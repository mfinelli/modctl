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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyInstalls(t *testing.T) {
	t.Parallel()

	known := func(game, instance, name string, present bool) knownInstall {
		return knownInstall{game, instance, name, present}
	}
	found := func(game, instance, name string) foundInstall {
		return foundInstall{game, instance, name}
	}

	cases := []struct {
		name  string
		known []knownInstall
		found []foundInstall
		want  []RefreshChange
	}{
		{
			name: "nothing known, nothing found",
		},
		{
			name:  "a first sighting is new",
			found: []foundInstall{found("1", "default", "Alpha")},
			want:  []RefreshChange{{"Alpha", RefreshNew}},
		},
		{
			name:  "a known install that is present is updated",
			known: []knownInstall{known("1", "default", "Alpha", true)},
			found: []foundInstall{found("1", "default", "Alpha")},
			want:  []RefreshChange{{"Alpha", RefreshUpdated}},
		},
		{
			name:  "a known install that was missing has returned",
			known: []knownInstall{known("1", "default", "Alpha", false)},
			found: []foundInstall{found("1", "default", "Alpha")},
			want:  []RefreshChange{{"Alpha", RefreshReturned}},
		},
		{
			name:  "a present install that is not found is missing",
			known: []knownInstall{known("1", "default", "Alpha", true)},
			want:  []RefreshChange{{"Alpha", RefreshMissing}},
		},
		{
			name:  "an install that was already missing is not reported again",
			known: []knownInstall{known("1", "default", "Alpha", false)},
			want:  nil,
		},
		{
			name: "another instance of the same game is a different install",
			known: []knownInstall{
				known("1", "default", "Alpha", true),
			},
			found: []foundInstall{
				found("1", "default", "Alpha"),
				found("1", "library-2", "Alpha"),
			},
			want: []RefreshChange{{"Alpha", RefreshUpdated}, {"Alpha", RefreshNew}},
		},
		{
			name: "missing installs come first and by name, then installs in the order found",
			known: []knownInstall{
				known("3", "default", "Zulu", true),
				known("1", "default", "Alpha", false),
				known("2", "default", "Mike", true),
				known("4", "default", "Bravo", true),
			},
			found: []foundInstall{
				found("5", "default", "Echo"),
				found("1", "default", "Alpha"),
				found("4", "default", "Bravo"),
			},
			want: []RefreshChange{
				{"Mike", RefreshMissing},
				{"Zulu", RefreshMissing},
				{"Echo", RefreshNew},
				{"Alpha", RefreshReturned},
				{"Bravo", RefreshUpdated},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := classifyInstalls(tc.known, tc.found)
			if len(tc.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRefreshResultAddChanges(t *testing.T) {
	t.Parallel()

	var r RefreshResult
	r.addChanges([]RefreshChange{
		{"Gone", RefreshMissing},
		{"Fresh", RefreshNew},
		{"Back", RefreshReturned},
		{"Same", RefreshUpdated},
		{"Fresher", RefreshNew},
	})

	assert.Equal(t, []string{"Fresh", "Fresher"}, r.New)
	assert.Equal(t, []string{"Back"}, r.Returned)
	assert.Equal(t, []string{"Same"}, r.Updated)
	assert.Equal(t, []string{"Gone"}, r.Missing)
	assert.Len(t, r.Changes, 5, "the order of Changes is kept for display")
	assert.Equal(t, "Gone", r.Changes[0].Name)
}

func TestAssignSteamInstanceIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		libs  []string
		roots []string
		want  map[string]string
	}{
		{
			name: "no libraries",
			libs: nil,
			want: map[string]string{},
		},
		{
			name:  "a single library is the default",
			libs:  []string{"/home/u/.local/share/Steam"},
			roots: []string{"/home/u/.local/share/Steam"},
			want:  map[string]string{"/home/u/.local/share/Steam": "default"},
		},
		{
			name: "without a Steam installation the first library in order of path is the default",
			libs: []string{"/mnt/b", "/mnt/a", "/mnt/c"},
			want: map[string]string{"/mnt/a": "default", "/mnt/b": "library_2", "/mnt/c": "library_3"},
		},
		{
			name:  "the library in the Steam installation is the default, whatever its path",
			libs:  []string{"/games/a", "/home/u/.local/share/Steam", "/mnt/c"},
			roots: []string{"/home/u/.local/share/Steam"},
			want: map[string]string{
				"/home/u/.local/share/Steam": "default",
				"/games/a":                   "library_2",
				"/mnt/c":                     "library_3",
			},
		},
		{
			name:  "when it is also the first in order of path nothing changes",
			libs:  []string{"/a/Steam", "/mnt/b"},
			roots: []string{"/a/Steam"},
			want:  map[string]string{"/a/Steam": "default", "/mnt/b": "library_2"},
		},
		{
			name:  "a Steam installation that is not among the libraries is no help",
			libs:  []string{"/mnt/b", "/mnt/a"},
			roots: []string{"/home/u/.local/share/Steam"},
			want:  map[string]string{"/mnt/a": "default", "/mnt/b": "library_2"},
		},
		{
			name:  "of several Steam installations the first one that is a library wins",
			libs:  []string{"/a", "/native/Steam", "/flatpak/Steam"},
			roots: []string{"/missing/Steam", "/flatpak/Steam", "/native/Steam"},
			want: map[string]string{
				"/flatpak/Steam": "default",
				"/a":             "library_2",
				"/native/Steam":  "library_3",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			libs := append([]string(nil), tc.libs...)

			got := assignSteamInstanceIDs(tc.libs, tc.roots)

			assert.Equal(t, tc.want, got)
			assert.Equal(t, libs, tc.libs, "the libraries are left as they were")
		})
	}

	t.Run("the ids are default and then library_2 up to the number of libraries", func(t *testing.T) {
		t.Parallel()

		libs := []string{"/e", "/c", "/a", "/d", "/b"}

		got := assignSteamInstanceIDs(libs, []string{"/d"})

		ids := make([]string, 0, len(got))
		for _, id := range got {
			ids = append(ids, id)
		}
		assert.ElementsMatch(t,
			[]string{"default", "library_2", "library_3", "library_4", "library_5"}, ids)
		assert.Equal(t, "default", got["/d"])
	})

	t.Run("the default does not change when libraries are added", func(t *testing.T) {
		t.Parallel()

		roots := []string{"/home/u/.local/share/Steam"}
		before := assignSteamInstanceIDs(
			[]string{"/home/u/.local/share/Steam", "/mnt/games"}, roots)
		after := assignSteamInstanceIDs(
			[]string{"/aaa/new", "/home/u/.local/share/Steam", "/mnt/games", "/zzz/new"}, roots)

		assert.Equal(t, "default", before["/home/u/.local/share/Steam"])
		assert.Equal(t, "default", after["/home/u/.local/share/Steam"])
	})

	t.Run("the default does not change when libraries are removed", func(t *testing.T) {
		t.Parallel()

		roots := []string{"/home/u/.local/share/Steam"}
		got := assignSteamInstanceIDs([]string{"/home/u/.local/share/Steam"}, roots)

		assert.Equal(t, map[string]string{"/home/u/.local/share/Steam": "default"}, got)
	})

	// This is how it is for now, and what is meant to change: the other
	// libraries are numbered by their place in the order of paths, so a new
	// one takes the number of the ones after it.
	t.Run("the other libraries are still renumbered when one is added", func(t *testing.T) {
		t.Parallel()

		roots := []string{"/home/u/.local/share/Steam"}
		before := assignSteamInstanceIDs(
			[]string{"/home/u/.local/share/Steam", "/mnt/games"}, roots)
		after := assignSteamInstanceIDs(
			[]string{"/aaa/new", "/home/u/.local/share/Steam", "/mnt/games"}, roots)

		assert.Equal(t, "library_2", before["/mnt/games"])
		assert.Equal(t, "library_3", after["/mnt/games"])
	})
}

// writeSteamRoot makes a Steam installation at root, whose
// libraryfolders.vdf lists the given libraries (in the format that Steam
// writes now).
func writeSteamRoot(t *testing.T, root string, libs ...string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("\"libraryfolders\"\n{\n")
	for i, lib := range libs {
		fmt.Fprintf(&b, "\t\"%d\"\n\t{\n\t\t\"path\"\t\t\"%s\"\n\t\t\"label\"\t\t\"\"\n\t}\n", i, lib)
	}
	b.WriteString("}\n")

	writeVDF(t, root, b.String())
}

// sortedPaths is the paths in the order that the libraries come in.
func sortedPaths(paths ...string) []string {
	out := append([]string(nil), paths...)
	sort.Strings(out)
	return out
}

func writeVDF(t *testing.T, root, content string) {
	t.Helper()

	dir := filepath.Join(root, "steamapps")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "libraryfolders.vdf"), []byte(content), 0o644))
}

func TestDiscoverSteamLibrariesIn(t *testing.T) {
	t.Parallel()

	t.Run("reads the libraries of a Steam installation", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		ext := filepath.Join(dir, "games")
		writeSteamRoot(t, root, root, ext)

		got := discoverSteamLibrariesIn([]string{root})

		assert.True(t, got.DidScan)
		assert.Equal(t, sortedPaths(ext, root), got.Libs, "in order of path")
		assert.Equal(t, []string{root}, got.Roots)
		assert.Empty(t, got.Warnings)
	})

	t.Run("a place that is not a Steam installation is passed over", func(t *testing.T) {
		t.Parallel()

		got := discoverSteamLibrariesIn([]string{filepath.Join(t.TempDir(), "nothing")})

		assert.False(t, got.DidScan)
		assert.Empty(t, got.Libs)
		assert.Empty(t, got.Roots)
	})

	t.Run("no places at all", func(t *testing.T) {
		t.Parallel()

		got := discoverSteamLibrariesIn(nil)

		assert.False(t, got.DidScan)
		assert.Empty(t, got.Libs)
		assert.Empty(t, got.Roots)
	})

	t.Run("several installations are kept in the order they were looked for", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		native := filepath.Join(dir, "native", "Steam")
		flatpak := filepath.Join(dir, "flatpak", "Steam")
		shared := filepath.Join(dir, "shared")
		writeSteamRoot(t, native, native, shared)
		writeSteamRoot(t, flatpak, flatpak, shared)

		got := discoverSteamLibrariesIn([]string{flatpak, native})

		assert.Equal(t, []string{flatpak, native}, got.Roots)
		assert.Equal(t, sortedPaths(flatpak, native, shared), got.Libs, "a library that both list is there once")
	})

	t.Run("an installation that is reached in two ways counts once", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		writeSteamRoot(t, root, root)
		link := filepath.Join(dir, "link-to-steam")
		require.NoError(t, os.Symlink(root, link))

		got := discoverSteamLibrariesIn([]string{root, link})

		assert.Equal(t, []string{root}, got.Roots)
		assert.Equal(t, []string{root}, got.Libs)
	})

	t.Run("library paths are cleaned up", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		ext := filepath.Join(dir, "games")
		require.NoError(t, os.MkdirAll(ext, 0o755))
		writeSteamRoot(t, root, root+"/", ext+"/", filepath.Join(ext, "..", "games"))

		got := discoverSteamLibrariesIn([]string{root})

		assert.Equal(t, sortedPaths(ext, root), got.Libs)
	})

	t.Run("a file that can't be parsed is a warning and not a scan", func(t *testing.T) {
		t.Parallel()

		root := filepath.Join(t.TempDir(), "Steam")
		writeVDF(t, root, "not vdf at all <<<>>>")

		got := discoverSteamLibrariesIn([]string{root})

		assert.False(t, got.DidScan)
		assert.Empty(t, got.Roots)
		require.Len(t, got.Warnings, 1)
		assert.Contains(t, got.Warnings[0], "failed to parse")
	})

	t.Run("a file with no libraries in it is a scan with nothing found", func(t *testing.T) {
		t.Parallel()

		root := filepath.Join(t.TempDir(), "Steam")
		writeVDF(t, root, "\"libraryfolders\"\n{\n}\n")

		got := discoverSteamLibrariesIn([]string{root})

		assert.True(t, got.DidScan)
		assert.Empty(t, got.Libs)
		require.Len(t, got.Warnings, 1)
		assert.Contains(t, got.Warnings[0], "no libraries found")
	})

	t.Run("the library in the Steam installation is the default, with other libraries around it", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		// "A-games" sorts before "Steam", so it would be the default if that
		// were by order of path
		root := filepath.Join(dir, "Steam")
		first := filepath.Join(dir, "A-games")
		last := filepath.Join(dir, "Z-games")
		writeSteamRoot(t, root, root, last, first)

		found := discoverSteamLibrariesIn([]string{root})
		got := assignSteamInstanceIDs(found.Libs, found.Roots)

		assert.Equal(t, map[string]string{
			root:  "default",
			first: "library_2",
			last:  "library_3",
		}, got)
	})
}
