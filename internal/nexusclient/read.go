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
	"errors"
	"fmt"
	"time"

	"github.com/mfinelli/modctl/internal/nexusclient/dbc"
)

func (c *Client) GetModCached(gameDomain string, modID int) (*ModInfo, error) {
	q := dbc.New(c.cacheDB)

	row, err := q.GetNexusModInfo(c.ctx, dbc.GetNexusModInfoParams{
		NexusGameDomain: gameDomain,
		NexusModID:      int64(modID),
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("reading nexus mod info cache: %w", err)
	}

	if err == nil {
		fetchedAt, parseErr := time.Parse(time.RFC3339, row.FetchedAt)
		if parseErr == nil && time.Since(fetchedAt) < modInfoTTL {
			c.logger.Debug("nexus mod info cache hit",
				"game_domain", gameDomain,
				"mod_id", modID,
				"fetched_at", fetchedAt,
			)
			return modInfoFromRow(row), nil
		}
		c.logger.Debug("nexus mod info cache stale or unparseable, refreshing",
			"game_domain", gameDomain,
			"mod_id", modID,
		)
	} else {
		c.logger.Debug("nexus mod info cache miss, fetching",
			"game_domain", gameDomain,
			"mod_id", modID,
		)
	}

	info, err := c.GetMod(gameDomain, modID)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func (c *Client) GetModFilesCached(gameDomain string, modID int) (*ModFilesResponse, error) {
	q := dbc.New(c.cacheDB)

	rows, err := q.GetNexusFileInfoForMod(c.ctx, dbc.GetNexusFileInfoForModParams{
		NexusGameDomain: gameDomain,
		NexusModID:      int64(modID),
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("reading nexus file info cache: %w", err)
	}

	if len(rows) > 0 {
		// use rows[0].FetchedAt to check staleness which is fine since
		// all rows for a mod are written atomically with the same
		// fetched_at in cacheModFiles
		fetchedAt, parseErr := time.Parse(time.RFC3339, rows[0].FetchedAt)
		if parseErr == nil && time.Since(fetchedAt) < modFilesTTL {
			c.logger.Debug("nexus file info cache hit",
				"game_domain", gameDomain,
				"mod_id", modID,
				"fetched_at", fetchedAt,
			)
			updateRows, err := q.GetNexusFileUpdatesForMod(c.ctx, dbc.GetNexusFileUpdatesForModParams{
				NexusGameDomain: gameDomain,
				NexusModID:      int64(modID),
			})
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("reading nexus file updates cache: %w", err)
			}
			return modFilesResponseFromRows(rows, updateRows), nil
		}
		c.logger.Debug("nexus file info cache stale, refreshing",
			"game_domain", gameDomain,
			"mod_id", modID,
		)
	} else {
		c.logger.Debug("nexus file info cache miss, fetching",
			"game_domain", gameDomain,
			"mod_id", modID,
		)
	}

	resp, err := c.GetModFiles(gameDomain, modID)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// modInfoFromRow converts a dbc.NexusModInfo row back to a ModInfo struct
func modInfoFromRow(row dbc.NexusModInfo) *ModInfo {
	return &ModInfo{
		ModID:       int(row.NexusModID),
		Name:        row.Name.String,
		Summary:     row.Summary.String,
		Author:      row.Author.String,
		DomainName:  row.NexusGameDomain,
		IsAvailable: row.IsAvailable.Int64 == 1,
		RawJSON:     []byte(row.RawJson.String),
	}
}

// modFilesResponseFromRows converts cache rows back to a ModFilesResponse
func modFilesResponseFromRows(fileRows []dbc.NexusFileInfo, updateRows []dbc.NexusFileUpdate) *ModFilesResponse {
	files := make([]ModFileInfo, 0, len(fileRows))
	for _, r := range fileRows {
		files = append(files, ModFileInfo{
			FileID:            int(r.NexusFileID),
			Name:              r.Name.String,
			Version:           r.Version.String,
			CategoryName:      r.CategoryName.String,
			IsPrimary:         r.IsPrimary.Int64 == 1,
			FileName:          r.FileName.String,
			SizeInBytes:       r.SizeInBytes.Int64,
			UploadedTimestamp: r.UploadedTimestamp.Int64,
		})
	}

	updates := make([]FileUpdateInfo, 0, len(updateRows))
	for _, r := range updateRows {
		updates = append(updates, FileUpdateInfo{
			OldFileID:         int(r.OldFileID),
			NewFileID:         int(r.NewFileID),
			UploadedTimestamp: r.UploadedTimestamp,
		})
	}

	return &ModFilesResponse{
		Files:       files,
		FileUpdates: updates,
	}
}
