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
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/andygrunwald/vdf"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mfinelli/modctl/dbq"
	"github.com/pressly/goose/v3"
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

			got := assignSteamInstanceIDs(tc.libs, tc.roots, nil, nil)

			assert.Equal(t, tc.want, got)
			assert.Equal(t, libs, tc.libs, "the libraries are left as they were")
		})
	}

	t.Run("the ids are default and then library_2 up to the number of libraries", func(t *testing.T) {
		t.Parallel()

		libs := []string{"/e", "/c", "/a", "/d", "/b"}

		got := assignSteamInstanceIDs(libs, []string{"/d"}, nil, nil)

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
			[]string{"/home/u/.local/share/Steam", "/mnt/games"}, roots, nil, nil)
		after := assignSteamInstanceIDs(
			[]string{"/aaa/new", "/home/u/.local/share/Steam", "/mnt/games", "/zzz/new"}, roots, nil, nil)

		assert.Equal(t, "default", before["/home/u/.local/share/Steam"])
		assert.Equal(t, "default", after["/home/u/.local/share/Steam"])
	})

	t.Run("the default does not change when libraries are removed", func(t *testing.T) {
		t.Parallel()

		roots := []string{"/home/u/.local/share/Steam"}
		got := assignSteamInstanceIDs([]string{"/home/u/.local/share/Steam"}, roots, nil, nil)

		assert.Equal(t, map[string]string{"/home/u/.local/share/Steam": "default"}, got)
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
		got := assignSteamInstanceIDs(found.Libs, found.Roots, nil, nil)

		assert.Equal(t, map[string]string{
			root:  "default",
			first: "library_2",
			last:  "library_3",
		}, got)
	})
}

