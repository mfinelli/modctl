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
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/archivescanner"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/exporter"
)

// Full imports a full bundle into a fresh database.
func Full(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	bs blobstore.Store,
	bundle *Bundle,
	opts Options,
	dbPath string,
	modctlVersion string,
	logger *slog.Logger,
) (Result, error) {
	var res Result

	// Validate schema version
	currentSchema, err := currentSchemaVersion(ctx, db)
	if err != nil {
		return res, fmt.Errorf("get current schema version: %w", err)
	}
	if bundle.Manifest.SchemaVersion > currentSchema {
		return res, fmt.Errorf(
			"bundle schema version %d is newer than current schema version %d; run migrations first",
			bundle.Manifest.SchemaVersion, currentSchema,
		)
	}

	if opts.DryRun {
		return dryRunFull(ctx, bundle)
	}

	// Verify DB is empty (beyond auto-seeded rows)
	isEmpty, err := q.ImportCheckDBIsEmpty(ctx)
	if err != nil {
		return res, fmt.Errorf("check database empty: %w", err)
	}
	if !isEmpty.Bool && !opts.Force {
		return res, fmt.Errorf(
			"database is not empty; use --force to overwrite, or import into a fresh installation",
		)
	}

	// Import blobs first - do this before replacing the DB so that if
	// blob ingestion fails we haven't touched the database yet
	archiveCount, backupCount, err := importBlobs(ctx, bundle, bs)
	if err != nil {
		return res, fmt.Errorf("import blobs: %w", err)
	}
	res.Archives = archiveCount
	res.Backups = backupCount

	// Replace the database by copying the bundle DB into place
	bundleDBPath := filepath.Join(bundle.BundleDir, exporter.DatabaseFilename)
	db.Close()
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return res, fmt.Errorf("remove existing database: %w", err)
	}
	if err := copyFile(bundleDBPath, dbPath); err != nil {
		return res, fmt.Errorf("restore database: %w", err)
	}

	// Reopen and migrate (handles case where bundle schema is older than binary)
	newDB, err := internal.SetupDB()
	if err != nil {
		return res, fmt.Errorf("reopen database: %w", err)
	}
	if err := internal.MigrateDB(ctx, newDB); err != nil {
		return res, fmt.Errorf("migrate restored database: %w", err)
	}
	newQ := dbq.New(newDB)

	// Scan missing inventories against the restored DB
	bq := dbq.New(bundle.BundleDB)
	allVersions, err := bq.ListAllModFileVersions(ctx)
	if err != nil {
		return res, fmt.Errorf("list mod file versions: %w", err)
	}
	for _, v := range allVersions {
		if v.InventoryScannedAt.Valid {
			continue
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}
		if err := archivescanner.ScanOne(
			ctx, newDB, newQ, bs,
			archivescanner.Scanner{},
			v.ArchiveSha256,
			logger,
		); err != nil {
			res.InventoryFailed++
		} else {
			res.InventoryScanned++
		}
	}

	return res, nil
}

func dryRunFull(ctx context.Context, bundle *Bundle) (Result, error) {
	var res Result
	bq := dbq.New(bundle.BundleDB)

	archiveBlobs, err := bq.ListBlobsByKind(ctx, string(blobstore.KindArchive))
	if err != nil {
		return res, err
	}
	backupBlobs, err := bq.ListBlobsByKind(ctx, string(blobstore.KindBackup))
	if err != nil {
		return res, err
	}
	res.Archives = len(archiveBlobs)
	res.Backups = len(backupBlobs)
	return res, nil
}
