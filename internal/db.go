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
	"fmt"
	"io/fs"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
	"github.com/spf13/viper"
)

const DB_PRAGMAS = "?_foreign_keys=ON&_journal_mode=WAL&_synchronous=NORMAL"

var Migrations embed.FS

func SetupDB() (*sql.DB, error) {
	return sql.Open("sqlite3", fmt.Sprintf("file:%s%s",
		viper.GetString("database"), DB_PRAGMAS))
}

func GooseProvider(db *sql.DB) (*goose.Provider, error) {
	// Make the provider FS point at the "migrations" directory within the embed.FS.
	fsys, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("error preparing migrations fs: %w", err)
	}

	return goose.NewProvider(goose.DialectSQLite3, db, fsys)
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