func TestAssignSteamInstanceIDsKeepsWhatLibrariesHave(t *testing.T) {
	t.Parallel()

	const steamLib = "/home/u/.local/share/Steam"

	set := func(ids ...string) map[string]struct{} {
		m := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			m[id] = struct{}{}
		}
		return m
	}

	tests := []struct {
		name  string
		libs  []string
		roots []string
		known map[string]string
		inUse map[string]struct{}
		want  map[string]string
	}{
		{
			name:  "a new library does not take the number of the ones after it",
			libs:  []string{"/aaa/new", steamLib, "/mnt/games"},
			roots: []string{steamLib},
			known: map[string]string{steamLib: "default", "/mnt/games": "library_2"},
			inUse: set("default", "library_2"),
			want: map[string]string{
				steamLib:     "default",
				"/mnt/games": "library_2",
				"/aaa/new":   "library_3",
			},
		},
		{
			name:  "libraries that go away leave the others as they are",
			libs:  []string{steamLib, "/mnt/c"},
			roots: []string{steamLib},
			known: map[string]string{steamLib: "default", "/mnt/b": "library_2", "/mnt/c": "library_3"},
			inUse: set("default", "library_2", "library_3"),
			want:  map[string]string{steamLib: "default", "/mnt/c": "library_3"},
		},
		{
			name:  "the id of a library that is not there is not given to another",
			libs:  []string{steamLib, "/mnt/new"},
			roots: []string{steamLib},
			known: map[string]string{steamLib: "default"},
			inUse: set("default", "library_2"),
			want:  map[string]string{steamLib: "default", "/mnt/new": "library_3"},
		},
		{
			name:  "ids are not given to a library that comes back after one was given out",
			libs:  []string{steamLib, "/mnt/games", "/mnt/new"},
			roots: []string{steamLib},
			known: map[string]string{steamLib: "default", "/mnt/games": "library_2"},
			inUse: set("default", "library_2", "library_3"),
			want:  map[string]string{steamLib: "default", "/mnt/games": "library_2", "/mnt/new": "library_4"},
		},
		{
			name:  "a library that is the default stays it, whatever the Steam installation is",
			libs:  []string{"/games/a", steamLib},
			roots: []string{steamLib},
			known: map[string]string{"/games/a": "default", steamLib: "library_2"},
			inUse: set("default", "library_2"),
			want:  map[string]string{"/games/a": "default", steamLib: "library_2"},
		},
		{
			name:  "a new library takes the default when nothing has it",
			libs:  []string{"/mnt/b", steamLib},
			roots: []string{steamLib},
			known: map[string]string{"/mnt/b": "library_2"},
			inUse: set("library_2"),
			want:  map[string]string{steamLib: "default", "/mnt/b": "library_2"},
		},
		{
			name:  "but not when it belongs to a library that is not there",
			libs:  []string{"/mnt/b", steamLib},
			roots: []string{steamLib},
			known: map[string]string{"/mnt/b": "library_2"},
			inUse: set("default", "library_2"),
			want:  map[string]string{steamLib: "library_3", "/mnt/b": "library_2"},
		},
		{
			name:  "with nothing known it is as it always was",
			libs:  []string{"/aaa", steamLib, "/zzz"},
			roots: []string{steamLib},
			known: map[string]string{},
			inUse: set(),
			want:  map[string]string{steamLib: "default", "/aaa": "library_2", "/zzz": "library_3"},
		},
		{
			name:  "an id that two libraries claim goes to the first, and the other is given a new one",
			libs:  []string{"/mnt/a", "/mnt/b"},
			known: map[string]string{"/mnt/a": "default", "/mnt/b": "default"},
			inUse: set("default"),
			want:  map[string]string{"/mnt/a": "default", "/mnt/b": "library_2"},
		},
		{
			name:  "ids that are already in use are skipped over when numbering",
			libs:  []string{steamLib, "/mnt/a", "/mnt/b", "/mnt/c"},
			roots: []string{steamLib},
			known: map[string]string{steamLib: "default", "/mnt/b": "library_3"},
			inUse: set("default", "library_3", "library_4"),
			want: map[string]string{
				steamLib: "default",
				"/mnt/b": "library_3",
				"/mnt/a": "library_2",
				"/mnt/c": "library_5",
			},
		},
		{
			name:  "an empty id is no id",
			libs:  []string{steamLib},
			roots: []string{steamLib},
			known: map[string]string{steamLib: ""},
			want:  map[string]string{steamLib: "default"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := assignSteamInstanceIDs(tc.libs, tc.roots, tc.known, tc.inUse)

			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("the maps that are given are left as they were", func(t *testing.T) {
		t.Parallel()

		known := map[string]string{steamLib: "default"}
		inUse := set("default")

		assignSteamInstanceIDs([]string{steamLib, "/mnt/new"}, []string{steamLib}, known, inUse)

		assert.Equal(t, map[string]string{steamLib: "default"}, known)
		assert.Equal(t, set("default"), inUse)
	})

	t.Run("no two libraries get the same id", func(t *testing.T) {
		t.Parallel()

		libs := []string{"/a", "/b", "/c", "/d", "/e", "/f"}
		got := assignSteamInstanceIDs(libs, nil,
			map[string]string{"/b": "library_2", "/e": "library_3", "/f": "default"},
			set("default", "library_2", "library_3", "library_7"))

		seen := map[string]string{}
		for lib, id := range got {
			other, dup := seen[id]
			assert.False(t, dup, "%s and %s both have %s", lib, other, id)
			seen[id] = lib
		}
		assert.Len(t, got, len(libs))
	})
}

func TestExistingLibraryInstances(t *testing.T) {
	t.Parallel()

	in := func(id, lib string, present bool) installLibrary {
		return installLibrary{InstanceID: id, LibraryRoot: lib, Present: present}
	}

	tests := []struct {
		name      string
		installs  []installLibrary
		wantByLib map[string]string
		wantInUse []string
	}{
		{
			name:      "nothing is known",
			wantByLib: map[string]string{},
			wantInUse: []string{},
		},
		{
			name: "each library has the id of its installs",
			installs: []installLibrary{
				in("default", "/a", true),
				in("default", "/a", true),
				in("library_2", "/b", true),
			},
			wantByLib: map[string]string{"/a": "default", "/b": "library_2"},
			wantInUse: []string{"default", "library_2"},
		},
		{
			name: "an install that doesn't say where it was found still has its id in use",
			installs: []installLibrary{
				in("default", "/a", true),
				in("library_2", "", true),
			},
			wantByLib: map[string]string{"/a": "default"},
			wantInUse: []string{"default", "library_2"},
		},
		{
			name: "an install without an id is nothing",
			installs: []installLibrary{
				in("", "/a", true),
			},
			wantByLib: map[string]string{},
			wantInUse: []string{},
		},
		{
			name: "a library has the id that most of its installs have",
			installs: []installLibrary{
				in("default", "/a", false),
				in("library_2", "/a", true),
				in("library_2", "/a", true),
			},
			wantByLib: map[string]string{"/a": "library_2"},
			wantInUse: []string{"default", "library_2"},
		},
		{
			name: "of two libraries that claim an id it goes to the one with most installs",
			installs: []installLibrary{
				in("default", "/a", true),
				in("default", "/b", true),
				in("default", "/b", true),
			},
			wantByLib: map[string]string{"/b": "default"},
			wantInUse: []string{"default"},
		},
		{
			name: "then to the one with most that are present",
			installs: []installLibrary{
				in("default", "/a", false),
				in("default", "/b", true),
			},
			wantByLib: map[string]string{"/b": "default"},
			wantInUse: []string{"default"},
		},
		{
			name: "then to the one that is first in order of path",
			installs: []installLibrary{
				in("default", "/b", true),
				in("default", "/a", true),
			},
			wantByLib: map[string]string{"/a": "default"},
			wantInUse: []string{"default"},
		},
		{
			name: "a library that lost an id to another is left to take the next best",
			installs: []installLibrary{
				in("default", "/a", true),
				in("default", "/a", true),
				in("default", "/b", true),
				in("library_2", "/b", true),
			},
			wantByLib: map[string]string{"/a": "default", "/b": "library_2"},
			wantInUse: []string{"default", "library_2"},
		},
		{
			name: "of ids that are equally common the default comes first, then in order of number",
			installs: []installLibrary{
				in("library_10", "/a", true),
				in("library_3", "/a", true),
			},
			wantByLib: map[string]string{"/a": "library_3"},
			wantInUse: []string{"library_10", "library_3"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ex := existingLibraryInstances(tc.installs)

			assert.Equal(t, tc.wantByLib, ex.ByPath)
			ids := make([]string, 0, len(ex.InUse))
			for id := range ex.InUse {
				ids = append(ids, id)
			}
			assert.ElementsMatch(t, tc.wantInUse, ids)
		})
	}
}

func TestLibraryOfInstall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		metadata      sql.NullString
		wantRoot      string
		wantContentID string
	}{
		{"the metadata that discovery writes", sql.NullString{
			String: `{"install_root_raw":"/lib/steamapps/common/Game","library_root":"/lib","manifest_path":"/lib/steamapps/appmanifest_1.acf","steamapps_root":"/lib/steamapps"}`,
			Valid:  true,
		}, "/lib", ""},
		{"with the content id of the library", sql.NullString{
			String: `{"library_root":"/lib","library_content_id":"-7695053421422003000"}`,
			Valid:  true,
		}, "/lib", "-7695053421422003000"},
		{"a content id without the library", sql.NullString{
			String: `{"library_content_id":"123"}`,
			Valid:  true,
		}, "", "123"},
		{"no metadata", sql.NullString{}, "", ""},
		{"empty metadata", sql.NullString{String: "", Valid: true}, "", ""},
		{"metadata that is not json", sql.NullString{String: "library_root=/lib", Valid: true}, "", ""},
		{"json without the library", sql.NullString{String: `{"manifest_path":"/x"}`, Valid: true}, "", ""},
		{"json of another shape", sql.NullString{String: `["/lib"]`, Valid: true}, "", ""},
		{"a library that is not a string", sql.NullString{String: `{"library_root":7}`, Valid: true}, "", ""},
		{"a content id that is not a string", sql.NullString{String: `{"library_root":"/lib","library_content_id":123}`, Valid: true}, "", ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, contentID := libraryOfInstall(tc.metadata)

			assert.Equal(t, tc.wantRoot, root)
			assert.Equal(t, tc.wantContentID, contentID)
		})
	}
}

