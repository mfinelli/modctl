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

package restore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/exporter"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnverifiedTarget(t *testing.T) {
	t.Parallel()

	str := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

	tests := []struct {
		name        string
		enabled     int64
		metadata    sql.NullString
		want        sql.NullString
		wantChanged bool
	}{
		{"a target that is enabled is marked", 1, sql.NullString{}, str(`{"unverified":true}`), true},
		{"what else its metadata has is kept", 1, str(`{"prefix":"x","n":2}`), str(`{"n":2,"prefix":"x","unverified":true}`), true},
		{"a mark that is already there stays one mark", 1, str(`{"unverified":true}`), str(`{"unverified":true}`), true},
		{"a mark that was false is made true", 1, str(`{"unverified":false}`), str(`{"unverified":true}`), true},
		{"metadata that is not an object is replaced", 1, str(`["x"]`), str(`{"unverified":true}`), true},
		{"metadata that is a scalar is replaced", 1, str(`3`), str(`{"unverified":true}`), true},
		{"any value that is not 0 is enabled", 5, sql.NullString{}, str(`{"unverified":true}`), true},
		{"a target that is disabled is left as it is", 0, sql.NullString{}, sql.NullString{}, false},
		{"with its metadata too", 0, str(`{"prefix":"x"}`), str(`{"prefix":"x"}`), false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, changed := unverifiedTarget(tc.enabled, tc.metadata)

			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantChanged, changed)
		})
	}
}

type targetState struct {
	Enabled  int64
	RootPath string
	Metadata sql.NullString
}

func targetStates(t *testing.T, db *sql.DB) map[string]targetState {
	t.Helper()

	rows, err := db.Query(`SELECT name, enabled, root_path, metadata FROM targets`)
	require.NoError(t, err)
	defer rows.Close()

	out := map[string]targetState{}
	for rows.Next() {
		var name string
		var s targetState
		require.NoError(t, rows.Scan(&name, &s.Enabled, &s.RootPath, &s.Metadata))
		out[name] = s
	}
	require.NoError(t, rows.Err())
	return out
}

// targetsOfABundle makes a game with targets that are each of a kind that
// matters: found by discovery, made by the user, and disabled.
func targetsOfABundle(t *testing.T, db *sql.DB) *bundleGame {
	t.Helper()

	g := newBundleGame(t, db, "1")
	g.rename("The game of the bundle")
	g.addTarget("proton_prefix", "discovered", "/other/machine/prefix", 1)
	g.addTarget("my_mods", "user_override", "/home/other/my-mods", 1)
	g.addTarget("not_wanted", "discovered", "/other/machine/not-wanted", 0)
	return g
}

// databaseDSN is how a test opens the database of a restore that it looks at.
func databaseDSN(path string) string {
	return database.DSN(path, true)
}

// target0Root is the path of the target that every game starts with.
func (g *bundleGame) target0Root(t *testing.T, db *sql.DB) string {
	t.Helper()

	var root string
	require.NoError(t, db.QueryRow(`SELECT root_path FROM targets WHERE game_install_id = ? AND name = 'game_dir'`, g.game.ID).Scan(&root))
	return root
}

func TestGameImportsTargetsDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	dest := testbuilder.SetupDB(t)
	bundleDB := testbuilder.SetupDB(t)
	g := targetsOfABundle(t, bundleDB)
	bundle := testBundle(t, bundleDB, g.game, exporter.ExportKindGame)

	res, err := Game(ctx, dest, dbq.New(dest), blobstore.Store{}, bundle, Options{Schema: testbuilder.Schema(), SkipInventory: true}, "test", quietLogger())

	require.NoError(t, err)
	got := targetStates(t, dest)
	marked := sql.NullString{String: `{"unverified":true}`, Valid: true}
	assert.Equal(t, int64(0), got["game_dir"].Enabled)
	assert.Equal(t, marked, got["game_dir"].Metadata)
	assert.Equal(t, targetState{Enabled: 0, RootPath: "/other/machine/prefix", Metadata: marked}, got["proton_prefix"],
		"the path is the one of the bundle until it is confirmed")
	assert.Equal(t, targetState{Enabled: 0, RootPath: "/home/other/my-mods", Metadata: marked}, got["my_mods"])
	assert.Equal(t, targetState{Enabled: 0, RootPath: "/other/machine/not-wanted"}, got["not_wanted"],
		"a target that was disabled is disabled, and not marked as one to enable when it is found")

	assert.ElementsMatch(t, []DisabledTarget{
		{Game: "The game of the bundle", Name: "game_dir", RootPath: g.target0Root(t, bundleDB)},
		{Game: "The game of the bundle", Name: "proton_prefix", RootPath: "/other/machine/prefix"},
		{Game: "The game of the bundle", Name: "my_mods", RootPath: "/home/other/my-mods", Custom: true},
	}, res.DisabledTargets, "it says which ones, and which of them nothing is going to confirm")
}

func TestFullImportsTargetsDisabledUnlessItIsTheSameMachine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	for _, tc := range []struct {
		name        string
		sameMachine bool
	}{
		{"another machine", false},
		{"the same machine", true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFullRestoreFixture(t)
			// the bundle has its own game with the targets that matter
			bundleDB := f.bundle.BundleDB
			g := newBundleGame(t, bundleDB, "3")
			g.rename("Another game of the bundle")
			g.addTarget("my_mods", "user_override", "/home/other/my-mods", 1)
			g.addTarget("not_wanted", "discovered", "/other/machine/not-wanted", 0)
			_, err := bundleDB.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
			require.NoError(t, err)

			res, err := Full(ctx, f.db, dbq.New(f.db), blobstore.Store{}, f.bundle, Options{Schema: testbuilder.Schema(), Force: true, SameMachine: tc.sameMachine}, f.dbPath, "test", quietLogger())

			require.NoError(t, err)
			restored, err := sql.Open("sqlite3", databaseDSN(f.dbPath))
			require.NoError(t, err)
			defer restored.Close()
			states := targetStates(t, restored)
			if tc.sameMachine {
				assert.Equal(t, int64(1), states["my_mods"].Enabled, "they are the targets of this machine")
				assert.False(t, states["my_mods"].Metadata.Valid)
				assert.Empty(t, res.DisabledTargets)
				return
			}

			marked := sql.NullString{String: `{"unverified":true}`, Valid: true}
			assert.Equal(t, targetState{Enabled: 0, RootPath: "/home/other/my-mods", Metadata: marked}, states["my_mods"])
			assert.Equal(t, targetState{Enabled: 0, RootPath: "/other/machine/not-wanted"}, states["not_wanted"])
			for name, s := range states {
				assert.Equal(t, int64(0), s.Enabled, "%s is enabled", name)
			}
			var custom []string
			for _, d := range res.DisabledTargets {
				if d.Custom {
					custom = append(custom, d.Name)
				}
				assert.NotEqual(t, "not_wanted", d.Name, "it was disabled already")
			}
			assert.Equal(t, []string{"my_mods"}, custom)
		})
	}
}
