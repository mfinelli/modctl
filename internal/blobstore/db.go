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
	"time"

	"github.com/mfinelli/modctl/dbq"
)

// EnsureBlobRecorded ensures the blobs table has a row for sha256,
// enforcing invariants:
//   - if the blob already exists, its kind and size must match
//   - otherwise, insert it
//
// verified_at is set only on insert. For existing blobs, verified_at is
// reserved for doctor --deep (rehash verification), not for "we saw a file".
func EnsureBlobRecorded(
	ctx context.Context,
	q *dbq.Queries,
	sha256 string,
	kind string,
	sizeBytes int64,
	originalName *string,
) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	existing, err := q.GetBlob(ctx, sha256)
	if err == nil {
		if existing.Kind != kind {
			return fmt.Errorf(
				"blob invariant violation: sha256=%s exists with kind=%s, expected kind=%s",
				sha256, existing.Kind, kind,
			)
		}
		if existing.SizeBytes != sizeBytes {
			return fmt.Errorf(
				"blob invariant violation: sha256=%s exists with size_bytes=%d, expected size_bytes=%d",
				sha256, existing.SizeBytes, sizeBytes,
			)
		}
		// Do not update verified_at here. That is only updated by deep verification.
		return nil
	}

	if err != sql.ErrNoRows {
		return fmt.Errorf("get blob: %w", err)
	}

	var orig sql.NullString
	if originalName != nil && *originalName != "" {
		orig = sql.NullString{String: *originalName, Valid: true}
	}

	if err := q.InsertBlob(ctx, dbq.InsertBlobParams{
		Sha256:       sha256,
		Kind:         kind,
		SizeBytes:    sizeBytes,
		OriginalName: orig,
		VerifiedAt:   sql.NullString{String: now, Valid: true},
	}); err != nil {
		return fmt.Errorf("insert blob: %w", err)
	}

	return nil
}
