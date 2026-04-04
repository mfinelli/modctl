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
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/mfinelli/modctl/internal/nexusclient/dbc"
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
	if !opts.NoVerify {
		var toVerify []blobToVerify
		archiveBlobs, err := q.ListArchiveBlobsForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list archive blobs: %w", err)
		}
		for _, b := range archiveBlobs {
			toVerify = append(toVerify, blobToVerify{b.Sha256, blobstore.KindArchive})
		}
		overrideBlobs, err := q.ListOverrideBlobsForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list override blobs: %w", err)
		}
		for _, b := range overrideBlobs {
			toVerify = append(toVerify, blobToVerify{b.Sha256, blobstore.KindOverride})
		}
		if err := verifyBlobs(ctx, q, bs, toVerify); err != nil {
			return fmt.Errorf("blob verification failed: %w", err)
		}
	}

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
	scopedDBPath, dbSha256, archiveCount, backupCount, overrideCount, err := buildGameScopedDB(
		ctx, q, gi, opts.SkipInventory,
	)
	if err != nil {
		return fmt.Errorf("build game-scoped database: %w", err)
	}
	defer os.Remove(scopedDBPath)

	// 1b. Build scoped nexus cache database
	cacheDBPath, cacheSha256, err := buildGameScopedCacheDB(
		ctx, q, opts.CacheDBPath, gi.ID,
	)
	if err != nil {
		return fmt.Errorf("build game-scoped nexus cache: %w", err)
	}
	if cacheDBPath != "" {
		defer os.Remove(cacheDBPath)
	}

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
	overrideBlobs, err := q.ListOverrideBlobsForGameInstall(ctx, gi.ID)
	if err != nil {
		return fmt.Errorf("list override blobs for game: %w", err)
	}

	// 4. Write manifest
	manifest := Manifest{
		ExportFormatVersion: ExportFormatVersion,
		ExportKind:          ExportKindGame,
		ExportedAt:          time.Now().UTC(),
		ModctlVersion:       opts.ModctlVersion,
		SchemaVersion:       schemaVersion,
		DBSha256:            dbSha256,
		NexusCacheSha256:    cacheSha256,
		Counts: ManifestCounts{
			Archives:  archiveCount,
			Backups:   backupCount,
			Overrides: overrideCount,
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

	// 5b. Write scoped nexus cache if present
	if cacheDBPath != "" {
		if err := writeFileToTar(tw, cacheDBPath, "nexus_cache.db"); err != nil {
			return fmt.Errorf("write nexus cache: %w", err)
		}
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

	// 7. Write override blobs
	for _, b := range overrideBlobs {
		skip, err := writeBlobToTar(ctx, tw, bs, blobstore.KindOverride, b.Sha256)
		if err != nil {
			return fmt.Errorf("write override blob %s: %w", b.Sha256, err)
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
	q *dbq.Queries,
	gi dbq.GameInstall,
	skipInventory bool,
) (path string, dbSha256 string, archiveCount int, backupCount int, overrideCount int, err error) {
	tmp, err := os.CreateTemp("", "modctl-export-*.sqlite")
	if err != nil {
		return "", "", 0, 0, 0, fmt.Errorf("create temp db: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()

	scopedDB, err := sql.Open("sqlite3", tmpPath+internal.DB_PRAGMAS)
	if err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("open scoped db: %w", err)
	}
	defer scopedDB.Close()

	if err := internal.MigrateDB(ctx, scopedDB); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("migrate scoped db: %w", err)
	}

	sq := dbq.New(scopedDB)

	if err := exportStore(ctx, q, sq, gi.StoreID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export store: %w", err)
	}

	if err := exportGameInstall(ctx, sq, gi); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export game install: %w", err)
	}

	if err := exportTargets(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export targets: %w", err)
	}

	archiveCount, err = exportBlobs(ctx, q, sq, gi.ID)
	if err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export blobs: %w", err)
	}

	if err := exportModPages(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export mod pages: %w", err)
	}

	if err := exportModFiles(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export mod files: %w", err)
	}

	if err := exportModFileVersions(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export mod file versions: %w", err)
	}

	if !skipInventory {
		if err := exportInventory(ctx, q, sq, gi.ID); err != nil {
			os.Remove(tmpPath)
			return "", "", 0, 0, 0, fmt.Errorf("export inventory: %w", err)
		}
	} else {
		// make sure the bundle doesn't mark blobs as inventoried
		// we explicitly excluded the inventories!
		if err := sq.ExportUnmarkInventoried(ctx); err != nil {
			os.Remove(tmpPath)
			return "", "", 0, 0, 0, fmt.Errorf("mark uninventories: %w", err)
		}
	}

	if err := exportRemapConfigs(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export remap configs: %w", err)
	}

	if err := exportProfiles(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export profiles: %w", err)
	}

	if err := exportProfileItems(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export profile items: %w", err)
	}

	if err := exportSkipBackupPatterns(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export skip-backup patterns: %w", err)
	}

	if err := exportWriteOncePatterns(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export write-once patterns: %w", err)
	}

	if err := exportProfilePathPolicies(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export profile path policies: %w", err)
	}

	if err := exportModIncompatibilities(ctx, q, sq, gi.ID); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export mod incompatibilities: %w", err)
	}

	overrideCount, err = exportOverrides(ctx, q, sq, gi.ID)
	if err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("export overrides: %w", err)
	}

	if err := scopedDB.Close(); err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("close scoped db: %w", err)
	}

	sha, err := hashFile(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", "", 0, 0, 0, fmt.Errorf("hash scoped db: %w", err)
	}

	return tmpPath, sha, archiveCount, backupCount, overrideCount, nil
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