// migratedDB is an empty database with all of the migrations applied.
func migratedDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err)
	// every connection to :memory: is its own database
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	p, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"))
	require.NoError(t, err)
	_, err = p.Up(context.Background())
	require.NoError(t, err)

	return db
}

// writeSteamGame puts a game in a Steam library, with the manifest that Steam
// writes for it.
func writeSteamGame(t *testing.T, lib, appid, name string) {
	t.Helper()

	steamapps := filepath.Join(lib, "steamapps")
	require.NoError(t, os.MkdirAll(filepath.Join(steamapps, "common", name), 0o755))

	manifest := fmt.Sprintf("\"AppState\"\n{\n\t\"appid\"\t\t\"%s\"\n\t\"name\"\t\t\"%s\"\n\t\"installdir\"\t\t\"%s\"\n}\n", appid, name, name)
	require.NoError(t, os.WriteFile(filepath.Join(steamapps, "appmanifest_"+appid+".acf"), []byte(manifest), 0o644))
}

// steamState is what the database knows of the Steam installs, by game.
type steamState map[string]dbq.GameInstall

func readSteamState(t *testing.T, q *dbq.Queries) steamState {
	t.Helper()

	installs, err := q.ListGameInstallsByStore(context.Background(), "steam")
	require.NoError(t, err)

	state := make(steamState, len(installs))
	for _, in := range installs {
		_, dup := state[in.StoreGameID]
		require.False(t, dup, "game %s is there twice", in.StoreGameID)
		state[in.StoreGameID] = in
	}
	return state
}

