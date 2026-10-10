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

package internal_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nexusMods is a database with a game install to put mod pages in, and
// helpers to put them there.
type nexusMods struct {
	t    *testing.T
	db   *sql.DB
	q    *dbq.Queries
	game dbq.GameInstall
	n    int // for blobs that are all different
}

func newNexusMods(t *testing.T) *nexusMods {
	t.Helper()

	db := testbuilder.SetupDB(t)
	return &nexusMods{t: t, db: db, q: dbq.New(db), game: testbuilder.NewGame(t, db).Build()}
}

// page adds a mod page to the game install.
func (m *nexusMods) page(gameID int64, name string) (pageID int64) {
	m.t.Helper()
	return exec(m.t, m.db, `INSERT INTO mod_pages (game_install_id, name, source_kind) VALUES (?, ?, 'nexus')`, gameID, name)
}

// file adds a mod file to a mod page.
func (m *nexusMods) file(pageID int64, label string) (fileID int64) {
	m.t.Helper()
	return exec(m.t, m.db, `INSERT INTO mod_files (mod_page_id, label) VALUES (?, ?)`, pageID, label)
}

// version adds a version, imported at the given time, to a mod file. A
// nexusFileID of 0 means that it is not linked to a file on Nexus.
func (m *nexusMods) version(fileID int64, nexusFileID int64, importedAt string) (versionID int64) {
	m.t.Helper()

	m.n++
	sha := fmt.Sprintf("%064x", m.n)
	exec(m.t, m.db, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha)

	var nexus sql.NullInt64
	if nexusFileID != 0 {
		nexus = sql.NullInt64{Int64: nexusFileID, Valid: true}
	}
	return exec(m.t, m.db,
		`INSERT INTO mod_file_versions (mod_file_id, archive_sha256, nexus_file_id, created_at) VALUES (?, ?, ?, ?)`,
		fileID, sha, nexus, importedAt)
}

func TestListImportedNexusFileIDsByGameInstall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("a game install with no mods has none", func(t *testing.T) {
		t.Parallel()
		m := newNexusMods(t)

		rows, err := m.q.ListImportedNexusFileIDsByGameInstall(ctx, m.game.ID)
		require.NoError(t, err)
		assert.Empty(t, rows)
	})

	t.Run("lists the nexus file of every version that has one, with its mod page", func(t *testing.T) {
		t.Parallel()
		m := newNexusMods(t)

		page := m.page(m.game.ID, "Appearance Menu Mod")
		main := m.file(page, "Main")
		m.version(main, 101, "2026-01-01T00:00:00.000Z")
		m.version(main, 102, "2026-02-01T00:00:00.000Z")
		optional := m.file(page, "Optional")
		m.version(optional, 201, "2026-01-15T00:00:00.000Z")

		rows, err := m.q.ListImportedNexusFileIDsByGameInstall(ctx, m.game.ID)
		require.NoError(t, err)

		got := map[int64]int64{}
		for _, r := range rows {
			require.True(t, r.NexusFileID.Valid)
			got[r.NexusFileID.Int64] = r.ModPageID
		}
		assert.Equal(t, map[int64]int64{101: page, 102: page, 201: page}, got)
		assert.Len(t, rows, 3)
	})

	t.Run("versions that are not linked to nexus are left out", func(t *testing.T) {
		t.Parallel()
		m := newNexusMods(t)

		page := m.page(m.game.ID, "Some Mod")
		file := m.file(page, "Main")
		m.version(file, 0, "2026-01-01T00:00:00.000Z")
		m.version(file, 101, "2026-02-01T00:00:00.000Z")

		rows, err := m.q.ListImportedNexusFileIDsByGameInstall(ctx, m.game.ID)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, int64(101), rows[0].NexusFileID.Int64)
	})

	t.Run("mods of different pages keep their own page", func(t *testing.T) {
		t.Parallel()
		m := newNexusMods(t)

		pageA := m.page(m.game.ID, "Mod A")
		pageB := m.page(m.game.ID, "Mod B")
		m.version(m.file(pageA, "Main"), 101, "2026-01-01T00:00:00.000Z")
		m.version(m.file(pageB, "Main"), 301, "2026-01-01T00:00:00.000Z")

		rows, err := m.q.ListImportedNexusFileIDsByGameInstall(ctx, m.game.ID)
		require.NoError(t, err)

		got := map[int64]int64{}
		for _, r := range rows {
			got[r.NexusFileID.Int64] = r.ModPageID
		}
		assert.Equal(t, map[int64]int64{101: pageA, 301: pageB}, got)
	})

	t.Run("mods of another game install are not included", func(t *testing.T) {
		t.Parallel()
		m := newNexusMods(t)

		other := testbuilder.NewGame(t, m.db).WithStoreGameID("999").Build()
		m.version(m.file(m.page(m.game.ID, "Mine"), "Main"), 101, "2026-01-01T00:00:00.000Z")
		m.version(m.file(m.page(other.ID, "Theirs"), "Main"), 901, "2026-01-01T00:00:00.000Z")

		rows, err := m.q.ListImportedNexusFileIDsByGameInstall(ctx, m.game.ID)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, int64(101), rows[0].NexusFileID.Int64)
	})
}