func exportBlobs(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) (archiveCount int, err error) {
	archiveRows, err := src.ExportGetArchiveBlobsForGameInstall(ctx, gameInstallID)
	if err != nil {
		return 0, fmt.Errorf("get archive blobs: %w", err)
	}
	for _, row := range archiveRows {
		if err := dst.ExportInsertBlob(ctx, dbq.ExportInsertBlobParams(row)); err != nil {
			return 0, fmt.Errorf("insert archive blob %s: %w", row.Sha256, err)
		}
		archiveCount++
	}

	return archiveCount, nil
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

func exportOverrides(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) (int, error) {
	overrideCount := 0
	rows, err := src.ExportGetOverridesForGameInstall(ctx, gameInstallID)
	if err != nil {
		return 0, fmt.Errorf("get overrides: %w", err)
	}
	for _, row := range rows {
		if row.BlobSha256.Valid {
			overrideCount++
		}

		if err := dst.ExportInsertOverride(ctx, dbq.ExportInsertOverrideParams{
			ID:                  row.ID,
			ProfileID:           row.ProfileID,
			TargetID:            row.TargetID,
			Relpath:             row.Relpath,
			BlobSha256:          row.BlobSha256,
			OverrideType:        row.OverrideType,
			SourceArchiveSha256: row.SourceArchiveSha256,
			SourceRawPath:       row.SourceRawPath,
			SourceContentSha256: row.SourceContentSha256,
			Notes:               row.Notes,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		}); err != nil {
			return 0, fmt.Errorf("insert override %d: %w", row.ID, err)
		}

		entries, err := src.ExportGetPatchEntriesForOverride(ctx, row.ID)
		if err != nil {
			return 0, fmt.Errorf("get patch entries for override %d: %w", row.ID, err)
		}
		for _, entry := range entries {
			if err := dst.ExportInsertPatchEntry(ctx, dbq.ExportInsertPatchEntryParams(entry)); err != nil {
				return 0, fmt.Errorf("insert patch entry %d: %w", entry.ID, err)
			}
		}
	}
	return overrideCount, nil
}

func exportSkipBackupPatterns(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ExportGetSkipBackupPatternsForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get skip-backup patterns: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertSkipBackupPattern(ctx, dbq.ExportInsertSkipBackupPatternParams(row)); err != nil {
			return fmt.Errorf("insert skip-backup pattern %d: %w", row.ID, err)
		}
	}
	return nil
}

func exportWriteOncePatterns(ctx context.Context, src, dst *dbq.Queries, gameInstallID int64) error {
	rows, err := src.ExportGetWriteOncePatternsForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get write-once patterns: %w", err)
	}
	for _, row := range rows {
		if err := dst.ExportInsertWriteOncePattern(ctx, dbq.ExportInsertWriteOncePatternParams(row)); err != nil {
			return fmt.Errorf("insert write-once pattern %d: %w", row.ID, err)
		}
	}
	return nil
}