func TestRefreshSteamLibraries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// libs makes what discovery gives for the given libraries, the first of
	// which is in the Steam installation.
	libs := func(root string, others ...string) steamLibraries {
		return steamLibraries{
			Libs:    sortedPaths(append([]string{root}, others...)...),
			Roots:   []string{root},
			DidScan: true,
		}
	}

	refresh := func(t *testing.T, db *sql.DB, steam steamLibraries) steamState {
		t.Helper()

		q := dbq.New(db)
		_, err := refreshSteamLibraries(ctx, db, q, steam)
		require.NoError(t, err)
		return readSteamState(t, q)
	}

	t.Run("a new library does not change the installs that are there", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		ext := filepath.Join(dir, "games")
		added := filepath.Join(dir, "A-added") // sorts before the others
		writeSteamGame(t, root, "100", "Root Game")
		writeSteamGame(t, ext, "200", "Ext Game")
		writeSteamGame(t, added, "300", "Added Game")
		db := migratedDB(t)

		first := refresh(t, db, libs(root, ext))
		require.Equal(t, "default", first["100"].InstanceID)
		require.Equal(t, "library_2", first["200"].InstanceID)

		second := refresh(t, db, libs(root, ext, added))

		assert.Equal(t, first["100"].ID, second["100"].ID)
		assert.Equal(t, "default", second["100"].InstanceID)
		assert.Equal(t, first["200"].ID, second["200"].ID)
		assert.Equal(t, "library_2", second["200"].InstanceID)
		assert.Equal(t, filepath.Join(ext, "steamapps", "common", "Ext Game"), second["200"].InstallRoot)
		assert.Equal(t, "library_3", second["300"].InstanceID)
	})

	t.Run("an install keeps its id while its library is away, and when it is back", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		ext := filepath.Join(dir, "games")
		other := filepath.Join(dir, "other")
		writeSteamGame(t, root, "100", "Root Game")
		writeSteamGame(t, ext, "200", "Ext Game")
		writeSteamGame(t, other, "400", "Other Game")
		db := migratedDB(t)

		first := refresh(t, db, libs(root, ext))
		require.Equal(t, "library_2", first["200"].InstanceID)

		// the disk with the library on it isn't there
		away := refresh(t, db, libs(root))
		assert.Equal(t, int64(0), away["200"].IsPresent)
		assert.Equal(t, "library_2", away["200"].InstanceID)

		// a library that shows up meanwhile can't have the id of the one
		// that is away
		meanwhile := refresh(t, db, libs(root, other))
		assert.Equal(t, "library_3", meanwhile["400"].InstanceID)
		assert.Equal(t, "library_2", meanwhile["200"].InstanceID)

		back := refresh(t, db, libs(root, ext, other))
		assert.Equal(t, first["200"].ID, back["200"].ID)
		assert.Equal(t, int64(1), back["200"].IsPresent)
		assert.Equal(t, "library_2", back["200"].InstanceID)
		assert.Equal(t, filepath.Join(ext, "steamapps", "common", "Ext Game"), back["200"].InstallRoot)
		assert.Equal(t, "library_3", back["400"].InstanceID)
	})

	t.Run("what the libraries had before this was kept is what they keep", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		first := filepath.Join(dir, "A-games") // first in order of path
		root := filepath.Join(dir, "Steam")
		writeSteamGame(t, first, "100", "A Game")
		writeSteamGame(t, root, "200", "Steam Game")
		db := migratedDB(t)

		// as it was when the default was the first library in order of
		// path: no Steam installation among them
		before := refresh(t, db, steamLibraries{
			Libs:    sortedPaths(first, root),
			DidScan: true,
		})
		require.Equal(t, "default", before["100"].InstanceID)
		require.Equal(t, "library_2", before["200"].InstanceID)

		// now the Steam installation is known, which would make its library
		// the default if it was starting over
		after := refresh(t, db, libs(root, first))

		assert.Equal(t, "default", after["100"].InstanceID)
		assert.Equal(t, "library_2", after["200"].InstanceID)
		assert.Equal(t, before["100"].ID, after["100"].ID)
		assert.Equal(t, before["200"].ID, after["200"].ID)
	})

	t.Run("the library in the Steam installation is the default to start with", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		first := filepath.Join(dir, "A-games")
		root := filepath.Join(dir, "Steam")
		writeSteamGame(t, first, "100", "A Game")
		writeSteamGame(t, root, "200", "Steam Game")
		db := migratedDB(t)

		got := refresh(t, db, libs(root, first))

		assert.Equal(t, "default", got["200"].InstanceID)
		assert.Equal(t, "library_2", got["100"].InstanceID)
	})

	t.Run("the same refresh twice changes nothing", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		ext := filepath.Join(dir, "games")
		writeSteamGame(t, root, "100", "Root Game")
		writeSteamGame(t, ext, "200", "Ext Game")
		db := migratedDB(t)

		first := refresh(t, db, libs(root, ext))
		second := refresh(t, db, libs(root, ext))

		for appid, in := range first {
			assert.Equal(t, in.ID, second[appid].ID, appid)
			assert.Equal(t, in.InstanceID, second[appid].InstanceID, appid)
		}
		assert.Len(t, second, len(first))
	})

	t.Run("a refresh that found no Steam changes nothing", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		writeSteamGame(t, root, "100", "Root Game")
		db := migratedDB(t)

		first := refresh(t, db, libs(root))
		second := refresh(t, db, steamLibraries{DidScan: false})

		assert.Equal(t, first["100"].ID, second["100"].ID)
		assert.Equal(t, int64(1), second["100"].IsPresent, "not marked as missing")
	})
}

func parseVDF(t *testing.T, text string) any {
	t.Helper()

	parsed, err := vdf.NewParser(strings.NewReader(text)).Parse()
	require.NoError(t, err)
	return parsed
}

func TestExtractSteamLibraries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vdf  string
		want []steamLibraryEntry
	}{
		{
			name: "the format that Steam writes now, with content ids",
			vdf: `"libraryfolders"
{
	"0"
	{
		"path"		"/home/u/.local/share/Steam"
		"label"		""
		"contentid"		"3328371409298419016"
		"totalsize"		"0"
		"apps"
		{
			"228980"		"1234"
		}
	}
	"1"
	{
		"path"		"/mnt/games"
		"contentid"		"1039182383252157525"
	}
}`,
			want: []steamLibraryEntry{
				{Path: "/home/u/.local/share/Steam", ContentID: "3328371409298419016"},
				{Path: "/mnt/games", ContentID: "1039182383252157525"},
			},
		},
		{
			name: "a content id can be negative",
			vdf:  `"libraryfolders" { "0" { "path" "/a" "contentid" "-7695053421422003000" } }`,
			want: []steamLibraryEntry{{Path: "/a", ContentID: "-7695053421422003000"}},
		},
		{
			name: "a library that Steam has not given one yet has none",
			vdf:  `"libraryfolders" { "0" { "path" "/a" "contentid" "5" } "1" { "path" "/b" } }`,
			want: []steamLibraryEntry{{Path: "/a", ContentID: "5"}, {Path: "/b"}},
		},
		{
			name: "zero is no content id",
			vdf:  `"libraryfolders" { "0" { "path" "/a" "contentid" "0" } }`,
			want: []steamLibraryEntry{{Path: "/a"}},
		},
		{
			name: "an empty content id is none",
			vdf:  `"libraryfolders" { "0" { "path" "/a" "contentid" "  " } }`,
			want: []steamLibraryEntry{{Path: "/a"}},
		},
		{
			name: "the old format has paths only",
			vdf:  `"libraryfolders" { "0" "/a" "1" "/b" }`,
			want: []steamLibraryEntry{{Path: "/a"}, {Path: "/b"}},
		},
		{
			name: "the entries come in the order of their keys, not as the file lists them",
			vdf:  `"libraryfolders" { "10" { "path" "/ten" } "2" { "path" "/two" } "1" { "path" "/one" } }`,
			want: []steamLibraryEntry{{Path: "/one"}, {Path: "/two"}, {Path: "/ten"}},
		},
		{
			name: "keys that are not libraries are passed over",
			vdf:  `"libraryfolders" { "contentstatsid" "123" "0" { "path" "/a" } }`,
			want: []steamLibraryEntry{{Path: "/a"}},
		},
		{
			name: "an entry without a path is passed over",
			vdf:  `"libraryfolders" { "0" { "contentid" "5" } "1" { "path" "/b" } }`,
			want: []steamLibraryEntry{{Path: "/b"}},
		},
		{
			name: "nothing at all",
			vdf:  `"libraryfolders" { }`,
			want: nil,
		},
		{
			name: "something else",
			vdf:  `"appstate" { "appid" "1" }`,
			want: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, extractSteamLibraries(parseVDF(t, tc.vdf)))
		})
	}

	t.Run("a parsed file that is not a map", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, extractSteamLibraries("libraryfolders"))
		assert.Nil(t, extractSteamLibraries(nil))
	})
}

