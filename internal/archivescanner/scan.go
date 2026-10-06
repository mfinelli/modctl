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

package archivescanner

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
)

// ScanAllResult summarizes the outcome of a ScanAll run
type ScanAllResult struct {
	Scanned int
	Failed  int
}

// ScanOne scans a single archive by sha256 and commits its inventory in a
// single transaction. If the archive has already been inventoried it is a
// no-op. Returns an error only if the scan or commit fails - the caller is
// responsible for deciding how loudly to surface that.
func ScanOne(
	ctx context.Context,
	sqldb *sql.DB,
	queries *dbq.Queries,
	store blobstore.Store,
	scanner Scanner,
	archiveSha256 string,
	logger *slog.Logger,
) error {
	log := logger.With("sha256", archiveSha256)

	already, err := queries.IsArchiveInventoried(ctx, archiveSha256)
	if err != nil {
		return fmt.Errorf("checking inventory status: %w", err)
	}
	if already {
		log.Info("archive already inventoried, skipping")
		// Still mark the new mod_file_versions row as scanned
		if err := queries.MarkArchiveInventoryScanned(ctx, archiveSha256); err != nil {
			log.Warn("failed to mark archive as scanned", "err", err)
		}
		return nil
	}

	return scanArchive(ctx, sqldb, store, scanner, archiveSha256, log, false)
}

// RescanOne re-reads a single archive and replaces its recorded inventory,
// even if it was already inventoried. The archive is scanned first and only
// then is the old inventory swapped for the new one in a single short
// transaction, so a failed scan leaves the existing inventory untouched and
// the database write lock is never held while bsdtar runs. Content hashes
// cached on the old entries are carried over where they are still valid, and
// the scanned timestamp of every version of the archive is refreshed.
func RescanOne(
	ctx context.Context,
	sqldb *sql.DB,
	store blobstore.Store,
	scanner Scanner,
	archiveSha256 string,
	logger *slog.Logger,
) error {
	log := logger.With("sha256", archiveSha256)
	return scanArchive(ctx, sqldb, store, scanner, archiveSha256, log, true)
}

// scanArchive runs bsdtar against an archive and commits the result. With
// replace set, any existing inventory for the archive is replaced; otherwise
// the archive is expected to have none.
func scanArchive(
	ctx context.Context,
	sqldb *sql.DB,
	store blobstore.Store,
	scanner Scanner,
	archiveSha256 string,
	log *slog.Logger,
	replace bool,
) error {
	archivePath, err := store.PathFor(blobstore.KindArchive, archiveSha256)
	if err != nil {
		return fmt.Errorf("resolving blob path: %w", err)
	}

	scanResult, err := scanner.Scan(ctx, archivePath)
	if err != nil {
		return fmt.Errorf("bsdtar scan failed: %w", err)
	}

	if scanResult.Warnings != "" {
		log.Warn("bsdtar warnings during scan", "warnings", scanResult.Warnings)
	}

	if err := commitArchiveInventory(ctx, sqldb, archiveSha256, scanResult.Entries, replace, log); err != nil {
		return fmt.Errorf("committing inventory: %w", err)
	}

	if replace {
		log.Info("rescanned archive", "entries", len(scanResult.Entries))
	} else {
		log.Info("scanned archive", "entries", len(scanResult.Entries))
	}
	return nil
}

// ScanAll scans all archives that have not yet had their inventory populated
// Each archive is scanned and committed in its own transaction so progress is
// saved as we go - a failure on one archive does not affect others
// Individual archive failures are logged but do not abort the run
func ScanAll(
	ctx context.Context,
	sqldb *sql.DB,
	queries *dbq.Queries,
	store blobstore.Store,
	scanner Scanner,
	logger *slog.Logger,
) (ScanAllResult, error) {
	archives, err := queries.ListUnscannedArchives(ctx)
	if err != nil {
		return ScanAllResult{}, fmt.Errorf("listing unscanned archives: %w", err)
	}

	refs := make([]archiveRef, len(archives))
	for i, a := range archives {
		refs[i] = archiveRef{sha256: a.Sha256, originalName: a.OriginalName.String}
	}

	return scanEach(refs, logger, func(sha256 string) error {
		return ScanOne(ctx, sqldb, queries, store, scanner, sha256, logger)
	}), nil
}

