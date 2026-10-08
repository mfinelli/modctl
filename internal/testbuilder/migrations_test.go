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

package testbuilder

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mfinelli/modctl/internal"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMigrationDB opens an empty file database with the production pragmas and
// returns it with a provider for the repo's migrations.
func newMigrationDB(t *testing.T) (*sql.DB, *goose.Provider) {
	t.Helper()

	db, err := sql.Open("sqlite3", internal.DSN(filepath.Join(t.TempDir(), "modctl.db"), false))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return db, NewProvider(t, db)
}

var (
	sqlLineComment = regexp.MustCompile(`--[^\n]*`)
	sqlSpaces      = regexp.MustCompile(`\s+`)
)

// normalizeSQL makes stored schema SQL comparable: comments, whitespace and
// identifier quoting (which ALTER TABLE ... RENAME adds) don't matter.
func normalizeSQL(s string) string {
	s = sqlLineComment.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, `"`, "")
	s = sqlSpaces.ReplaceAllString(s, " ")
	s = strings.NewReplacer(" (", "(", "( ", "(", " )", ")", ", ", ",", " ,", ",").Replace(s)
	return strings.TrimSpace(s)
}

// schemaSnapshot describes every table, column, foreign key, index and
// trigger of the database in a stable, comparable form.
func schemaSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()

	query := func(q string, args ...any) [][]string {
		rows, err := db.Query(q, args...)
		require.NoError(t, err, q)
		defer rows.Close()

		cols, err := rows.Columns()
		require.NoError(t, err)

		var out [][]string
		for rows.Next() {
			vals := make([]sql.NullString, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			require.NoError(t, rows.Scan(ptrs...))
			row := make([]string, len(cols))
			for i, v := range vals {
				row[i] = v.String
			}
			out = append(out, row)
		}
		require.NoError(t, rows.Err())
		return out
	}

	var lines []string

	objects := query(`SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_master
		WHERE name NOT LIKE 'sqlite_%' AND name != 'schema_migrations'
		ORDER BY type, name`)
	for _, o := range objects {
		typ, name, table, stmt := o[0], o[1], o[2], o[3]
		lines = append(lines, fmt.Sprintf("%s %s on %s: %s", typ, name, table, normalizeSQL(stmt)))

		if typ != "table" {
			continue
		}

		for _, c := range query(fmt.Sprintf(`SELECT name, type, "notnull", dflt_value, pk, hidden FROM pragma_table_xinfo('%s')`, name)) {
			lines = append(lines, fmt.Sprintf("  column %s", strings.Join(c, "|")))
		}

		var fks []string
		for _, f := range query(fmt.Sprintf(`SELECT "table", "from", "to", on_update, on_delete FROM pragma_foreign_key_list('%s')`, name)) {
			fks = append(fks, fmt.Sprintf("  fk %s", strings.Join(f, "|")))
		}
		sort.Strings(fks)
		lines = append(lines, fks...)

		// implicit indexes (UNIQUE constraints) have generated names, so
		// describe them by what they cover
		var idx []string
		for _, i := range query(fmt.Sprintf(`SELECT name, "unique", origin FROM pragma_index_list('%s')`, name)) {
			cols := query(fmt.Sprintf(`SELECT name FROM pragma_index_info('%s') ORDER BY seqno`, i[0]))
			var names []string
			for _, c := range cols {
				names = append(names, c[0])
			}
			idx = append(idx, fmt.Sprintf("  index unique=%s origin=%s (%s)", i[1], i[2], strings.Join(names, ",")))
		}
		sort.Strings(idx)
		lines = append(lines, idx...)
	}

	return strings.Join(lines, "\n")
}

