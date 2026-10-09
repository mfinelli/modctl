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
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// profileItems is a profile with two mod versions in it.
type profileItems struct {
	db      *sql.DB
	q       *dbq.Queries
	profile dbq.Profile

	// the two items, and the mod file versions they pin
	itemA, itemB       int64
	versionA, versionB int64
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	res, err := db.Exec(query, args...)
	require.NoError(t, err, query)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

func newProfileItems(t *testing.T) *profileItems {
	t.Helper()

	db := testbuilder.SetupDB(t)
	gi := testbuilder.NewGame(t, db).Build()
	profile := testbuilder.NewProfile(t, db).WithGame(gi).Build()
	target := exec(t, db, `INSERT INTO targets (game_install_id, name, root_path) VALUES (?, 'game_dir', '/game')`, gi.ID)

	p := &profileItems{db: db, q: dbq.New(db), profile: profile}
	add := func(n int) (item, version int64) {
		sha := fmt.Sprintf("%064x", n)
		exec(t, db, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha)
		page := exec(t, db, `INSERT INTO mod_pages (game_install_id, name, source_kind) VALUES (?, ?, 'local')`, gi.ID, fmt.Sprintf("Mod %d", n))
		file := exec(t, db, `INSERT INTO mod_files (mod_page_id, label) VALUES (?, 'Main')`, page)
		version = exec(t, db, `INSERT INTO mod_file_versions (mod_file_id, archive_sha256) VALUES (?, ?)`, file, sha)
		item = exec(t, db, `INSERT INTO profile_items (profile_id, mod_file_version_id, target_id, priority) VALUES (?, ?, ?, ?)`,
			profile.ID, version, target, n)
		return item, version
	}
	p.itemA, p.versionA = add(1)
	p.itemB, p.versionB = add(2)
	return p
}

func TestSetProfileItemEnabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("reports whether anything changed", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		// the item starts out disabled
		changed, err := internal.SetProfileItemEnabled(ctx, &p.profile, p.q, p.versionA, false)
		require.NoError(t, err)
		assert.False(t, changed, "already disabled")

		changed, err = internal.SetProfileItemEnabled(ctx, &p.profile, p.q, p.versionA, true)
		require.NoError(t, err)
		assert.True(t, changed)

		changed, err = internal.SetProfileItemEnabled(ctx, &p.profile, p.q, p.versionA, true)
		require.NoError(t, err)
		assert.False(t, changed, "already enabled")

		changed, err = internal.SetProfileItemEnabled(ctx, &p.profile, p.q, p.versionA, false)
		require.NoError(t, err)
		assert.True(t, changed)
	})

	t.Run("only touches the version it is given", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		_, err := internal.SetProfileItemEnabled(ctx, &p.profile, p.q, p.versionA, true)
		require.NoError(t, err)

		var a, b int64
		require.NoError(t, p.db.QueryRow(`SELECT enabled FROM profile_items WHERE id = ?`, p.itemA).Scan(&a))
		require.NoError(t, p.db.QueryRow(`SELECT enabled FROM profile_items WHERE id = ?`, p.itemB).Scan(&b))
		assert.EqualValues(t, 1, a)
		assert.EqualValues(t, 0, b)
	})

	t.Run("a version that is not in the profile is an error", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		changed, err := internal.SetProfileItemEnabled(ctx, &p.profile, p.q, 999999, true)
		require.Error(t, err)
		assert.False(t, changed)
		assert.Contains(t, err.Error(), "is not in profile")
	})
}

func TestRemapHelpers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	stripOne := sql.NullInt64{Int64: 1, Valid: true}
	subdir := sql.NullString{String: "Data", Valid: true}

	t.Run("AddRemapRule returns the position it used", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		first, err := internal.AddRemapRule(ctx, p.db, p.q, p.itemA, "strip_components", stripOne, sql.NullString{}, -1)
		require.NoError(t, err)
		second, err := internal.AddRemapRule(ctx, p.db, p.q, p.itemA, "select_subdir", sql.NullInt64{}, subdir, -1)
		require.NoError(t, err)
		assert.Equal(t, first+1, second, "positions are assigned one after the other")

		explicit, err := internal.AddRemapRule(ctx, p.db, p.q, p.itemA, "dest_prefix", sql.NullInt64{}, sql.NullString{String: "Mods", Valid: true}, 10)
		require.NoError(t, err)
		assert.EqualValues(t, 10, explicit, "an explicit position is used as given")
	})

	t.Run("AddRemapRule rejects a rule the database refuses", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		pos, err := internal.AddRemapRule(ctx, p.db, p.q, p.itemA, "strip_components", sql.NullInt64{}, sql.NullString{}, -1)
		require.Error(t, err)
		assert.Zero(t, pos)
	})

	t.Run("ClearRemapConfig reports whether there was anything to clear", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		cleared, err := internal.ClearRemapConfig(ctx, p.db, p.q, p.itemA)
		require.NoError(t, err)
		assert.False(t, cleared, "no config yet")

		_, err = internal.AddRemapRule(ctx, p.db, p.q, p.itemA, "strip_components", stripOne, sql.NullString{}, -1)
		require.NoError(t, err)

		cleared, err = internal.ClearRemapConfig(ctx, p.db, p.q, p.itemA)
		require.NoError(t, err)
		assert.True(t, cleared)

		rules, err := p.q.ListRemapRulesForProfileItem(ctx, p.itemA)
		require.NoError(t, err)
		assert.Empty(t, rules)

		cleared, err = internal.ClearRemapConfig(ctx, p.db, p.q, p.itemA)
		require.NoError(t, err)
		assert.False(t, cleared, "already cleared")
	})

	t.Run("CopyRemapConfig returns how many rules it copied", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		copied, err := internal.CopyRemapConfig(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Zero(t, copied, "nothing to copy from an item without rules")

		_, err = internal.AddRemapRule(ctx, p.db, p.q, p.itemA, "strip_components", stripOne, sql.NullString{}, -1)
		require.NoError(t, err)
		_, err = internal.AddRemapRule(ctx, p.db, p.q, p.itemA, "select_subdir", sql.NullInt64{}, subdir, -1)
		require.NoError(t, err)

		copied, err = internal.CopyRemapConfig(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Equal(t, 2, copied)

		rules, err := p.q.ListRemapRulesForProfileItem(ctx, p.itemB)
		require.NoError(t, err)
		assert.Len(t, rules, 2)
	})
}

