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
	"embed"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
)

const DB_PRAGMAS = "?_foreign_keys=ON&_journal_mode=WAL&_synchronous=NORMAL"

var Migrations embed.FS

func SetupDB() (*sql.DB, error) {
	// TODO: db name/path is configurable
	return sql.Open("sqlite3", fmt.Sprintf("file:%s%s",
		"modctl.db", DB_PRAGMAS))
}

func MigrateDB(db *sql.DB) error {
	goose.SetBaseFS(Migrations)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("error setting goose dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("error migrating database: %w", err)
	}

	return nil
}
