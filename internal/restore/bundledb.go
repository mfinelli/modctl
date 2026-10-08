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
	"fmt"

	"github.com/mfinelli/modctl/internal"
	"github.com/pressly/goose/v3"
)

// providerFunc builds a goose provider (and so the set of known migrations)
// for a database. Production code uses internal.GooseProvider; tests supply
// one that reads migrations from the filesystem because the embedded copy is
// only populated in the real binary.
type providerFunc func(*sql.DB) (*goose.Provider, error)

// openBundleDB opens the extracted bundle database read-only. If the bundle
// was exported by an older version of modctl its schema is migrated to the
// current one first, so that the current queries can read it. This only ever
// touches the temporary extracted copy: the original bundle file is opened
// read-only and streamed into the temp directory, and never written to.
func openBundleDB(ctx context.Context, dbPath string, newProvider providerFunc) (*sql.DB, error) {
	if err := migrateBundleDB(ctx, dbPath, newProvider); err != nil {
		return nil, err
	}
	return sql.Open("sqlite3", internal.DSN(dbPath, true))
}

// migrateBundleDB brings the bundle database at dbPath up to the latest
// schema known to this version of modctl. A database that is already current
// is left untouched. So is one that is newer than modctl knows about: there is
// nothing to migrate it to, and it is up to the caller to decide what to do
// (import refuses it, verify just reports on it).
func migrateBundleDB(ctx context.Context, dbPath string, newProvider providerFunc) error {
	db, err := sql.Open("sqlite3", internal.DSN(dbPath, false))
	if err != nil {
		return fmt.Errorf("open bundle database for migration: %w", err)
	}
	defer db.Close()

	p, err := newProvider(db)
	if err != nil {
		return fmt.Errorf("get goose provider: %w", err)
	}

	current, latest, err := p.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("get bundle schema version: %w", err)
	}

	if current < latest {
		if _, err := p.Up(ctx); err != nil {
			return fmt.Errorf("migrate bundle database from schema version %d: %w", current, err)
		}
	}

	return nil
}