// snapshotDiff lists the lines that are only in want ("-") or only in got
// ("+"), so a failure shows what differs instead of two whole schemas.
func snapshotDiff(want, got string) string {
	count := func(s string) map[string]int {
		m := map[string]int{}
		if s != "" {
			for _, l := range strings.Split(s, "\n") {
				m[l]++
			}
		}
		return m
	}
	w, g := count(want), count(got)

	var out []string
	for l, n := range w {
		if g[l] < n {
			out = append(out, "- "+l)
		}
	}
	for l, n := range g {
		if w[l] < n {
			out = append(out, "+ "+l)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// TestMigrationsRoundTrip checks that every Down migration really undoes its
// Up: rolling a fully migrated database back to any version must leave
// exactly the schema that migrating a fresh database up to that version
// produces, and migrating forward again must give back the latest schema.
func TestMigrationsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, probe := newMigrationDB(t)
	_, latest, err := probe.GetVersions(ctx)
	require.NoError(t, err)
	require.Greater(t, latest, int64(1))

	// the schema at each version, straight from the Up migrations
	want := map[int64]string{0: ""}
	for v := int64(1); v <= latest; v++ {
		db, p := newMigrationDB(t)
		_, err := p.UpTo(ctx, v)
		require.NoError(t, err, "up to %d", v)
		want[v] = schemaSnapshot(t, db)
	}

	for v := int64(0); v < latest; v++ {
		t.Run(fmt.Sprintf("down to %d and back", v), func(t *testing.T) {
			t.Parallel()

			db, p := newMigrationDB(t)
			_, err := p.Up(ctx)
			require.NoError(t, err)

			_, err = p.DownTo(ctx, v)
			require.NoError(t, err, "down to %d", v)

			current, _, err := p.GetVersions(ctx)
			require.NoError(t, err)
			assert.Equal(t, v, current)
			if d := snapshotDiff(want[v], schemaSnapshot(t, db)); d != "" {
				t.Errorf("schema after rolling back to %d differs from migrating up to it:\n%s", v, d)
			}

			_, err = p.Up(ctx)
			require.NoError(t, err, "up again from %d", v)
			if d := snapshotDiff(want[latest], schemaSnapshot(t, db)); d != "" {
				t.Errorf("schema after migrating forward again from %d differs from the latest:\n%s", v, d)
			}
		})
	}
}

// seedMigrationData inserts a small but representative set of rows into a
// fully migrated database: every major table gets at least one row.
func seedMigrationData(t *testing.T, db *sql.DB) {
	t.Helper()

	archive := fmt.Sprintf("%064x", 1)
	backup := fmt.Sprintf("%064x", 2)
	override := fmt.Sprintf("%064x", 3)

	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO blobs (sha256, kind, size_bytes, original_name) VALUES (?, 'archive', 10, 'a.zip'), (?, 'backup', 5, NULL), (?, 'override', 7, NULL)`, []any{archive, backup, override}},
		{`INSERT INTO game_installs (id, store_id, store_game_id, display_name, install_root) VALUES (1, 'steam', '100', 'Game', '/game')`, nil},
		{`INSERT INTO targets (id, game_install_id, name, root_path) VALUES (1, 1, 'game_dir', '/game'), (2, 1, 'proton_prefix', '/prefix')`, nil},
		{`INSERT INTO profiles (id, game_install_id, name) VALUES (1, 1, 'default')`, nil},
		{`INSERT INTO mod_pages (id, game_install_id, name, source_kind) VALUES (1, 1, 'Mod', 'local')`, nil},
		{`INSERT INTO mod_files (id, mod_page_id, label) VALUES (1, 1, 'Main')`, nil},
		{`INSERT INTO mod_file_versions (id, mod_file_id, archive_sha256) VALUES (1, 1, ?)`, []any{archive}},
		{`INSERT INTO profile_items (id, profile_id, mod_file_version_id, target_id, priority) VALUES (1, 1, 1, 1, 1)`, nil},
		{`INSERT INTO archive_inventory_entries (archive_sha256, raw_path, entry_type, size_bytes, position) VALUES (?, 'a.txt', 'file', 3, 0)`, []any{archive}},
		{`INSERT INTO overrides (id, profile_id, target_id, relpath, blob_sha256, override_type) VALUES (1, 1, 1, 'o.ini', ?, 'full_file')`, []any{override}},
		{`INSERT INTO operations (id, game_install_id, op_type, status) VALUES (1, 1, 'apply', 'success')`, nil},
		{`INSERT INTO installed_files (game_install_id, target_id, relpath, content_sha256, size_bytes, owner_mod_file_version_id, last_operation_id) VALUES (1, 1, 'a.txt', ?, 3, 1, 1)`, []any{archive}},
		{`INSERT INTO backups (game_install_id, target_id, relpath, backup_blob_sha256, size_bytes) VALUES (1, 1, 'a.txt', ?, 5)`, []any{backup}},
		{`INSERT INTO operation_changes (operation_id, game_install_id, target_id, relpath, action) VALUES (1, 1, 1, 'a.txt', 'write')`, nil},
	} {
		_, err := db.Exec(q.sql, q.args...)
		require.NoError(t, err, q.sql)
	}
}

// rowCounts returns the number of rows in every table of the database.
func rowCounts(t *testing.T, db *sql.DB) map[string]int64 {
	t.Helper()

	rows, err := db.Query(`SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'schema_migrations'`)
	require.NoError(t, err)
	var names []string
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		names = append(names, n)
	}
	require.NoError(t, rows.Err())
	rows.Close()

	counts := map[string]int64{}
	for _, n := range names {
		var c int64
		require.NoError(t, db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, n)).Scan(&c))
		counts[n] = c
	}
	return counts
}

// TestMigrationsDownWithData rolls a database that holds data back to every
// version and forward again: the Down migrations must cope with rows being
// present, the rows of every table that survives the rollback must survive
// the round trip, and no foreign key may be left dangling.
func TestMigrationsDownWithData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, probe := newMigrationDB(t)
	_, latest, err := probe.GetVersions(ctx)
	require.NoError(t, err)

	for v := int64(0); v < latest; v++ {
		t.Run(fmt.Sprintf("down to %d and back", v), func(t *testing.T) {
			t.Parallel()

			db, p := newMigrationDB(t)
			_, err := p.Up(ctx)
			require.NoError(t, err)
			seedMigrationData(t, db)
			before := rowCounts(t, db)

			_, err = p.DownTo(ctx, v)
			require.NoError(t, err, "down to %d with data present", v)
			survivors := rowCounts(t, db)

			_, err = p.Up(ctx)
			require.NoError(t, err, "up again from %d", v)
			after := rowCounts(t, db)

			// a table that existed at version v keeps its rows through the round trip
			for table := range survivors {
				assert.Equal(t, before[table], after[table], "rows in %s after rolling back to %d and forward again", table, v)
			}

			rows, err := db.Query(`PRAGMA foreign_key_check`)
			require.NoError(t, err)
			defer rows.Close()
			assert.False(t, rows.Next(), "foreign key violations after rolling back to %d and forward again", v)
		})
	}
}
