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
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pragma reads one pragma's value through the connection.
func pragma(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var v string
	require.NoError(t, db.QueryRow("PRAGMA "+name).Scan(&v))
	return v
}

// odd directory names that a plain-path DSN gets wrong
var dsnTestDirs = map[string]string{
	"plain":   "modctl",
	"spaces":  "my mods",
	"query":   "what?is=this",
	"hash":    "mods#1",
	"percent": "100%25 done",
	"unicode": "módctl-モッド",
}

func TestDSNReadWrite(t *testing.T) {
	t.Parallel()

	for name, dir := range dsnTestDirs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			full := filepath.Join(t.TempDir(), dir)
			require.NoError(t, os.MkdirAll(full, 0o755))
			path := filepath.Join(full, "modctl.db")

			db, err := sql.Open("sqlite3", DSN(path, false))
			require.NoError(t, err)
			defer db.Close()

			_, err = db.Exec(`CREATE TABLE t (a)`)
			require.NoError(t, err)

			// the database ended up where we said, not at some truncated path
			_, err = os.Stat(path)
			assert.NoError(t, err)

			// and the pragmas we rely on were applied to the connection
			assert.Equal(t, "1", pragma(t, db, "foreign_keys"))
			assert.Equal(t, "wal", pragma(t, db, "journal_mode"))
			assert.Equal(t, "1", pragma(t, db, "synchronous")) // NORMAL
		})
	}
}

func TestDSNReadOnly(t *testing.T) {
	t.Parallel()

	t.Run("a database in WAL mode", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "modctl.db")
		rw, err := sql.Open("sqlite3", DSN(path, false))
		require.NoError(t, err)
		_, err = rw.Exec(`CREATE TABLE t (a); INSERT INTO t VALUES (1)`)
		require.NoError(t, err)
		rw.Close()

		db, err := sql.Open("sqlite3", DSN(path, true))
		require.NoError(t, err)
		defer db.Close()

		var n int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n))
		assert.Equal(t, 1, n)
		assert.Equal(t, "1", pragma(t, db, "foreign_keys"))

		_, err = db.Exec(`INSERT INTO t VALUES (2)`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "readonly")
	})

	// This is how the databases inside an export are stored (VACUUM INTO
	// writes them without WAL). Asking such a database for WAL on a read-only
	// connection fails, so the read-only DSN must not.
	t.Run("a database in rollback-journal mode", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		src, err := sql.Open("sqlite3", DSN(filepath.Join(dir, "src.db"), false))
		require.NoError(t, err)
		defer src.Close()
		_, err = src.Exec(`CREATE TABLE t (a); INSERT INTO t VALUES (1)`)
		require.NoError(t, err)

		snapshot := filepath.Join(dir, "snapshot.db")
		_, err = src.Exec(`VACUUM INTO ?`, snapshot)
		require.NoError(t, err)

		db, err := sql.Open("sqlite3", DSN(snapshot, true))
		require.NoError(t, err)
		defer db.Close()

		var n int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n))
		assert.Equal(t, 1, n)
		assert.Equal(t, "delete", pragma(t, db, "journal_mode"))

		_, err = db.Exec(`INSERT INTO t VALUES (2)`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "readonly")
	})

	t.Run("a missing database is an error rather than being created", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nope.db")
		db, err := sql.Open("sqlite3", DSN(path, true))
		require.NoError(t, err)
		defer db.Close()

		require.Error(t, db.Ping())
		_, err = os.Stat(path)
		assert.True(t, os.IsNotExist(err))
	})
}
