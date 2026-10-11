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

package argresolver

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var modSeq atomic.Int64

// modFixture is a game with mods in it, which is what the resolvers of mod pages
// and versions are for.
type modFixture struct {
	t    *testing.T
	db   *sql.DB
	q    *dbq.Queries
	game dbq.GameInstall
}

func newModFixture(t *testing.T) modFixture {
	t.Helper()

	db := testbuilder.SetupDB(t)
	return modFixture{t: t, db: db, q: dbq.New(db), game: testbuilder.NewGame(t, db).Build()}
}

func (f modFixture) exec(query string, args ...any) int64 {
	f.t.Helper()

	res, err := f.db.Exec(query, args...)
	require.NoError(f.t, err, query)
	id, err := res.LastInsertId()
	require.NoError(f.t, err)
	return id
}

// page adds a mod page to the game, and a file to it.
func (f modFixture) page(game dbq.GameInstall, name, sourceKind string) (pageID, fileID int64) {
	f.t.Helper()

	pageID = f.exec(`INSERT INTO mod_pages (game_install_id, name, source_kind) VALUES (?, ?, ?)`, game.ID, name, sourceKind)
	fileID = f.exec(`INSERT INTO mod_files (mod_page_id, label) VALUES (?, 'Main')`, pageID)
	return pageID, fileID
}

