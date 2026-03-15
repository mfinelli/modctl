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
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mfinelli/modctl/internal"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
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

	migrationsDir := findMigrationsDir(t)
	fsys := os.DirFS(migrationsDir)

	base, err := database.NewStore(database.DialectSQLite3, "schema_migrations")
	require.NoError(t, err, "create goose store")

	p, err := goose.NewProvider(goose.DialectCustom, db, fsys,
		goose.WithStore(&internal.SqliteStore{Store: base}),
	)
	require.NoError(t, err, "create goose provider")

	_, err = p.Up(context.Background())
	require.NoError(t, err, "run migrations")

	return db
}

func findMigrationsDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// filename is the absolute path to this source file;
	// walk up until we find a directory containing "migrations/"
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "migrations")); err == nil {
			return filepath.Join(dir, "migrations")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find migrations directory")
		}
		dir = parent
	}
}