// buildGameScopedCacheDB constructs a fresh nexus cache database containing
// only rows relevant to the given game install's mod pages. Returns the path
// to the temp file and its sha256. If the cache DB does not exist or the game
// has no nexus-linked mod pages, returns empty strings and no error.
func buildGameScopedCacheDB(
	ctx context.Context,
	q *dbq.Queries,
	cacheDBPath string,
	gameInstallID int64,
) (path string, sha256hex string, err error) {
	if _, err := os.Stat(cacheDBPath); os.IsNotExist(err) {
		return "", "", nil
	}

	// Get nexus identifiers for this game's mod pages
	modPages, err := q.GetNexusLinkedModPages(ctx, gameInstallID)
	if err != nil {
		return "", "", fmt.Errorf("get nexus linked mod pages: %w", err)
	}
	if len(modPages) == 0 {
		return "", "", nil
	}

	// Open source cache DB
	srcCacheDB, err := sql.Open("sqlite3", cacheDBPath+internal.DB_PRAGMAS)
	if err != nil {
		return "", "", fmt.Errorf("open cache db: %w", err)
	}
	defer srcCacheDB.Close()

	// Create destination temp file
	tmp, err := os.CreateTemp("", "modctl-export-cache-*.sqlite")
	if err != nil {
		return "", "", fmt.Errorf("create temp cache db: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()

	dstCacheDB, err := sql.Open("sqlite3", tmpPath+internal.DB_PRAGMAS)
	if err != nil {
		os.Remove(tmpPath)
		return "", "", fmt.Errorf("open scoped cache db: %w", err)
	}
	defer dstCacheDB.Close()

	if err := nexusclient.InitCacheDB(ctx, dstCacheDB); err != nil {
		os.Remove(tmpPath)
		return "", "", fmt.Errorf("init scoped cache db: %w", err)
	}

	srcQ := dbc.New(srcCacheDB)
	dstQ := dbc.New(dstCacheDB)

	// Copy rows for each mod page
	for _, mp := range modPages {
		domain := mp.NexusGameDomain.String
		modID := mp.NexusModID.Int64

		if err := ExportCacheModInfo(ctx, srcQ, dstQ, domain, modID); err != nil {
			os.Remove(tmpPath)
			return "", "", fmt.Errorf("export cache mod info (%s/%d): %w", domain, modID, err)
		}
		if err := ExportCacheFileInfo(ctx, srcQ, dstQ, domain, modID); err != nil {
			os.Remove(tmpPath)
			return "", "", fmt.Errorf("export cache file info (%s/%d): %w", domain, modID, err)
		}
		if err := ExportCacheFileUpdates(ctx, srcQ, dstQ, domain, modID); err != nil {
			os.Remove(tmpPath)
			return "", "", fmt.Errorf("export cache file updates (%s/%d): %w", domain, modID, err)
		}
	}

	if err := dstCacheDB.Close(); err != nil {
		os.Remove(tmpPath)
		return "", "", fmt.Errorf("close scoped cache db: %w", err)
	}

	sha, err := hashFile(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", "", fmt.Errorf("hash scoped cache db: %w", err)
	}

	return tmpPath, sha, nil
}

func ExportCacheModInfo(ctx context.Context, src, dst *dbc.Queries, domain string, modID int64) error {
	row, err := src.GetNexusModInfo(ctx, dbc.GetNexusModInfoParams{
		NexusGameDomain: domain,
		NexusModID:      modID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return dst.UpsertNexusModInfo(ctx, dbc.UpsertNexusModInfoParams(row))
}

func ExportCacheFileInfo(ctx context.Context, src, dst *dbc.Queries, domain string, modID int64) error {
	rows, err := src.GetNexusFileInfoForMod(ctx, dbc.GetNexusFileInfoForModParams{
		NexusGameDomain: domain,
		NexusModID:      modID,
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := dst.UpsertNexusFileInfo(ctx, dbc.UpsertNexusFileInfoParams(row)); err != nil {
			return err
		}
	}
	return nil
}

func ExportCacheFileUpdates(ctx context.Context, src, dst *dbc.Queries, domain string, modID int64) error {
	rows, err := src.GetNexusFileUpdatesForMod(ctx, dbc.GetNexusFileUpdatesForModParams{
		NexusGameDomain: domain,
		NexusModID:      modID,
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := dst.UpsertNexusFileUpdate(ctx, dbc.UpsertNexusFileUpdateParams(row)); err != nil {
			return err
		}
	}
	return nil
}
