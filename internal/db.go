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
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/spf13/viper"
)

// DB_PRAGMAS are the connection parameters for a read-write database:
// foreign keys enforced, write-ahead logging and synchronous NORMAL. The
// driver runs each of them as a PRAGMA when a connection is opened.
const DB_PRAGMAS = "?_foreign_keys=ON&_journal_mode=WAL&_synchronous=NORMAL"

// DB_PRAGMAS_READONLY are the connection parameters for a read-only database.
// There is deliberately no journal mode: a read-only connection can't change
// it, and asking for WAL on a database that is stored in rollback-journal
// mode fails with "attempt to write a readonly database". That is exactly how
// the databases inside an export are stored, since VACUUM INTO writes them
// that way.
const DB_PRAGMAS_READONLY = "?_foreign_keys=ON&mode=ro"

var Migrations embed.FS

// DSN returns the connection string for the SQLite database at path, which
// every database open in modctl should use rather than building its own.
//
// It is a file: URI with an escaped path. With a plain path the driver cuts
// the query string off before SQLite sees it, which silently drops mode=ro
// (the connection stays writable), and a path that contains a '?' is cut
// short at it. The read-only variant is described at DB_PRAGMAS_READONLY.
func DSN(path string, readOnly bool) string {
	pragmas := DB_PRAGMAS
	if readOnly {
		pragmas = DB_PRAGMAS_READONLY
	}
	return fmt.Sprintf("file:%s%s", url.PathEscape(path), pragmas)
}

func SetupDB() (*sql.DB, error) {
	return sql.Open("sqlite3", DSN(viper.GetString("database"), false))
}

func SetupDBReadOnly() (*sql.DB, error) {
	return sql.Open("sqlite3", DSN(viper.GetString("database"), true))
}

func GooseProvider(db *sql.DB) (*goose.Provider, error) {
	// Make the provider FS point at the "migrations" directory within the embed.FS.
	fsys, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("error preparing migrations fs: %w", err)
	}

	base, err := database.NewStore(database.DialectSQLite3, "schema_migrations")
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectCustom, db, fsys,
		goose.WithStore(&SqliteStore{Store: base}),
	)
}

func MigrateDB(ctx context.Context, db *sql.DB) error {
	p, err := GooseProvider(db)
	if err != nil {
		return fmt.Errorf("error setting up goose provider: %w", err)
	}

	_, err = p.Up(ctx)
	if err != nil {
		return fmt.Errorf("error migrating database: %w", err)
	}

	return nil
}

// CurrentSchemaVersion returns the version of the latest migration that has
// been applied to db. This is what the database itself is at, which is not
// necessarily the latest migration this build of modctl knows about: a new
// database reports 0 and one that has not been migrated since an upgrade
// reports its older version.
func CurrentSchemaVersion(ctx context.Context, db *sql.DB) (int64, error) {
	p, err := GooseProvider(db)
	if err != nil {
		return 0, fmt.Errorf("get goose provider: %w", err)
	}

	return schemaVersion(ctx, p)
}

// schemaVersion returns the applied migration version as seen by p. It is
// split out from CurrentSchemaVersion so that tests can supply a provider that
// reads its migrations from the filesystem (the embedded copy is only
// populated in the real binary).
func schemaVersion(ctx context.Context, p *goose.Provider) (int64, error) {
	current, _, err := p.GetVersions(ctx)
	if err != nil {
		return 0, fmt.Errorf("get schema version: %w", err)
	}

	return current, nil
}

// LatestSchemaVersion returns the version of the latest migration known to
// this build of modctl: the highest version among the embedded migrations. It
// does not look at any database, so it is the version a database ends up at
// once it has been fully migrated, and what a bundle's schema version has to
// be compared against to tell whether this build can import it.
func LatestSchemaVersion() (int64, error) {
	fsys, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		return 0, fmt.Errorf("error preparing migrations fs: %w", err)
	}

	return latestMigrationVersion(fsys)
}

// latestMigrationVersion returns the highest version among the .sql
// migrations in the root of fsys. It is split out from LatestSchemaVersion so
// that tests can supply their own migrations (the embedded copy is only
// populated in the real binary).
func latestMigrationVersion(fsys fs.FS) (int64, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return 0, fmt.Errorf("read migrations: %w", err)
	}

	var latest int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}

		// goose's own parsing, so that we can't disagree with it about which
		// version a file is
		v, err := goose.NumericComponent(e.Name())
		if err != nil {
			return 0, fmt.Errorf("migration %q: %w", e.Name(), err)
		}
		latest = max(latest, v)
	}

	if latest == 0 {
		return 0, errors.New("no migrations found")
	}

	return latest, nil
}

// OpenDB opens the database that the commands use: it has to be there (it is
// made by init), and it is migrated to the latest schema before it is handed
// over. The caller closes it.
func OpenDB(ctx context.Context) (*sql.DB, error) {
	return openDB(ctx, MigrateDB)
}

// openDB is OpenDB with the migrating done by migrate, so that a test can
// have migrations that are not the ones that are embedded in the binary.
func openDB(ctx context.Context, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	if err := EnsureDBExists(); err != nil {
		return nil, err
	}

	db, err := SetupDB()
	if err != nil {
		return nil, fmt.Errorf("error setting up database: %w", err)
	}

	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// EnsureDBExists verifies that the configured database file exists
// and is a regular file. If not, it returns a user-friendly error.
func EnsureDBExists() error {
	path := viper.GetString("database")
	if path == "" {
		return fmt.Errorf("database path is not configured")
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(
				"database not found at %s\n\nRun `modctl init` to initialize the state directory",
				path,
			)
		}
		return fmt.Errorf("cannot access database %s: %w", path, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("database path %s exists but is not a regular file", path)
	}

	return nil
}

// A custom goose store to let us override the schema migrations table to use
// more idiomatic sqlite
type SqliteStore struct {
	database.Store // embed and delegate everything
}

func (s *SqliteStore) CreateVersionTable(ctx context.Context, db database.DBTxConn) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        id         INTEGER PRIMARY KEY,
        version_id INTEGER NOT NULL,
        is_applied INTEGER NOT NULL CHECK (is_applied IN (TRUE, FALSE)),
        tstamp     TEXT NOT NULL DEFAULT (datetime('now'))
    ) STRICT;`)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO schema_migrations (version_id, is_applied) VALUES (0, TRUE);`)
	return err
}
