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

package nexusclient

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mfinelli/modctl/internal/nexusclient/dbc"
)

const (
	ModInfoTTL  = 7 * 24 * time.Hour
	ModFilesTTL = 24 * time.Hour
)

func (c *Client) cacheModInfo(info *ModInfo) error {
	q := dbc.New(c.db)
	err := q.UpsertNexusModInfo(c.ctx, dbc.UpsertNexusModInfoParams{
		NexusGameDomain: info.DomainName,
		NexusModID:      info.ModID,
		FetchedAt:       time.Now().UTC().Format(time.RFC3339),
		Name:            sqlNullString(info.Name),
		Summary:         sqlNullString(info.Summary),
		Author:          sqlNullString(info.Author),
		IsAvailable:     sqlNullBool(info.IsAvailable),
		RawJson:         sqlNullString(string(info.RawJSON)),
	})
	if err != nil {
		return fmt.Errorf("upserting nexus mod info cache: %w", err)
	}
	return nil
}

func (c *Client) cacheModFiles(gameDomain string, modID int64, resp *ModFilesResponse) error {
	fetchedAt := time.Now().UTC().Format(time.RFC3339)

	tx, err := c.db.BeginTx(c.ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	q := dbc.New(tx)

	if err := q.DeleteNexusFileInfoForMod(c.ctx, dbc.DeleteNexusFileInfoForModParams{
		NexusGameDomain: gameDomain,
		NexusModID:      modID,
	}); err != nil {
		return fmt.Errorf("deleting stale nexus file info cache: %w", err)
	}
	if err := q.DeleteNexusFileUpdatesForMod(c.ctx, dbc.DeleteNexusFileUpdatesForModParams{
		NexusGameDomain: gameDomain,
		NexusModID:      modID,
	}); err != nil {
		return fmt.Errorf("deleting stale nexus file updates cache: %w", err)
	}

	for _, f := range resp.Files {
		if err := q.UpsertNexusFileInfo(c.ctx, dbc.UpsertNexusFileInfoParams{
			NexusGameDomain:   gameDomain,
			NexusModID:        modID,
			NexusFileID:       f.FileID,
			FetchedAt:         fetchedAt,
			Name:              sqlNullString(f.Name),
			Version:           sqlNullString(f.Version),
			CategoryName:      sqlNullString(f.CategoryName),
			IsPrimary:         sqlNullBool(f.IsPrimary),
			FileName:          sqlNullString(f.FileName),
			SizeInBytes:       sqlNullInt64(f.SizeInBytes),
			UploadedTimestamp: sqlNullInt64(f.UploadedTimestamp),
			RawJson:           sqlNullString(string(resp.RawJSON)),
		}); err != nil {
			return fmt.Errorf("upserting nexus file info for file %d: %w", f.FileID, err)
		}
	}

	for _, u := range resp.FileUpdates {
		if err := q.UpsertNexusFileUpdate(c.ctx, dbc.UpsertNexusFileUpdateParams{
			NexusGameDomain:   gameDomain,
			NexusModID:        modID,
			OldFileID:         u.OldFileID,
			NewFileID:         u.NewFileID,
			UploadedTimestamp: u.UploadedTimestamp,
			FetchedAt:         fetchedAt,
		}); err != nil {
			return fmt.Errorf("upserting nexus file update for old_file_id %d: %w", u.OldFileID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing file cache transaction: %w", err)
	}

	return nil
}

// helpers for nullable types
// TODO we have these elsewhere we need a single copy
func sqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func sqlNullBool(b bool) sql.NullInt64 {
	if b {
		return sql.NullInt64{Int64: 1, Valid: true}
	}
	return sql.NullInt64{Int64: 0, Valid: true}
}

func sqlNullInt64(i int64) sql.NullInt64 {
	return sql.NullInt64{Int64: i, Valid: true}
}
