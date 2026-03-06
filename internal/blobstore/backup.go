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

package blobstore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mfinelli/modctl/dbq"
)

// BackupResult is the outcome of a successful IngestBackup call
type BackupResult struct {
	SHA256Hex string
	SizeBytes int64
	// Existed is true if the blob was already in the backup store (deduped)
	Existed bool
}

// IngestBackup copies srcPath into the backup blob store, records the blob,
// and upserts the backups row - all within a single transaction.
//
// srcPath is the on-disk path of the pre-existing file to back up.
// originalContentSha256 is the hash of the file computed before ingestion,
// if already available; pass empty string if not.
func IngestBackup(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	bs Store,
	srcPath string,
	gameInstallID int64,
	targetID int64,
	relpath string,
	operationID sql.NullInt64,
	originalContentSha256 string,
) (BackupResult, error) {
	// Ingest the file into the backup blob store first - this is outside the
	// transaction since it's a filesystem operation and IngestFile is
	// idempotent (content-addressed, atomic rename into place).
	ingestResult, err := bs.IngestFile(ctx, KindBackup, srcPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("ingest backup file: %w", err)
	}

	var origSha sql.NullString
	if originalContentSha256 != "" {
		origSha = sql.NullString{String: originalContentSha256, Valid: true}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return BackupResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := q.WithTx(tx)

	if err := EnsureBlobRecorded(
		ctx,
		qtx,
		ingestResult.SHA256Hex,
		string(KindBackup),
		ingestResult.SizeBytes,
		nil,
	); err != nil {
		return BackupResult{}, fmt.Errorf("record backup blob: %w", err)
	}

	if err := qtx.UpsertBackup(ctx, dbq.UpsertBackupParams{
		GameInstallID:         gameInstallID,
		TargetID:              targetID,
		Relpath:               relpath,
		BackupBlobSha256:      ingestResult.SHA256Hex,
		OriginalContentSha256: origSha,
		SizeBytes:             ingestResult.SizeBytes,
		CreatedByOperationID:  operationID,
	}); err != nil {
		return BackupResult{}, fmt.Errorf("upsert backup row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return BackupResult{}, fmt.Errorf("commit: %w", err)
	}

	return BackupResult{
		SHA256Hex: ingestResult.SHA256Hex,
		SizeBytes: ingestResult.SizeBytes,
		Existed:   ingestResult.Existed,
	}, nil
}
