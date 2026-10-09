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

package internal

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mfinelli/modctl/dbq"
)

// CopyDeployRules copies skip-backup and write-once patterns from one profile
// item to another, in one transaction. If the destination already has patterns
// of a kind that the source also has, they are replaced. If the source has no
// patterns for a given kind that kind is a no-op and the destination keeps what
// it has. It returns the number of patterns copied for each kind.
func CopyDeployRules(ctx context.Context, db *sql.DB, q *dbq.Queries, srcItemID, dstItemID int64) (skipBackup, writeOnce int, err error) {
	skipBackupPatterns, err := q.ListSkipBackupPatterns(ctx, srcItemID)
	if err != nil {
		return 0, 0, fmt.Errorf("list source skip-backup patterns: %w", err)
	}

	writeOncePatterns, err := q.ListWriteOncePatterns(ctx, srcItemID)
	if err != nil {
		return 0, 0, fmt.Errorf("list source write-once patterns: %w", err)
	}

	if len(skipBackupPatterns) == 0 && len(writeOncePatterns) == 0 {
		return 0, 0, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	// Replace destination skip-backup patterns, but only if there is
	// something to replace them with: a kind the source has none of is left
	// alone, as it is by CopySkipBackupPatterns.
	if len(skipBackupPatterns) > 0 {
		if err := qtx.DeleteAllSkipBackupPatterns(ctx, dstItemID); err != nil {
			return 0, 0, fmt.Errorf("clear dst skip-backup patterns: %w", err)
		}
		for _, p := range skipBackupPatterns {
			if err := qtx.AddSkipBackupPattern(ctx, dbq.AddSkipBackupPatternParams{
				ProfileItemID: dstItemID,
				Pattern:       p.Pattern,
			}); err != nil {
				return 0, 0, fmt.Errorf("copy skip-backup pattern %q: %w", p.Pattern, err)
			}
		}
	}

	// Replace destination write-once patterns, on the same terms
	if len(writeOncePatterns) > 0 {
		if err := qtx.DeleteAllWriteOncePatterns(ctx, dstItemID); err != nil {
			return 0, 0, fmt.Errorf("clear dst write-once patterns: %w", err)
		}
		for _, p := range writeOncePatterns {
			if err := qtx.AddWriteOncePattern(ctx, dbq.AddWriteOncePatternParams{
				ProfileItemID: dstItemID,
				Pattern:       p.Pattern,
			}); err != nil {
				return 0, 0, fmt.Errorf("copy write-once pattern %q: %w", p.Pattern, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit: %w", err)
	}

	return len(skipBackupPatterns), len(writeOncePatterns), nil
}

// CopySkipBackupPatterns copies skip-backup patterns from one profile item to
// another within the same profile. If the destination already has patterns
// they are replaced. If the source has no patterns this is a no-op. It returns
// the number of patterns copied.
func CopySkipBackupPatterns(ctx context.Context, db *sql.DB, q *dbq.Queries, srcItemID, dstItemID int64) (copied int, err error) {
	patterns, err := q.ListSkipBackupPatterns(ctx, srcItemID)
	if err != nil {
		return 0, fmt.Errorf("list source skip-backup patterns: %w", err)
	}
	if len(patterns) == 0 {
		return 0, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	if err := qtx.DeleteAllSkipBackupPatterns(ctx, dstItemID); err != nil {
		return 0, fmt.Errorf("clear dst skip-backup patterns: %w", err)
	}
	for _, p := range patterns {
		if err := qtx.AddSkipBackupPattern(ctx, dbq.AddSkipBackupPatternParams{
			ProfileItemID: dstItemID,
			Pattern:       p.Pattern,
		}); err != nil {
			return 0, fmt.Errorf("copy skip-backup pattern %q: %w", p.Pattern, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return len(patterns), nil
}

// CopyWriteOncePatterns copies write-once patterns from one profile item to
// another within the same profile. If the destination already has patterns
// they are replaced. If the source has no patterns this is a no-op. It returns
// the number of patterns copied.
func CopyWriteOncePatterns(ctx context.Context, db *sql.DB, q *dbq.Queries, srcItemID, dstItemID int64) (copied int, err error) {
	patterns, err := q.ListWriteOncePatterns(ctx, srcItemID)
	if err != nil {
		return 0, fmt.Errorf("list source write-once patterns: %w", err)
	}
	if len(patterns) == 0 {
		return 0, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	if err := qtx.DeleteAllWriteOncePatterns(ctx, dstItemID); err != nil {
		return 0, fmt.Errorf("clear dst write-once patterns: %w", err)
	}
	for _, p := range patterns {
		if err := qtx.AddWriteOncePattern(ctx, dbq.AddWriteOncePatternParams{
			ProfileItemID: dstItemID,
			Pattern:       p.Pattern,
		}); err != nil {
			return 0, fmt.Errorf("copy write-once pattern %q: %w", p.Pattern, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return len(patterns), nil
}
