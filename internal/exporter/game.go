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

package exporter

import (
	"archive/tar"
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/blobstore"
)

// Game performs a game-scoped export containing only data relevant to a
// single game install.
func Game(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	bs blobstore.Store,
	gi dbq.GameInstall,
	opts Options,
) error {
	out, err := os.Create(opts.OutputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}

	// clean up partial output on any failure
	success := false
	defer func() {
		if !success {
			out.Close()
			os.Remove(opts.OutputPath)
		}
	}()

	zw, err := zstd.NewWriter(out)
	if err != nil {
		return fmt.Errorf("create zstd writer: %w", err)
	}
	defer zw.Close()

	tw := tar.NewWriter(zw)
	defer tw.Close()

	// 1. Build the scoped SQLite database
	scopedDBPath, dbSha256, archiveCount, backupCount, err := buildGameScopedDB(
		ctx, db, q, gi, opts.SkipInventory,
	)
	if err != nil {
		return fmt.Errorf("build game-scoped database: %w", err)
	}
	defer os.Remove(scopedDBPath)

	// 2. Get schema version from source DB
	schemaVersion, err := currentSchemaVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("get schema version: %w", err)
	}

	// 3. Collect blobs referenced by this game
	archiveBlobs, err := q.ListArchiveBlobsForGameInstall(ctx, gi.ID)
	if err != nil {
		return fmt.Errorf("list archive blobs for game: %w", err)
	}
	backupBlobs, err := q.ListBackupBlobsForGameInstall(ctx, gi.ID)
	if err != nil {
		return fmt.Errorf("list backup blobs for game: %w", err)
	}

	// 4. Write manifest
	manifest := Manifest{
		ExportFormatVersion: ExportFormatVersion,
		ExportKind:          ExportKindGame,
		ExportedAt:          time.Now().UTC(),
		ModctlVersion:       opts.ModctlVersion,
		SchemaVersion:       schemaVersion,
		DBSha256:            dbSha256,
		Counts: ManifestCounts{
			Archives: archiveCount,
			Backups:  backupCount,
		},
		Game: &ManifestGame{
			StoreID:     gi.StoreID,
			StoreGameID: gi.StoreGameID,
			DisplayName: gi.DisplayName,
		},
	}
	if err := writeManifest(tw, manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// 5. Write scoped database
	if err := writeFileToTar(tw, scopedDBPath, DatabaseFilename); err != nil {
		return fmt.Errorf("write database: %w", err)
	}

	// 6. Write archive blobs
	var skipped []string
	for _, b := range archiveBlobs {
		skip, err := writeBlobToTar(ctx, tw, bs, blobstore.KindArchive, b.Sha256)
		if err != nil {
			return fmt.Errorf("write archive blob %s: %w", b.Sha256, err)
		}
		if skip {
			skipped = append(skipped, b.Sha256)
		}
	}

	// 7. Write backup blobs
	for _, b := range backupBlobs {
		skip, err := writeBlobToTar(ctx, tw, bs, blobstore.KindBackup, b.Sha256)
		if err != nil {
			return fmt.Errorf("write backup blob %s: %w", b.Sha256, err)
		}
		if skip {
			skipped = append(skipped, b.Sha256)
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zstd: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	success = true

	// TODO: we should probably push this up to the caller
	for _, sha := range skipped {
		fmt.Fprintf(os.Stderr, "warning: blob %s... missing from disk, skipped in export\n", sha[:16])
	}

	return nil
}

// buildGameScopedDB constructs a fresh SQLite database containing only rows
// relevant to the given game install. Returns the path to the temp file,
// the sha256 of the database, and blob counts.
func buildGameScopedDB(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	gi dbq.GameInstall,
	skipInventory bool,
) (path string, dbSha256 string, archiveCount int, backupCount int, err error) {
	tmp, err := os.CreateTemp("", "modctl-export-*.sqlite")
	if err != nil {
		return "", "", 0, 0, fmt.Errorf("create temp db: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()

	scopedDB, err := sql.Open("sqlite3", tmpPath+internal.DB_PRAGMAS)
	if err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("open scoped db: %w", err)
	}
	defer scopedDB.Close()

	if err := internal.MigrateDB(ctx, scopedDB); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("migrate scoped db: %w", err)
	}

	sq := dbq.New(scopedDB)

	if err := exportStore(ctx, q, sq, gi.StoreID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export store: %w", err)
	}

	if err := exportGameInstall(ctx, sq, gi); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export game install: %w", err)
	}

	if err := exportTargets(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export targets: %w", err)
	}

	archiveCount, backupCount, err = exportBlobs(ctx, q, sq, gi.ID)
	if err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export blobs: %w", err)
	}

	if err := exportModPages(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export mod pages: %w", err)
	}

	if err := exportModFiles(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export mod files: %w", err)
	}

	if err := exportModFileVersions(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export mod file versions: %w", err)
	}

	if !skipInventory {
		if err := exportInventory(ctx, q, sq, gi.ID); err != nil {
			os.Remove(tmpPath)
			return "", "", 0, 0, fmt.Errorf("export inventory: %w", err)
		}
	}

	if err := exportRemapConfigs(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export remap configs: %w", err)
	}

	if err := exportProfiles(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export profiles: %w", err)
	}

	if err := exportProfileItems(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export profile items: %w", err)
	}

	if err := exportProfilePathPolicies(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export profile path policies: %w", err)
	}

	if err := exportBackups(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export backups: %w", err)
	}

	if err := exportModIncompatibilities(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("export mod incompatibilities: %w", err)
	}

	if err := scopedDB.Close(); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("close scoped db: %w", err)
	}

	sha, err := hashFile(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, fmt.Errorf("hash scoped db: %w", err)
	}

	return tmpPath, sha, archiveCount, backupCount, nil
}