// writeSteamRootWithIDs is writeSteamRoot for libraries that have content ids.
func writeSteamRootWithIDs(t *testing.T, root string, libs ...steamLibraryEntry) {
	t.Helper()

	var b strings.Builder
	b.WriteString("\"libraryfolders\"\n{\n")
	for i, lib := range libs {
		fmt.Fprintf(&b, "\t\"%d\"\n\t{\n\t\t\"path\"\t\t\"%s\"\n", i, lib.Path)
		if lib.ContentID != "" {
			fmt.Fprintf(&b, "\t\t\"contentid\"\t\t\"%s\"\n", lib.ContentID)
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")

	writeVDF(t, root, b.String())
}

func TestDiscoverSteamLibrariesInContentIDs(t *testing.T) {
	t.Parallel()

	t.Run("the content ids are by library path", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		ext := filepath.Join(dir, "games")
		bare := filepath.Join(dir, "no-id")
		writeSteamRootWithIDs(t, root,
			steamLibraryEntry{Path: root, ContentID: "111"},
			steamLibraryEntry{Path: ext, ContentID: "-222"},
			steamLibraryEntry{Path: bare})

		got := discoverSteamLibrariesIn([]string{root})

		assert.Equal(t, map[string]string{root: "111", ext: "-222"}, got.ContentIDs)
		assert.Equal(t, sortedPaths(root, ext, bare), got.Libs)
		assert.Empty(t, got.Warnings)
	})

	t.Run("without any there are none", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		writeSteamRoot(t, root, root)

		got := discoverSteamLibrariesIn([]string{root})

		assert.Empty(t, got.ContentIDs)
	})

	t.Run("a library that two installations list is known once", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		native := filepath.Join(dir, "native", "Steam")
		flatpak := filepath.Join(dir, "flatpak", "Steam")
		shared := filepath.Join(dir, "shared")
		writeSteamRootWithIDs(t, native,
			steamLibraryEntry{Path: native, ContentID: "1"},
			steamLibraryEntry{Path: shared, ContentID: "9"})
		writeSteamRootWithIDs(t, flatpak,
			steamLibraryEntry{Path: flatpak, ContentID: "2"},
			steamLibraryEntry{Path: shared, ContentID: "9"})

		got := discoverSteamLibrariesIn([]string{native, flatpak})

		assert.Equal(t, map[string]string{native: "1", flatpak: "2", shared: "9"}, got.ContentIDs)
		assert.Empty(t, got.Warnings, "it is one library, not two that look alike")
	})

	t.Run("two content ids for one library is a warning, and the first is used", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		native := filepath.Join(dir, "native", "Steam")
		flatpak := filepath.Join(dir, "flatpak", "Steam")
		shared := filepath.Join(dir, "shared")
		writeSteamRootWithIDs(t, native, steamLibraryEntry{Path: shared, ContentID: "9"})
		writeSteamRootWithIDs(t, flatpak, steamLibraryEntry{Path: shared, ContentID: "10"})

		got := discoverSteamLibrariesIn([]string{native, flatpak})

		assert.Equal(t, "9", got.ContentIDs[shared])
		require.Len(t, got.Warnings, 1)
		assert.Contains(t, got.Warnings[0], "two content ids")
		assert.Contains(t, got.Warnings[0], shared)
	})

	t.Run("libraries that share a content id are a warning", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		a := filepath.Join(dir, "a-disk")
		b := filepath.Join(dir, "b-disk")
		writeSteamRootWithIDs(t, root,
			steamLibraryEntry{Path: root, ContentID: "1"},
			steamLibraryEntry{Path: b, ContentID: "77"},
			steamLibraryEntry{Path: a, ContentID: "77"})

		got := discoverSteamLibrariesIn([]string{root})

		assert.Equal(t, "77", got.ContentIDs[a])
		assert.Equal(t, "77", got.ContentIDs[b])
		require.Len(t, got.Warnings, 1)
		assert.Contains(t, got.Warnings[0], "same content id")
		assert.Contains(t, got.Warnings[0], "(77)")
		assert.Contains(t, got.Warnings[0], fmt.Sprintf("%s, %s", a, b), "in order of path")
	})
}