func TestDeployRuleCopies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	addSkip := func(t *testing.T, p *profileItems, item int64, patterns ...string) {
		t.Helper()
		for _, pat := range patterns {
			require.NoError(t, p.q.AddSkipBackupPattern(ctx, dbq.AddSkipBackupPatternParams{ProfileItemID: item, Pattern: pat}))
		}
	}
	addOnce := func(t *testing.T, p *profileItems, item int64, patterns ...string) {
		t.Helper()
		for _, pat := range patterns {
			require.NoError(t, p.q.AddWriteOncePattern(ctx, dbq.AddWriteOncePatternParams{ProfileItemID: item, Pattern: pat}))
		}
	}

	t.Run("skip-backup patterns", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		copied, err := internal.CopySkipBackupPatterns(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Zero(t, copied)

		addSkip(t, p, p.itemA, "*.ini", "*.cfg", "saves/*")
		addSkip(t, p, p.itemB, "old/*") // replaced by the copy

		copied, err = internal.CopySkipBackupPatterns(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Equal(t, 3, copied)

		got, err := p.q.ListSkipBackupPatterns(ctx, p.itemB)
		require.NoError(t, err)
		assert.Len(t, got, 3)
	})

	t.Run("write-once patterns", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		copied, err := internal.CopyWriteOncePatterns(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Zero(t, copied)

		addOnce(t, p, p.itemA, "*.cfg", "user/*")

		copied, err = internal.CopyWriteOncePatterns(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Equal(t, 2, copied)

		got, err := p.q.ListWriteOncePatterns(ctx, p.itemB)
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	t.Run("both kinds at once", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		skip, once, err := internal.CopyDeployRules(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Zero(t, skip)
		assert.Zero(t, once)

		addSkip(t, p, p.itemA, "*.ini")
		addOnce(t, p, p.itemA, "*.cfg", "user/*")

		skip, once, err = internal.CopyDeployRules(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Equal(t, 1, skip)
		assert.Equal(t, 2, once)
	})

	t.Run("a kind the source has none of is left alone on the destination", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		// the source only has skip-backup patterns; the destination has
		// write-once patterns of its own, which must survive the copy
		addSkip(t, p, p.itemA, "*.ini")
		addOnce(t, p, p.itemB, "keep-me/*", "me-too.cfg")

		skip, once, err := internal.CopyDeployRules(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Equal(t, 1, skip)
		assert.Zero(t, once)

		gotOnce, err := p.q.ListWriteOncePatterns(ctx, p.itemB)
		require.NoError(t, err)
		assert.Len(t, gotOnce, 2, "the destination's write-once patterns are untouched")
	})

	t.Run("and the same the other way round", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		addOnce(t, p, p.itemA, "*.cfg")
		addSkip(t, p, p.itemB, "keep-me/*")

		skip, once, err := internal.CopyDeployRules(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Zero(t, skip)
		assert.Equal(t, 1, once)

		gotSkip, err := p.q.ListSkipBackupPatterns(ctx, p.itemB)
		require.NoError(t, err)
		assert.Len(t, gotSkip, 1, "the destination's skip-backup patterns are untouched")
	})

	t.Run("a kind the source has replaces the destination's", func(t *testing.T) {
		t.Parallel()
		p := newProfileItems(t)

		addSkip(t, p, p.itemA, "a", "b")
		addOnce(t, p, p.itemA, "c")
		addSkip(t, p, p.itemB, "old1", "old2", "old3")
		addOnce(t, p, p.itemB, "old4")

		skip, once, err := internal.CopyDeployRules(ctx, p.db, p.q, p.itemA, p.itemB)
		require.NoError(t, err)
		assert.Equal(t, 2, skip)
		assert.Equal(t, 1, once)

		gotSkip, err := p.q.ListSkipBackupPatterns(ctx, p.itemB)
		require.NoError(t, err)
		require.Len(t, gotSkip, 2)
		gotOnce, err := p.q.ListWriteOncePatterns(ctx, p.itemB)
		require.NoError(t, err)
		require.Len(t, gotOnce, 1)
		assert.Equal(t, "c", gotOnce[0].Pattern)
	})
}
