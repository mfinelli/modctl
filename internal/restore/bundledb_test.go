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
	"path/filepath"
	"testing"

	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testBlobSha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func testProvider(t *testing.T) providerFunc {
	t.Helper()
	return func(db *sql.DB) (*goose.Provider, error) {
		return testbuilder.NewProvider(t, db), nil
	}
}

// newBundleDBFile creates a database file the way an export would, migrated
// to `behind` versions before the latest schema, with one row in it so we can
// check that data survives. It returns the path and the latest schema version.
func newBundleDBFile(t *testing.T, behind int64) (string, int64) {
	t.Helper()
	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "modctl.db")
	db, err := sql.Open("sqlite3", path+internal.DB_PRAGMAS)
	require.NoError(t, err)
	defer db.Close()

	p := testbuilder.NewProvider(t, db)
	_, latest, err := p.GetVersions(ctx)
	require.NoError(t, err)
	require.Greater(t, latest, behind, "test needs more migrations than it asks to skip")

	if behind == 0 {
		_, err = p.Up(ctx)
	} else {
		_, err = p.UpTo(ctx, latest-behind)
	}
	require.NoError(t, err)

	_, err = db.Exec(
		`INSERT INTO blobs (sha256, kind, size_bytes, original_name) VALUES (?, 'archive', 10, 'x.zip')`,
		testBlobSha,
	)
	require.NoError(t, err)

	return path, latest
}

func schemaVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var v int64
	require.NoError(t, db.QueryRow(
		`SELECT COALESCE(MAX(version_id), 0) FROM schema_migrations WHERE is_applied`,
	).Scan(&v))
	return v
}

func blobCount(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM blobs WHERE sha256 = ?`, testBlobSha).Scan(&n))
	return n
}

func TestOpenBundleDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("older schemas are migrated and keep their data", func(t *testing.T) {
		t.Parallel()

		for _, behind := range []int64{1, 2, 5} {
			behind := behind
			t.Run("behind "+string(rune('0'+behind)), func(t *testing.T) {
				t.Parallel()

				path, latest := newBundleDBFile(t, behind)

				// before: really is behind
				raw, err := sql.Open("sqlite3", path+internal.DB_PRAGMAS)
				require.NoError(t, err)
				assert.Equal(t, latest-behind, schemaVersion(t, raw))
				raw.Close()

				db, err := openBundleDB(ctx, path, testProvider(t))
				require.NoError(t, err)
				defer db.Close()

				assert.Equal(t, latest, schemaVersion(t, db))
				assert.EqualValues(t, 1, blobCount(t, db))
			})
		}
	})

	t.Run("current schema is left as it is", func(t *testing.T) {
		t.Parallel()

		path, latest := newBundleDBFile(t, 0)

		db, err := openBundleDB(ctx, path, testProvider(t))
		require.NoError(t, err)
		defer db.Close()

		assert.Equal(t, latest, schemaVersion(t, db))
		assert.EqualValues(t, 1, blobCount(t, db))
	})

	t.Run("newer schema is left alone", func(t *testing.T) {
		t.Parallel()

		path, latest := newBundleDBFile(t, 0)

		raw, err := sql.Open("sqlite3", path+internal.DB_PRAGMAS)
		require.NoError(t, err)
		_, err = raw.Exec(`INSERT INTO schema_migrations (version_id, is_applied) VALUES (?, TRUE)`, latest+1)
		require.NoError(t, err)
		raw.Close()

		// nothing to migrate to: open it as it is and let callers decide
		db, err := openBundleDB(ctx, path, testProvider(t))
		require.NoError(t, err)
		defer db.Close()

		assert.Equal(t, latest+1, schemaVersion(t, db))
		assert.EqualValues(t, 1, blobCount(t, db))
	})

	t.Run("the bundle database is read-only", func(t *testing.T) {
		t.Parallel()

		path, _ := newBundleDBFile(t, 0)

		db, err := openBundleDB(ctx, path, testProvider(t))
		require.NoError(t, err)
		defer db.Close()

		// reads work
		assert.EqualValues(t, 1, blobCount(t, db))

		// writes do not
		_, err = db.Exec(`DELETE FROM blobs`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "readonly")
		_, err = db.Exec(`CREATE TABLE not_allowed (a)`)
		require.Error(t, err)
		assert.EqualValues(t, 1, blobCount(t, db))
	})

	t.Run("a database in rollback-journal mode can still be opened read-only", func(t *testing.T) {
		t.Parallel()

		// e.g. a bundle whose database was written without WAL; opening it
		// read-only can't switch it to WAL itself, so the migration step has
		// to have done that first
		path := filepath.Join(t.TempDir(), "modctl.db")
		raw, err := sql.Open("sqlite3", path+"?_foreign_keys=ON&_journal_mode=DELETE")
		require.NoError(t, err)
		p := testbuilder.NewProvider(t, raw)
		_, err = p.Up(ctx)
		require.NoError(t, err)
		_, err = raw.Exec(
			`INSERT INTO blobs (sha256, kind, size_bytes, original_name) VALUES (?, 'archive', 10, 'x.zip')`,
			testBlobSha,
		)
		require.NoError(t, err)
		raw.Close()

		db, err := openBundleDB(ctx, path, testProvider(t))
		require.NoError(t, err)
		defer db.Close()

		assert.EqualValues(t, 1, blobCount(t, db))
		_, err = db.Exec(`DELETE FROM blobs`)
		require.Error(t, err)
	})

	t.Run("missing database is an error", func(t *testing.T) {
		t.Parallel()

		_, err := openBundleDB(ctx, filepath.Join(t.TempDir(), "nope", "modctl.db"), testProvider(t))
		require.Error(t, err)
	})
}
