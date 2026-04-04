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

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/exporter"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/mfinelli/modctl/internal/nexusclient/dbc"
)

// importFullCache copies the nexus cache database from the bundle into place,
// initializing it if it does not already exist.
func importFullCache(ctx context.Context, bundleCachePath, destCachePath string) error {
	if err := copyFile(bundleCachePath, destCachePath); err != nil {
		return fmt.Errorf("copy nexus cache: %w", err)
	}

	// Open and initialize to handle schema version check / reset if stale
	db, err := sql.Open("sqlite3", destCachePath+internal.DB_PRAGMAS)
	if err != nil {
		return fmt.Errorf("open nexus cache: %w", err)
	}
	defer db.Close()

	return nexusclient.InitCacheDB(ctx, db)
}

// importGameCache merges nexus cache rows for the imported game's mod pages
// into the live cache database.
func importGameCache(
	ctx context.Context,
	bundleCachePath string,
	destCachePath string,
	bq *dbq.Queries,
	oldGameInstallID int64,
) error {
	// Get nexus identifiers for the imported game's mod pages
	modPages, err := bq.ExportGetModPagesForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get mod pages: %w", err)
	}

	// Filter to nexus mod pages only
	type nexusKey struct {
		domain string
		modID  int64
	}
	var keys []nexusKey
	for _, mp := range modPages {
		if mp.NexusGameDomain.Valid && mp.NexusModID.Valid {
			keys = append(keys, nexusKey{mp.NexusGameDomain.String, mp.NexusModID.Int64})
		}
	}
	if len(keys) == 0 {
		return nil
	}

	// Open bundle cache DB
	srcDB, err := sql.Open("sqlite3", bundleCachePath+internal.DB_PRAGMAS+"&mode=ro")
	if err != nil {
		return fmt.Errorf("open bundle nexus cache: %w", err)
	}
	defer srcDB.Close()
	src := dbc.New(srcDB)

	// Open or create live cache DB
	destDB, err := sql.Open("sqlite3", destCachePath+internal.DB_PRAGMAS)
	if err != nil {
		return fmt.Errorf("open live nexus cache: %w", err)
	}
	defer destDB.Close()

	if err := nexusclient.InitCacheDB(ctx, destDB); err != nil {
		return fmt.Errorf("init live nexus cache: %w", err)
	}
	dst := dbc.New(destDB)

	// Merge rows for each mod page
	for _, key := range keys {
		if err := exporter.ExportCacheModInfo(ctx, src, dst, key.domain, key.modID); err != nil {
			return fmt.Errorf("merge cache mod info (%s/%d): %w", key.domain, key.modID, err)
		}
		if err := exporter.ExportCacheFileInfo(ctx, src, dst, key.domain, key.modID); err != nil {
			return fmt.Errorf("merge cache file info (%s/%d): %w", key.domain, key.modID, err)
		}
		if err := exporter.ExportCacheFileUpdates(ctx, src, dst, key.domain, key.modID); err != nil {
			return fmt.Errorf("merge cache file updates (%s/%d): %w", key.domain, key.modID, err)
		}
	}

	return nil
}