// RescanAll re-reads every archive in the blob store and replaces its
// recorded inventory (see RescanOne). Each archive is handled in its own
// transaction, and individual failures are logged but do not abort the run;
// an archive that fails keeps its existing inventory.
func RescanAll(
	ctx context.Context,
	sqldb *sql.DB,
	queries *dbq.Queries,
	store blobstore.Store,
	scanner Scanner,
	logger *slog.Logger,
) (ScanAllResult, error) {
	archives, err := queries.ListArchiveBlobs(ctx)
	if err != nil {
		return ScanAllResult{}, fmt.Errorf("listing archives: %w", err)
	}

	refs := make([]archiveRef, len(archives))
	for i, a := range archives {
		refs[i] = archiveRef{sha256: a.Sha256, originalName: a.OriginalName.String}
	}

	return scanEach(refs, logger, func(sha256 string) error {
		return RescanOne(ctx, sqldb, store, scanner, sha256, logger)
	}), nil
}

// archiveRef identifies an archive for logging purposes
type archiveRef struct {
	sha256       string
	originalName string
}

// scanEach applies scan to each archive, counting successes and failures.
// A failure is logged and does not stop the run.
func scanEach(archives []archiveRef, logger *slog.Logger, scan func(sha256 string) error) ScanAllResult {
	var result ScanAllResult

	for _, archive := range archives {
		if err := scan(archive.sha256); err != nil {
			logger.Warn("failed to scan archive, skipping",
				"sha256", archive.sha256,
				"original_name", archive.originalName,
				"err", err,
			)
			result.Failed++
			continue
		}
		result.Scanned++
	}

	return result
}

// commitArchiveInventory inserts all entries for a single archive and marks
// it as scanned within a single transaction. With replace set, the archive's
// existing entries are deleted first (within the same transaction), cached
// content hashes that are still valid are restored onto the new entries, and
// the scanned timestamp of every version of the archive is refreshed.
func commitArchiveInventory(
	ctx context.Context,
	sqldb *sql.DB,
	archiveSha256 string,
	entries []Entry,
	replace bool,
	log *slog.Logger,
) error {
	tx, err := sqldb.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			log.Warn("failed to rollback transaction", "err", err)
		}
	}()

	qtx := dbq.New(tx)

	var cached []cachedHash
	if replace {
		rows, err := qtx.ListHashedInventoryEntriesForArchive(ctx, archiveSha256)
		if err != nil {
			return fmt.Errorf("reading cached content hashes: %w", err)
		}
		for _, r := range rows {
			if !r.ContentSha256.Valid || !r.SizeBytes.Valid {
				continue
			}
			cached = append(cached, cachedHash{
				Position: r.Position,
				Type:     r.EntryType,
				Size:     r.SizeBytes.Int64,
				Sha256:   r.ContentSha256.String,
			})
		}

		if err := qtx.DeleteInventoryEntriesForArchive(ctx, archiveSha256); err != nil {
			return fmt.Errorf("deleting existing inventory: %w", err)
		}
	}

	for _, e := range entries {
		params := dbq.InsertArchiveInventoryEntryParams{
			ArchiveSha256: archiveSha256,
			RawPath:       toNullString(e.RawPath),
			EntryType:     string(e.Type),
			SizeBytes:     toNullInt64(e.SizeBytes, e.RawPath != ""),
			LinkTarget:    toNullString(e.LinkTarget),
			Position:      int64(e.Position),
			ParseError:    toNullString(e.ParseError),
		}
		if err := qtx.InsertArchiveInventoryEntry(ctx, params); err != nil {
			return fmt.Errorf("inserting entry at position %d (%q): %w", e.Position, e.RawPath, err)
		}
	}

	if replace {
		for position, sha := range carryForwardHashes(cached, entries) {
			if err := qtx.SetInventoryEntryContentSha256(ctx, dbq.SetInventoryEntryContentSha256Params{
				ContentSha256: sql.NullString{String: sha, Valid: true},
				ArchiveSha256: archiveSha256,
				Position:      position,
			}); err != nil {
				return fmt.Errorf("restoring content hash at position %d: %w", position, err)
			}
		}

		if err := qtx.RefreshArchiveInventoryScanned(ctx, archiveSha256); err != nil {
			return fmt.Errorf("refreshing scanned timestamp: %w", err)
		}
	} else if err := qtx.MarkArchiveInventoryScanned(ctx, archiveSha256); err != nil {
		return fmt.Errorf("marking archive as scanned: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// TODO we already have this move to util and keep one copy
func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// TODO we already have this move to util and keep one copy
func toNullInt64(v int64, valid bool) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: valid}
}
