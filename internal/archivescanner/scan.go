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
	Skipped int
	Failed  int
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

	var result ScanAllResult

	for _, archive := range archives {
		log := logger.With("sha256", archive.Sha256, "original_name", archive.OriginalName)

		archivePath, err := store.PathFor(blobstore.KindArchive, archive.Sha256)
		if err != nil {
			log.Warn("could not resolve blob path, skipping", "err", err)
			result.Skipped++
			continue
		}

		scanResult, err := scanner.Scan(ctx, archivePath)
		if err != nil {
			log.Warn("bsdtar scan failed, skipping", "err", err)
			result.Failed++
			continue
		}

		if scanResult.Warnings != "" {
			log.Warn("bsdtar warnings during scan", "warnings", scanResult.Warnings)
		}

		if err := commitArchiveInventory(ctx, sqldb, archive.Sha256, scanResult.Entries, log); err != nil {
			log.Warn("failed to commit inventory, skipping", "err", err)
			result.Failed++
			continue
		}

		log.Info("scanned archive", "entries", len(scanResult.Entries))
		result.Scanned++
	}

	return result, nil
}

// commitArchiveInventory inserts all entries for a single archive and marks
// it as scanned within a single transaction.
func commitArchiveInventory(
	ctx context.Context,
	sqldb *sql.DB,
	archiveSha256 string,
	entries []Entry,
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

	if err := qtx.MarkArchiveInventoryScanned(ctx, archiveSha256); err != nil {
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