func TestListModsByGameInstallNexusFileID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	byName := func(t *testing.T, m *nexusMods) map[string]dbq.ListModsByGameInstallRow {
		t.Helper()

		rows, err := m.q.ListModsByGameInstall(ctx, m.game.ID)
		require.NoError(t, err)

		out := map[string]dbq.ListModsByGameInstallRow{}
		for _, r := range rows {
			out[r.ModName] = r
		}
		return out
	}

	t.Run("is the nexus file of the version that was imported last", func(t *testing.T) {
		t.Parallel()
		m := newNexusMods(t)

		page := m.page(m.game.ID, "Appearance Menu Mod")
		file := m.file(page, "Main")
		newest := m.version(file, 102, "2026-02-01T00:00:00.000Z")
		m.version(file, 101, "2026-01-01T00:00:00.000Z")

		row := byName(t, m)["Appearance Menu Mod"]
		assert.Equal(t, sql.NullInt64{Int64: newest, Valid: true}, row.ModFileVersionID)
		assert.Equal(t, sql.NullInt64{Int64: 102, Valid: true}, row.NexusFileID)
	})

	t.Run("follows the version that was imported last across files", func(t *testing.T) {
		t.Parallel()
		m := newNexusMods(t)

		page := m.page(m.game.ID, "Two Files")
		m.version(m.file(page, "Main"), 101, "2026-03-01T00:00:00.000Z")
		m.version(m.file(page, "Optional"), 201, "2026-01-01T00:00:00.000Z")

		row := byName(t, m)["Two Files"]
		assert.Equal(t, sql.NullInt64{Int64: 101, Valid: true}, row.NexusFileID)
		assert.Equal(t, int64(2), row.VersionsCount)
	})

	t.Run("is empty when that version is not linked to nexus", func(t *testing.T) {
		t.Parallel()
		m := newNexusMods(t)

		page := m.page(m.game.ID, "Local Version")
		file := m.file(page, "Main")
		m.version(file, 101, "2026-01-01T00:00:00.000Z")
		m.version(file, 0, "2026-02-01T00:00:00.000Z")

		row := byName(t, m)["Local Version"]
		assert.False(t, row.NexusFileID.Valid)
	})

	t.Run("is empty for a mod page that has no versions", func(t *testing.T) {
		t.Parallel()
		m := newNexusMods(t)

		m.page(m.game.ID, "Empty")

		row, ok := byName(t, m)["Empty"]
		require.True(t, ok, "the page is still listed")
		assert.False(t, row.NexusFileID.Valid)
		assert.False(t, row.ModFileVersionID.Valid)
	})
}
