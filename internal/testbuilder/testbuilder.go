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
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/pressly/goose/v3"
	goosedb "github.com/pressly/goose/v3/database"
	"github.com/stretchr/testify/require"
)

// SetupDB creates an in-memory SQLite database with migrations applied.
// The DB is closed automatically when the test completes.
// Apply migrations from the filesystem rather than the embedded FS which
// is not populated when running tests.
func SetupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err, "open in-memory sqlite")
	t.Cleanup(func() { db.Close() })

	p := NewProvider(t, db)

	_, err = p.Up(context.Background())
	require.NoError(t, err, "run migrations")

	return db
}

// SetupDBFile is SetupDB for a database in a file, which is what something that
// opens its database by its path needs (a database that is not in memory is
// one for every connection, which is what lets the code that is tested open it
// for itself). It returns the database, for what the test puts in it, and the
// path to it. The file is in a directory of the test, and the database is
// closed when the test completes.
func SetupDBFile(t *testing.T) (*sql.DB, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "modctl.db")
	db, err := sql.Open("sqlite3", database.DSN(path, false))
	require.NoError(t, err, "open sqlite file")
	t.Cleanup(func() { db.Close() })

	_, err = NewProvider(t, db).Up(context.Background())
	require.NoError(t, err, "run migrations")

	return db, path
}

// NewProvider returns a goose provider for db that reads migrations from the
// filesystem, for tests that need to control how far a database is migrated.
func NewProvider(t *testing.T, db *sql.DB) *goose.Provider {
	t.Helper()

	fsys := os.DirFS(findMigrationsDir(t))

	base, err := goosedb.NewStore(goosedb.DialectSQLite3, "schema_migrations")
	require.NoError(t, err, "create goose store")

	p, err := goose.NewProvider(goose.DialectCustom, db, fsys,
		goose.WithStore(&database.SqliteStore{Store: base}),
	)
	require.NoError(t, err, "create goose provider")

	return p
}

// MigrationsFS is what main gives to database.Migrations in the binary: the
// repository, which has its migrations in a "migrations" directory. The
// embedded copy is not there in a test, so a package whose tests need to
// migrate a database sets this once, in its TestMain, before any test runs:
//
//	func TestMain(m *testing.M) {
//		database.Migrations = testbuilder.MigrationsFS()
//		os.Exit(m.Run())
//	}
//
// It is not set by a test for as long as the test runs, since that is a change
// of something that is global, which the tests that run at the same time would
// see.
func MigrationsFS() fs.FS {
	dir, err := migrationsDir()
	if err != nil {
		panic(err)
	}

	return os.DirFS(filepath.Dir(dir))
}

func findMigrationsDir(t *testing.T) string {
	t.Helper()

	dir, err := migrationsDir()
	require.NoError(t, err)

	return dir
}

func migrationsDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	// filename is the absolute path to this source file;
	// walk up until we find a directory containing "migrations/"
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "migrations")); err == nil {
			return filepath.Join(dir, "migrations"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find migrations directory")
		}
		dir = parent
	}
}