// version adds a version of a mod file ("" for one that has none).
func (f modFixture) version(fileID int64, versionString string) int64 {
	f.t.Helper()

	sha := fmt.Sprintf("%064x", modSeq.Add(1))
	f.exec(`INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha)
	var v any
	if versionString != "" {
		v = versionString
	}
	return f.exec(`INSERT INTO mod_file_versions (mod_file_id, archive_sha256, version_string) VALUES (?, ?, ?)`, fileID, sha, v)
}

// item puts a version in a profile, and returns the id of the item.
func (f modFixture) item(profile dbq.Profile, versionID int64, priority int64, enabled bool) int64 {
	f.t.Helper()

	target, err := f.q.UpsertDiscoveredTarget(context.Background(), dbq.UpsertDiscoveredTargetParams{
		GameInstallID: profile.GameInstallID, Name: "game_dir", RootPath: f.t.TempDir(),
	})
	require.NoError(f.t, err)
	on := 0
	if enabled {
		on = 1
	}
	return f.exec(`INSERT INTO profile_items (profile_id, mod_file_version_id, target_id, priority, enabled) VALUES (?, ?, ?, ?, ?)`,
		profile.ID, versionID, target.ID, priority, on)
}

func TestResolveModFileVersionArg(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("a number is the id of a version", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		_, file := f.page(f.game, "Big Mod", "local")
		id := f.version(file, "1.2")

		got, err := ResolveModFileVersionArg(ctx, f.q, f.game, fmt.Sprint(id))

		require.NoError(t, err)
		assert.Equal(t, id, got.ID)
		assert.Equal(t, "Big Mod", got.ModPageName)
		assert.Equal(t, "Main", got.FileLabel)
		assert.Equal(t, sql.NullString{String: "1.2", Valid: true}, got.VersionString)
		assert.NotEmpty(t, got.ArchiveSha256)
	})

	t.Run("a number that is not a version of the game is an error that says so", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)

		_, err := ResolveModFileVersionArg(ctx, f.q, f.game, "424242")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no mod file version with id 424242")
		assert.Contains(t, err.Error(), fmt.Sprintf("%q", f.game.DisplayName))
	})

	t.Run("a version of another game is not found by its number", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		other := testbuilder.NewGame(t, f.db).WithStoreGameID("2").Build()
		_, file := f.page(other, "Theirs", "local")
		id := f.version(file, "1.0")

		_, err := ResolveModFileVersionArg(ctx, f.q, f.game, fmt.Sprint(id))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no mod file version with id")
	})

	t.Run("a name is the mod page, and it is one version", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		_, file := f.page(f.game, "Big Mod", "local")
		id := f.version(file, "")

		got, err := ResolveModFileVersionArg(ctx, f.q, f.game, "Big Mod")

		require.NoError(t, err)
		assert.Equal(t, id, got.ID)
		assert.Equal(t, "Big Mod", got.ModPageName)
		assert.Equal(t, "Main", got.FileLabel)
		assert.False(t, got.VersionString.Valid)
	})

	t.Run("a name is matched without regard to case", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		_, file := f.page(f.game, "Big Mod", "local")
		id := f.version(file, "1.0")

		got, err := ResolveModFileVersionArg(ctx, f.q, f.game, "bIg mOd")

		require.NoError(t, err)
		assert.Equal(t, id, got.ID)
	})

	t.Run("a name that is not a mod page is an error that says so", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)

		_, err := ResolveModFileVersionArg(ctx, f.q, f.game, "Nothing")

		require.Error(t, err)
		assert.Contains(t, err.Error(), `no mod file versions found for "Nothing"`)
	})

	t.Run("a mod page of another game is not found by its name", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		other := testbuilder.NewGame(t, f.db).WithStoreGameID("2").Build()
		_, file := f.page(other, "Theirs", "local")
		f.version(file, "1.0")

		_, err := ResolveModFileVersionArg(ctx, f.q, f.game, "Theirs")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no mod file versions found")
	})

	t.Run("a name with several versions lists them with their numbers", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		_, file := f.page(f.game, "Big Mod", "local")
		v1 := f.version(file, "1.0")
		v2 := f.version(file, "")

		_, err := ResolveModFileVersionArg(ctx, f.q, f.game, "Big Mod")

		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, `Multiple mod file versions found for "Big Mod". Specify a numeric ID:`)
		assert.Contains(t, msg, fmt.Sprintf("  %-6d  Big Mod › Main (1.0)", v1))
		assert.Contains(t, msg, fmt.Sprintf("  %-6d  Big Mod › Main (no version)", v2))
		assert.NotContains(t, msg, "\n\n\n", "no blank line at the end")
	})

	t.Run("a name that two mod pages have lists the versions of both", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		_, a := f.page(f.game, "Same Name", "local")
		_, b := f.page(f.game, "Same Name", "nexus")
		va := f.version(a, "1")
		vb := f.version(b, "2")

		_, err := ResolveModFileVersionArg(ctx, f.q, f.game, "Same Name")

		require.Error(t, err)
		assert.Contains(t, err.Error(), fmt.Sprint(va))
		assert.Contains(t, err.Error(), fmt.Sprint(vb))
	})

	t.Run("something that starts like a number is a name", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		_, file := f.page(f.game, "2077 Redux", "local")
		id := f.version(file, "1.0")

		got, err := ResolveModFileVersionArg(ctx, f.q, f.game, "2077 Redux")

		require.NoError(t, err)
		assert.Equal(t, id, got.ID)
	})
}

func TestResolveModPageArg(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("a number is the id of a mod page", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		page, _ := f.page(f.game, "Big Mod", "nexus")

		got, err := ResolveModPageArg(ctx, f.q, f.game, fmt.Sprint(page))

		require.NoError(t, err)
		assert.Equal(t, page, got.ID)
		assert.Equal(t, "Big Mod", got.Name)
		assert.Equal(t, "nexus", got.SourceKind)
	})

	t.Run("a number that is not a mod page of the game is an error that says so", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		other := testbuilder.NewGame(t, f.db).WithStoreGameID("2").Build()
		theirs, _ := f.page(other, "Theirs", "local")

		for _, arg := range []string{"424242", fmt.Sprint(theirs)} {
			_, err := ResolveModPageArg(ctx, f.q, f.game, arg)

			require.Error(t, err, arg)
			assert.Contains(t, err.Error(), "no mod page with id "+arg)
			assert.Contains(t, err.Error(), fmt.Sprintf("%q", f.game.DisplayName))
		}
	})

	t.Run("a name is the mod page, without regard to case", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		page, _ := f.page(f.game, "Big Mod", "local")

		got, err := ResolveModPageArg(ctx, f.q, f.game, "big MOD")

		require.NoError(t, err)
		assert.Equal(t, page, got.ID)
		assert.Equal(t, "Big Mod", got.Name)
		assert.Equal(t, "local", got.SourceKind)
	})

	t.Run("a name that is not a mod page is an error that says so", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)

		_, err := ResolveModPageArg(ctx, f.q, f.game, "Nothing")

		require.Error(t, err)
		assert.Contains(t, err.Error(), `no mod page found for "Nothing"`)
	})

	t.Run("a name that two mod pages have lists them with their numbers and where they are from", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		a, _ := f.page(f.game, "Same Name", "local")
		b, _ := f.page(f.game, "Same Name", "nexus")

		_, err := ResolveModPageArg(ctx, f.q, f.game, "Same Name")

		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, `Multiple mod pages found for "Same Name". Specify a numeric ID:`)
		assert.Contains(t, msg, fmt.Sprintf("  %-6d  Same Name (local)", a))
		assert.Contains(t, msg, fmt.Sprintf("  %-6d  Same Name (nexus)", b))
	})
}

func TestResolveEnabledProfileVersionArg(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// a profile with a mod that has three versions, "1.0" and "2.0" in it
	setup := func(t *testing.T) (f modFixture, profile dbq.Profile, v1, v2, v3 int64) {
		f = newModFixture(t)
		profile = testbuilder.NewProfile(t, f.db).WithGame(f.game).WithName("Mine").Build()
		_, file := f.page(f.game, "Big Mod", "local")
		v1, v2, v3 = f.version(file, "1.0"), f.version(file, "2.0"), f.version(file, "3.0")
		return f, profile, v1, v2, v3
	}

	t.Run("a number is the version, which is not looked up in the profile", func(t *testing.T) {
		t.Parallel()
		f, profile, _, _, v3 := setup(t)

		got, err := ResolveEnabledProfileVersionArg(ctx, f.q, f.game, profile, fmt.Sprint(v3))

		require.NoError(t, err)
		assert.Equal(t, v3, got)
	})

	t.Run("a number that is not a version is an error that says so", func(t *testing.T) {
		t.Parallel()
		f, profile, _, _, _ := setup(t)

		_, err := ResolveEnabledProfileVersionArg(ctx, f.q, f.game, profile, "424242")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no mod file version with id 424242")
	})

	t.Run("a name with one version that is enabled is that one, though the mod has more", func(t *testing.T) {
		t.Parallel()

		// whichever place it has among the versions that are in the profile
		for name, enabledFirst := range map[string]bool{"it comes first": true, "it comes last": false} {
			enabledFirst := enabledFirst
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				f, profile, v1, v2, _ := setup(t)
				if enabledFirst {
					f.item(profile, v1, 1, true)
					f.item(profile, v2, 2, false)
				} else {
					f.item(profile, v1, 1, false)
					f.item(profile, v2, 2, true)
				}
				want := v2
				if enabledFirst {
					want = v1
				}

				got, err := ResolveEnabledProfileVersionArg(ctx, f.q, f.game, profile, "Big Mod")

				require.NoError(t, err)
				assert.Equal(t, want, got)
			})
		}
	})

	t.Run("a name with several versions that are enabled lists only those", func(t *testing.T) {
		t.Parallel()
		f, profile, v1, v2, v3 := setup(t)
		f.item(profile, v1, 1, true)
		f.item(profile, v2, 2, true)
		f.item(profile, v3, 3, false)

		_, err := ResolveEnabledProfileVersionArg(ctx, f.q, f.game, profile, "Big Mod")

		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, `Multiple enabled versions of "Big Mod" in profile "Mine". Specify a numeric ID:`)
		assert.Contains(t, msg, fmt.Sprintf("  %-6d  Big Mod › Main (1.0)", v1))
		assert.Contains(t, msg, fmt.Sprintf("  %-6d  Big Mod › Main (2.0)", v2))
		assert.NotContains(t, msg, "(3.0)", "the one that is disabled is not an answer")
	})

	t.Run("a name with none enabled is for the usual resolver to say", func(t *testing.T) {
		t.Parallel()
		f, profile, v1, v2, _ := setup(t)
		f.item(profile, v1, 1, false)
		f.item(profile, v2, 2, false)

		_, err := ResolveEnabledProfileVersionArg(ctx, f.q, f.game, profile, "Big Mod")

		require.Error(t, err)
		assert.Contains(t, err.Error(), `Multiple mod file versions found for "Big Mod"`,
			"all three versions are candidates, as they are for any command")
	})

	t.Run("a mod that has one version and is not enabled is that version", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		profile := testbuilder.NewProfile(t, f.db).WithGame(f.game).WithName("Mine").Build()
		_, file := f.page(f.game, "Small Mod", "local")
		only := f.version(file, "1.0")
		f.item(profile, only, 1, false)

		got, err := ResolveEnabledProfileVersionArg(ctx, f.q, f.game, profile, "Small Mod")

		require.NoError(t, err)
		assert.Equal(t, only, got)
	})

	t.Run("what is enabled in another profile does not count", func(t *testing.T) {
		t.Parallel()
		f, profile, v1, v2, _ := setup(t)
		other := testbuilder.NewProfile(t, f.db).WithGame(f.game).WithName("Other").Build()
		f.item(other, v1, 1, true)
		f.item(other, v2, 2, true)

		_, err := ResolveEnabledProfileVersionArg(ctx, f.q, f.game, profile, "Big Mod")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "Multiple mod file versions found", "not the list of the enabled ones of the profile")
		assert.NotContains(t, err.Error(), "Multiple enabled versions")
	})
}
