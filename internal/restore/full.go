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
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/archivescanner"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/exporter"
	"github.com/mfinelli/modctl/internal/fsutil"
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

	if opts.Schema == nil {
		return res, errors.New("no schema to restore the database with")
	}

	// Validate schema version
	currentSchema, err := opts.Schema.CurrentVersion(ctx, db)
	if err != nil {
		return res, fmt.Errorf("get current schema version: %w", err)
	}
	if bundle.Manifest.SchemaVersion > currentSchema {
		return res, fmt.Errorf(
			"bundle schema version %d is newer than current schema version %d; upgrade modctl to import this bundle",
			bundle.Manifest.SchemaVersion, currentSchema,
		)
	}

	// Nothing is changed (not even a dry run is let through) for a bundle that
	// would have something written outside of the game directory. This is
	// before the database is replaced, which cannot be undone.
	bq := dbq.New(bundle.BundleDB)
	if err := validateFullPaths(ctx, bq, opts.SameMachine); err != nil {
		return res, err
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
	archiveCount, backupCount, overrideCount, err := importBlobs(ctx, bundle, bs)
	if err != nil {
		return res, fmt.Errorf("import blobs: %w", err)
	}
	res.Archives = archiveCount
	res.Backups = backupCount
	res.Overrides = overrideCount

	// Replace the database with the one of the bundle. It is copied next to the
	// one that is there first, and only when that is whole does it take its
	// place, by being renamed over it, which is atomic: a copy that fails (a full
	// disk, a bundle that is cut short) leaves the database that is there as it
	// was, and still open.
	bundleDBPath := filepath.Join(bundle.BundleDir, exporter.DatabaseFilename)
	restoringPath := dbPath + ".restoring"
	if err := fsutil.CopyFile(ctx, bundleDBPath, restoringPath); err != nil {
		return res, fmt.Errorf("restore database: %w", err)
	}
	db.Close()
	// what the database that is replaced may have left is not part of the new one
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suffix)
	}
	if err := os.Rename(restoringPath, dbPath); err != nil {
		_ = os.Remove(restoringPath)
		return res, fmt.Errorf("replace database: %w", err)
	}

	// Reopen and migrate (handles case where bundle schema is older than binary)
	newDB, err := database.Setup(dbPath)
	if err != nil {
		return res, fmt.Errorf("reopen database: %w", err)
	}
	defer newDB.Close()
	if err := opts.Schema.Migrate(ctx, newDB); err != nil {
		return res, fmt.Errorf("migrate restored database: %w", err)
	}
	newQ := dbq.New(newDB)

	// Zero out on-disk state unless this is a same-machine restore, which is also
	// when the targets are not the ones of another machine
	if !opts.SameMachine {
		if err := zeroOnDiskState(ctx, newDB, newQ); err != nil {
			return res, fmt.Errorf("zero on-disk state: %w", err)
		}

		disabled, err := disableImportedTargets(ctx, newDB)
		if err != nil {
			return res, fmt.Errorf("disable imported targets: %w", err)
		}
		res.DisabledTargets = disabled
	}

	// Import nexus cache if present in bundle
	if bundle.Manifest.NexusCacheSha256 != "" {
		bundleCachePath := filepath.Join(bundle.BundleDir, "nexus_cache.db")
		if err := importFullCache(ctx, bundleCachePath, opts.CacheDBPath); err != nil {
			return res, fmt.Errorf("import nexus cache: %w", err)
		}
	}

	// Scan missing inventories against the restored DB
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

func zeroOnDiskState(ctx context.Context, db *sql.DB, q *dbq.Queries) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := q.WithTx(tx)

	err = qtx.DeleteAllInstalledFiles(ctx)
	if err != nil {
		return fmt.Errorf("delete all installed_files: %w", err)
	}

	err = qtx.DeleteAllBackups(ctx)
	if err != nil {
		return fmt.Errorf("delete all backups: %w", err)
	}

	err = qtx.DeleteAllOperations(ctx)
	if err != nil {
		return fmt.Errorf("delete all operations and operation changes: %w", err)
	}

	err = qtx.ZeroAllGameInstallsState(ctx)
	if err != nil {
		return fmt.Errorf("zeroing game installs state: %w", err)
	}

	return tx.Commit()
}
