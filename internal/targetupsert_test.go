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
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A target that was imported from a bundle is disabled and has "unverified" in
// its metadata, and finding the target when discovering what is installed is
// what confirms it.

type targetRow struct {
	Enabled  int64
	RootPath string
	Origin   string
	Metadata sql.NullString
}

func insertTarget(t *testing.T, db *sql.DB, gameID int64, name, origin, root string, enabled int, metadata any) int64 {
	t.Helper()

	res, err := db.Exec(`INSERT INTO targets (game_install_id, name, root_path, origin, metadata, created_at, updated_at, enabled)
		VALUES (?, ?, ?, ?, ?, '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z', ?)`,
		gameID, name, root, origin, metadata, enabled)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

func readTarget(t *testing.T, db *sql.DB, id int64) targetRow {
	t.Helper()

	var r targetRow
	require.NoError(t, db.QueryRow(`SELECT enabled, root_path, origin, metadata FROM targets WHERE id = ?`, id).
		Scan(&r.Enabled, &r.RootPath, &r.Origin, &r.Metadata))
	return r
}

func TestUpsertDiscoveredTargetConfirmsAnImportedTarget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const local = "/local/game"

	tests := []struct {
		name         string
		origin       string
		enabled      int
		metadata     any
		wantEnabled  int64
		wantRoot     string
		wantMetadata sql.NullString
	}{
		{
			name:   "an imported target is enabled and its mark is taken off",
			origin: "discovered", enabled: 0, metadata: `{"unverified":true}`,
			wantEnabled: 1, wantRoot: local, wantMetadata: sql.NullString{},
		},
		{
			name:   "what else the metadata has is kept",
			origin: "discovered", enabled: 0, metadata: `{"unverified":true,"prefix":"x"}`,
			wantEnabled: 1, wantRoot: local, wantMetadata: sql.NullString{String: `{"prefix":"x"}`, Valid: true},
		},
		{
			name:   "a target that was disabled by the user stays disabled",
			origin: "discovered", enabled: 0, metadata: nil,
			wantEnabled: 0, wantRoot: local, wantMetadata: sql.NullString{},
		},
		{
			name:   "also with metadata of its own",
			origin: "discovered", enabled: 0, metadata: `{"prefix":"x"}`,
			wantEnabled: 0, wantRoot: local, wantMetadata: sql.NullString{String: `{"prefix":"x"}`, Valid: true},
		},
		{
			name:   "a mark that is false is not a mark",
			origin: "discovered", enabled: 0, metadata: `{"unverified":false}`,
			wantEnabled: 0, wantRoot: local, wantMetadata: sql.NullString{String: `{"unverified":false}`, Valid: true},
		},
		{
			name:   "a mark that is not true is not a mark",
			origin: "discovered", enabled: 0, metadata: `{"unverified":"yes"}`,
			wantEnabled: 0, wantRoot: local, wantMetadata: sql.NullString{String: `{"unverified":"yes"}`, Valid: true},
		},
		{
			name:   "metadata that is not an object has no mark",
			origin: "discovered", enabled: 0, metadata: `["unverified"]`,
			wantEnabled: 0, wantRoot: local, wantMetadata: sql.NullString{String: `["unverified"]`, Valid: true},
		},
		{
			name:   "a target that is enabled is enabled, with the mark or without it",
			origin: "discovered", enabled: 1, metadata: `{"unverified":true}`,
			wantEnabled: 1, wantRoot: local, wantMetadata: sql.NullString{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := testbuilder.SetupDB(t)
			game := testbuilder.NewGame(t, db).Build()
			id := insertTarget(t, db, game.ID, "game_dir", tc.origin, "/other/machine/game", tc.enabled, tc.metadata)

			got, err := dbq.New(db).UpsertDiscoveredTarget(ctx, dbq.UpsertDiscoveredTargetParams{
				GameInstallID: game.ID, Name: "game_dir", RootPath: local,
			})

			require.NoError(t, err)
			assert.Equal(t, id, got.ID, "it is the same target")
			assert.Equal(t, targetRow{Enabled: tc.wantEnabled, RootPath: tc.wantRoot, Origin: tc.origin, Metadata: tc.wantMetadata},
				readTarget(t, db, id))
			assert.Equal(t, tc.wantEnabled, got.Enabled, "and what comes back is how it is now")
		})
	}

	t.Run("a target of the user is not touched, mark or not", func(t *testing.T) {
		t.Parallel()

		db := testbuilder.SetupDB(t)
		game := testbuilder.NewGame(t, db).Build()
		id := insertTarget(t, db, game.ID, "game_dir", "user_override", "/custom/dir", 0, `{"unverified":true}`)

		_, err := dbq.New(db).UpsertDiscoveredTarget(ctx, dbq.UpsertDiscoveredTargetParams{
			GameInstallID: game.ID, Name: "game_dir", RootPath: local,
		})

		require.ErrorIs(t, err, sql.ErrNoRows, "the conflict is ignored")
		assert.Equal(t, targetRow{Enabled: 0, RootPath: "/custom/dir", Origin: "user_override",
			Metadata: sql.NullString{String: `{"unverified":true}`, Valid: true}}, readTarget(t, db, id))
	})

	t.Run("a target that is found for the first time is enabled", func(t *testing.T) {
		t.Parallel()

		db := testbuilder.SetupDB(t)
		game := testbuilder.NewGame(t, db).Build()

		got, err := dbq.New(db).UpsertDiscoveredTarget(ctx, dbq.UpsertDiscoveredTargetParams{
			GameInstallID: game.ID, Name: "proton_prefix", RootPath: local,
		})

		require.NoError(t, err)
		assert.Equal(t, int64(1), got.Enabled)
		assert.False(t, got.Metadata.Valid)
	})

	t.Run("only the target with that name is confirmed", func(t *testing.T) {
		t.Parallel()

		db := testbuilder.SetupDB(t)
		game := testbuilder.NewGame(t, db).Build()
		confirmed := insertTarget(t, db, game.ID, "game_dir", "discovered", "/other/game", 0, `{"unverified":true}`)
		other := insertTarget(t, db, game.ID, "proton_prefix", "discovered", "/other/prefix", 0, `{"unverified":true}`)

		_, err := dbq.New(db).UpsertDiscoveredTarget(ctx, dbq.UpsertDiscoveredTargetParams{
			GameInstallID: game.ID, Name: "game_dir", RootPath: local,
		})

		require.NoError(t, err)
		assert.Equal(t, int64(1), readTarget(t, db, confirmed).Enabled)
		assert.Equal(t, targetRow{Enabled: 0, RootPath: "/other/prefix", Origin: "discovered",
			Metadata: sql.NullString{String: `{"unverified":true}`, Valid: true}}, readTarget(t, db, other),
			"what was not found is still not trusted")
	})
}