func exportStore(ctx context.Context, src, dst *dbq.Queries, storeID string) error {
	row, err := src.GetStoreById(ctx, storeID)
	if err != nil {
		return fmt.Errorf("get store %s: %w", storeID, err)
	}
	return dst.ExportInsertStore(ctx, dbq.ExportInsertStoreParams(row))
}

func exportGameInstall(ctx context.Context, dst *dbq.Queries, gi dbq.GameInstall) error {
	return dst.ExportInsertGameInstall(ctx, dbq.ExportInsertGameInstallParams{
		ID:              gi.ID,
		StoreID:         gi.StoreID,
		StoreGameID:     gi.StoreGameID,
		DisplayName:     gi.DisplayName,
		InstanceID:      gi.InstanceID,
		CanonicalGameID: gi.CanonicalGameID,
		InstallRoot:     gi.InstallRoot,
		Metadata:        gi.Metadata,
		LastSeenAt:      gi.LastSeenAt,
		IsPresent:       gi.IsPresent,
		CreatedAt:       gi.CreatedAt,
		UpdatedAt:       gi.UpdatedAt,
	})
}

func exportTargets(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ListTargetsForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get targets: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertTarget(ctx, dbq.ExportInsertTargetParams(row)); err != nil {
			return fmt.Errorf("insert target %d: %w", row.ID, err)
		}
	}
	return nil
}

func exportBlobs(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) (archiveCount, backupCount int, err error) {
	archiveRows, err := src.ExportGetArchiveBlobsForGameInstall(ctx, gameInstallID)
	if err != nil {
		return 0, 0, fmt.Errorf("get archive blobs: %w", err)
	}
	for _, row := range archiveRows {
		if err := dst.ExportInsertBlob(ctx, dbq.ExportInsertBlobParams(row)); err != nil {
			return 0, 0, fmt.Errorf("insert archive blob %s: %w", row.Sha256, err)
		}
		archiveCount++
	}

	backupRows, err := src.ExportGetBackupBlobsForGameInstall(ctx, gameInstallID)
	if err != nil {
		return 0, 0, fmt.Errorf("get backup blobs: %w", err)
	}
	for _, row := range backupRows {
		if err := dst.ExportInsertBlob(ctx, dbq.ExportInsertBlobParams(row)); err != nil {
			return 0, 0, fmt.Errorf("insert backup blob %s: %w", row.Sha256, err)
		}
		backupCount++
	}

	return archiveCount, backupCount, nil
}