func TestKnownLibraryInstances(t *testing.T) {
	t.Parallel()

	in := func(id, root, contentID string) installLibrary {
		return installLibrary{InstanceID: id, LibraryRoot: root, LibraryContentID: contentID, Present: true}
	}

	t.Run("by path when there are no content ids", func(t *testing.T) {
		t.Parallel()

		ex := existingLibraryInstances([]installLibrary{in("default", "/a", ""), in("library_2", "/b", "")})

		known, moves := knownLibraryInstances([]string{"/a", "/b", "/c"}, nil, ex)

		assert.Equal(t, map[string]string{"/a": "default", "/b": "library_2"}, known)
		assert.Empty(t, moves)
	})

	t.Run("a library that is where it was is not a move", func(t *testing.T) {
		t.Parallel()

		ex := existingLibraryInstances([]installLibrary{in("library_2", "/b", "22")})

		known, moves := knownLibraryInstances([]string{"/b"}, map[string]string{"/b": "22"}, ex)

		assert.Equal(t, map[string]string{"/b": "library_2"}, known)
		assert.Empty(t, moves)
	})

	t.Run("a library that has moved keeps its id, and the move is reported", func(t *testing.T) {
		t.Parallel()

		ex := existingLibraryInstances([]installLibrary{in("library_2", "/old/b", "22")})

		known, moves := knownLibraryInstances([]string{"/new/b"}, map[string]string{"/new/b": "22"}, ex)

		assert.Equal(t, map[string]string{"/new/b": "library_2"}, known)
		assert.Equal(t, []steamLibraryMove{{Lib: "/new/b", From: []string{"/old/b"}, InstanceID: "library_2"}}, moves)
	})

	t.Run("where it was is where most of its installs were", func(t *testing.T) {
		t.Parallel()

		ex := existingLibraryInstances([]installLibrary{
			in("library_2", "/first", "22"),
			in("library_2", "/second", "22"),
			in("library_2", "/second", "22"),
		})

		_, moves := knownLibraryInstances([]string{"/third"}, map[string]string{"/third": "22"}, ex)

		require.Len(t, moves, 1)
		assert.Equal(t, []string{"/second", "/first"}, moves[0].From)
	})

	t.Run("a library that has no id yet is known by its path, and that is how it gets one", func(t *testing.T) {
		t.Parallel()

		// what is known was found before there were content ids
		ex := existingLibraryInstances([]installLibrary{in("library_2", "/b", "")})

		known, moves := knownLibraryInstances([]string{"/b"}, map[string]string{"/b": "22"}, ex)

		assert.Equal(t, map[string]string{"/b": "library_2"}, known)
		assert.Empty(t, moves, "nothing says it has moved")
	})

	t.Run("the content id comes before the path", func(t *testing.T) {
		t.Parallel()

		// /p used to be the library 33, and the library 22 is there now
		ex := existingLibraryInstances([]installLibrary{
			in("library_2", "/p", "33"),
			in("library_3", "/q", "22"),
		})

		known, _ := knownLibraryInstances([]string{"/p"}, map[string]string{"/p": "22"}, ex)

		assert.Equal(t, map[string]string{"/p": "library_3"}, known)
	})

	t.Run("a library that takes the place of one that moved does not take its id", func(t *testing.T) {
		t.Parallel()

		// library 22 was at /p and is at /q now, and library 44 is at /p
		ex := existingLibraryInstances([]installLibrary{in("library_2", "/p", "22")})

		known, moves := knownLibraryInstances(
			[]string{"/p", "/q"},
			map[string]string{"/p": "44", "/q": "22"},
			ex)

		assert.Equal(t, map[string]string{"/q": "library_2"}, known, "/p is not given library_2 as well")
		require.Len(t, moves, 1)
		assert.Equal(t, "/q", moves[0].Lib)
	})

	t.Run("but one that has no content id at all can't be told, and is known by its path", func(t *testing.T) {
		t.Parallel()

		ex := existingLibraryInstances([]installLibrary{in("library_2", "/p", "22")})

		known, _ := knownLibraryInstances([]string{"/p"}, nil, ex)

		assert.Equal(t, map[string]string{"/p": "library_2"}, known)
	})

	t.Run("libraries that share a content id are known by their paths", func(t *testing.T) {
		t.Parallel()

		ex := existingLibraryInstances([]installLibrary{
			in("library_2", "/a", "77"),
			in("library_3", "/b", "77"),
		})

		known, moves := knownLibraryInstances(
			[]string{"/a", "/b"},
			map[string]string{"/a": "77", "/b": "77"},
			ex)

		assert.Equal(t, map[string]string{"/a": "library_2", "/b": "library_3"}, known)
		assert.Empty(t, moves)
	})

	t.Run("a copy of a library next to the library is not it", func(t *testing.T) {
		t.Parallel()

		// 77 is at /a, and has been copied to /copy: neither is a move
		ex := existingLibraryInstances([]installLibrary{in("library_2", "/a", "77")})

		known, moves := knownLibraryInstances(
			[]string{"/a", "/copy"},
			map[string]string{"/a": "77", "/copy": "77"},
			ex)

		assert.Equal(t, map[string]string{"/a": "library_2"}, known)
		assert.Empty(t, moves)
	})

	t.Run("a library that nothing is known of is not known", func(t *testing.T) {
		t.Parallel()

		ex := existingLibraryInstances([]installLibrary{in("default", "/a", "11")})

		known, moves := knownLibraryInstances([]string{"/new"}, map[string]string{"/new": "99"}, ex)

		assert.Empty(t, known)
		assert.Empty(t, moves)
	})
}