func exportModPages(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ExportGetModPagesForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get mod pages: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertModPage(ctx, dbq.ExportInsertModPageParams(row)); err != nil {
			return fmt.Errorf("insert mod page %d: %w", row.ID, err)
		}
	}
	return nil
}

func exportModFiles(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ExportGetModFilesForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get mod files: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertModFile(ctx, dbq.ExportInsertModFileParams(row)); err != nil {
			return fmt.Errorf("insert mod file %d: %w", row.ID, err)
		}
	}
	return nil
}

func exportModFileVersions(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ExportGetModFileVersionsForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get mod file versions: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertModFileVersion(ctx, dbq.ExportInsertModFileVersionParams(row)); err != nil {
			return fmt.Errorf("insert mod file version %d: %w", row.ID, err)
		}
	}
	return nil
}

func exportInventory(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ExportGetInventoryForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get inventory: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertInventoryEntry(ctx, dbq.ExportInsertInventoryEntryParams(row)); err != nil {
			return fmt.Errorf("insert inventory entry %d: %w", row.ID, err)
		}
	}
	return nil
}

func exportRemapConfigs(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	configs, err := src.ExportGetRemapConfigsForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get remap configs: %w", err)
	}
	for _, cfg := range configs {
		if err := dst.ExportInsertRemapConfig(ctx, dbq.ExportInsertRemapConfigParams(cfg)); err != nil {
			return fmt.Errorf("insert remap config %d: %w", cfg.ID, err)
		}

		rules, err := src.ExportGetRemapRulesForConfig(ctx, cfg.ID)
		if err != nil {
			return fmt.Errorf("get remap rules for config %d: %w", cfg.ID, err)
		}
		for _, rule := range rules {
			if err := dst.ExportInsertRemapRule(ctx, dbq.ExportInsertRemapRuleParams(rule)); err != nil {
				return fmt.Errorf("insert remap rule %d: %w", rule.ID, err)
			}
		}
	}
	return nil
}

func exportProfiles(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.GetProfilesForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get profiles: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertProfile(ctx, dbq.ExportInsertProfileParams(row)); err != nil {
			return fmt.Errorf("insert profile %d: %w", row.ID, err)
		}
	}
	return nil
}

func exportProfileItems(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ExportGetProfileItemsForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get profile items: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertProfileItem(ctx, dbq.ExportInsertProfileItemParams(row)); err != nil {
			return fmt.Errorf("insert profile item %d: %w", row.ID, err)
		}
	}
	return nil
}

func exportProfilePathPolicies(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ExportGetProfilePathPoliciesForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get profile path policies: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertProfilePathPolicy(ctx, dbq.ExportInsertProfilePathPolicyParams(row)); err != nil {
			return fmt.Errorf("insert profile path policy %d: %w", row.ID, err)
		}
	}
	return nil
}

func exportBackups(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ExportGetBackupsForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get backups: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertBackup(ctx, dbq.ExportInsertBackupParams{
			ID:                    row.ID,
			GameInstallID:         row.GameInstallID,
			TargetID:              row.TargetID,
			Relpath:               row.Relpath,
			BackupBlobSha256:      row.BackupBlobSha256,
			OriginalContentSha256: row.OriginalContentSha256,
			SizeBytes:             row.SizeBytes,
			CreatedAt:             row.CreatedAt,
		}); err != nil {
			return fmt.Errorf("insert backup %d: %w", row.ID, err)
		}
	}
	return nil
}

func exportModIncompatibilities(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ExportGetModIncompatibilitiesForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get mod incompatibilities: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertModIncompatibility(ctx, dbq.ExportInsertModIncompatibilityParams(row)); err != nil {
			return fmt.Errorf("insert mod incompatibility %d: %w", row.ID, err)
		}
	}
	return nil
}