func TestExistingLibraryInstancesContentIDs(t *testing.T) {
	t.Parallel()

	in := func(id, root, contentID string, present bool) installLibrary {
		return installLibrary{InstanceID: id, LibraryRoot: root, LibraryContentID: contentID, Present: present}
	}

	t.Run("by content id and by path", func(t *testing.T) {
		t.Parallel()

		ex := existingLibraryInstances([]installLibrary{
			in("default", "/a", "11", true),
			in("library_2", "/b", "22", true),
			in("library_3", "/c", "", true),
		})

		assert.Equal(t, map[string]string{"/a": "default", "/b": "library_2", "/c": "library_3"}, ex.ByPath)
		assert.Equal(t, map[string]string{"11": "default", "22": "library_2"}, ex.ByContentID)
	})

	t.Run("a library that moved has the one id, wherever its installs were", func(t *testing.T) {
		t.Parallel()

		ex := existingLibraryInstances([]installLibrary{
			in("library_2", "/old", "22", false),
			in("library_2", "/new", "22", true),
			in("library_2", "/new", "22", true),
		})

		assert.Equal(t, map[string]string{"22": "library_2"}, ex.ByContentID)
		assert.Equal(t, []string{"/new", "/old"}, ex.RootsOfContentID["22"])
	})

	t.Run("the content ids that were at a path", func(t *testing.T) {
		t.Parallel()

		ex := existingLibraryInstances([]installLibrary{
			in("library_2", "/p", "22", false),
			in("library_3", "/p", "44", true),
			in("library_3", "/p", "44", true),
			in("library_4", "/q", "", true),
		})

		assert.Equal(t, []string{"22", "44"}, ex.ContentIDsOfRoot["/p"])
		assert.NotContains(t, ex.ContentIDsOfRoot, "/q", "no content id to speak of")
	})

	t.Run("claims are resolved on their own for each of the two", func(t *testing.T) {
		t.Parallel()

		// two libraries claim library_2 by content id: the one with most
		// installs has it, and the other has none by content id, but still
		// has the one that its path says
		ex := existingLibraryInstances([]installLibrary{
			in("library_2", "/a", "77", true),
			in("library_2", "/b", "77", true),
			in("library_2", "/b", "77", true),
		})

		assert.Equal(t, map[string]string{"77": "library_2"}, ex.ByContentID)
		assert.Equal(t, map[string]string{"/b": "library_2"}, ex.ByPath)
	})
}

func TestRefreshSteamLibrariesContentIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// libs makes what discovery gives, the first library being the one in the
	// Steam installation.
	libs := func(ids map[string]string, root string, others ...string) steamLibraries {
		return steamLibraries{
			Libs:       sortedPaths(append([]string{root}, others...)...),
			Roots:      []string{root},
			ContentIDs: ids,
			DidScan:    true,
		}
	}

	refresh := func(t *testing.T, db *sql.DB, steam steamLibraries) (steamState, RefreshResult) {
		t.Helper()

		q := dbq.New(db)
		result, err := refreshSteamLibraries(ctx, db, q, steam)
		require.NoError(t, err)
		return readSteamState(t, q), result
	}

	t.Run("a library that moves keeps its instance, its installs and what is attached to them", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		before := filepath.Join(dir, "disk-a", "SteamLibrary")
		after := filepath.Join(dir, "disk-b", "SteamLibrary")
		writeSteamGame(t, root, "100", "Root Game")
		writeSteamGame(t, before, "200", "Ext Game")
		writeSteamGame(t, after, "200", "Ext Game") // where the disk is mounted now
		db := migratedDB(t)

		first, _ := refresh(t, db, libs(map[string]string{root: "11", before: "22"}, root, before))
		require.Equal(t, "library_2", first["200"].InstanceID)

		second, result := refresh(t, db, libs(map[string]string{root: "11", after: "22"}, root, after))

		assert.Equal(t, first["200"].ID, second["200"].ID, "the same install, not a new one")
		assert.Equal(t, "library_2", second["200"].InstanceID)
		assert.Equal(t, int64(1), second["200"].IsPresent)
		assert.Equal(t, filepath.Join(after, "steamapps", "common", "Ext Game"), second["200"].InstallRoot)
		assert.Equal(t, first["100"].ID, second["100"].ID)
		assert.Len(t, second, 2)

		moved := warningsContainingText(result.Warnings, "was found at")
		require.Len(t, moved, 1)
		assert.Contains(t, moved[0], after)
		assert.Contains(t, moved[0], before)
		assert.Contains(t, moved[0], "library_2")
	})

	t.Run("a moved library does not make the one that is new take its instance", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		path := filepath.Join(dir, "disk", "SteamLibrary") // the same path for two different disks
		elsewhere := filepath.Join(dir, "other", "SteamLibrary")
		writeSteamGame(t, root, "100", "Root Game")
		writeSteamGame(t, path, "200", "Old Disk Game")
		writeSteamGame(t, elsewhere, "200", "Old Disk Game")
		db := migratedDB(t)

		first, _ := refresh(t, db, libs(map[string]string{root: "11", path: "22"}, root, path))
		require.Equal(t, "library_2", first["200"].InstanceID)

		// the old disk is somewhere else now, and another one is at its place
		require.NoError(t, os.RemoveAll(filepath.Join(path, "steamapps")))
		writeSteamGame(t, path, "300", "New Disk Game")
		second, _ := refresh(t, db, libs(map[string]string{root: "11", path: "44", elsewhere: "22"}, root, path, elsewhere))

		assert.Equal(t, first["200"].ID, second["200"].ID)
		assert.Equal(t, "library_2", second["200"].InstanceID, "the old disk keeps it")
		assert.Equal(t, filepath.Join(elsewhere, "steamapps", "common", "Old Disk Game"), second["200"].InstallRoot)
		assert.Equal(t, "library_3", second["300"].InstanceID, "the new one has an id of its own")
	})

	t.Run("installs from before there were content ids keep their instance, and are known by it from then on", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		before := filepath.Join(dir, "disk-a", "SteamLibrary")
		after := filepath.Join(dir, "disk-b", "SteamLibrary")
		writeSteamGame(t, root, "100", "Root Game")
		writeSteamGame(t, before, "200", "Ext Game")
		writeSteamGame(t, after, "200", "Ext Game")
		db := migratedDB(t)

		legacy, _ := refresh(t, db, libs(nil, root, before))
		_, contentID := libraryOfInstall(legacy["200"].Metadata)
		require.Empty(t, contentID, "as it was before")

		upgraded, result := refresh(t, db, libs(map[string]string{root: "11", before: "22"}, root, before))

		assert.Equal(t, legacy["100"].ID, upgraded["100"].ID)
		assert.Equal(t, legacy["200"].ID, upgraded["200"].ID)
		assert.Equal(t, "default", upgraded["100"].InstanceID)
		assert.Equal(t, "library_2", upgraded["200"].InstanceID)
		_, contentID = libraryOfInstall(upgraded["200"].Metadata)
		assert.Equal(t, "22", contentID, "it is written down now")
		assert.Empty(t, warningsContainingText(result.Warnings, "was found at"), "nothing has moved")

		moved, _ := refresh(t, db, libs(map[string]string{root: "11", after: "22"}, root, after))

		assert.Equal(t, legacy["200"].ID, moved["200"].ID)
		assert.Equal(t, "library_2", moved["200"].InstanceID)
	})

	t.Run("an entry with no content id is known by its path, as it was", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		ext := filepath.Join(dir, "games")
		writeSteamGame(t, root, "100", "Root Game")
		writeSteamGame(t, ext, "200", "Ext Game")
		db := migratedDB(t)

		first, _ := refresh(t, db, libs(map[string]string{root: "11"}, root, ext))
		second, _ := refresh(t, db, libs(map[string]string{root: "11"}, root, ext))

		assert.Equal(t, first["200"].ID, second["200"].ID)
		assert.Equal(t, "library_2", second["200"].InstanceID)
		_, contentID := libraryOfInstall(second["200"].Metadata)
		assert.Empty(t, contentID)
		_, contentID = libraryOfInstall(second["100"].Metadata)
		assert.Equal(t, "11", contentID)
	})

	t.Run("libraries that share a content id are told apart by their path", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		a := filepath.Join(dir, "a-disk")
		b := filepath.Join(dir, "b-disk")
		writeSteamGame(t, root, "100", "Root Game")
		writeSteamGame(t, a, "200", "A Game")
		writeSteamGame(t, b, "300", "B Game")
		db := migratedDB(t)
		ids := map[string]string{root: "11", a: "77", b: "77"}

		first, result := refresh(t, db, libs(ids, root, a, b))
		require.Equal(t, "library_2", first["200"].InstanceID)
		require.Equal(t, "library_3", first["300"].InstanceID)
		assert.Len(t, warningsContainingText(result.Warnings, "same content id"), 0, "that is for discovery to say")

		second, _ := refresh(t, db, libs(ids, root, a, b))

		assert.Equal(t, first["200"].ID, second["200"].ID)
		assert.Equal(t, first["300"].ID, second["300"].ID)
		assert.Equal(t, "library_2", second["200"].InstanceID)
		assert.Equal(t, "library_3", second["300"].InstanceID)
	})

	t.Run("the content id is written down with the install", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		root := filepath.Join(dir, "Steam")
		writeSteamGame(t, root, "100", "Root Game")
		db := migratedDB(t)

		got, _ := refresh(t, db, libs(map[string]string{root: "-7695053421422003000"}, root))

		var meta map[string]any
		require.NoError(t, json.Unmarshal([]byte(got["100"].Metadata.String), &meta))
		assert.Equal(t, "-7695053421422003000", meta["library_content_id"])
		assert.Equal(t, root, meta["library_root"])
	})
}

func warningsContainingText(warnings []string, substr string) []string {
	var out []string
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			out = append(out, w)
		}
	}
	return out
}
