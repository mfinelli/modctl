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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mfinelli/modctl/internal/nexusclient/dbc"
	"github.com/mfinelli/modctl/internal/updatechain"
)

// CacheReader provides read-only access to the Nexus cache DB.
// Use this when you need cache data but don't need to make API calls.
type CacheReader struct {
	ctx    context.Context
	db     *sql.DB
	logger *slog.Logger
}

func NewCacheReader(ctx context.Context, logger *slog.Logger) (*CacheReader, error) {
	db, err := openCacheDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening nexus cache db: %w", err)
	}
	return &CacheReader{
		ctx:    ctx,
		db:     db,
		logger: logger,
	}, nil
}

func (r *CacheReader) Close() error {
	return r.db.Close()
}

func (r *CacheReader) GetNexusFileInfo(gameDomain string, modID int64, fileID int64) (*dbc.GetNexusFileInfoRow, error) {
	q := dbc.New(r.db)
	row, err := q.GetNexusFileInfo(r.ctx, dbc.GetNexusFileInfoParams{
		NexusGameDomain: gameDomain,
		NexusModID:      modID,
		NexusFileID:     fileID,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *CacheReader) GetNexusFileUpdateChain(gameDomain string, modID int64) ([]dbc.GetNexusFileUpdateChainRow, error) {
	q := dbc.New(r.db)
	return q.GetNexusFileUpdateChain(r.ctx, dbc.GetNexusFileUpdateChainParams{
		NexusGameDomain: gameDomain,
		NexusModID:      modID,
	})
}

// GetUpdateChain returns the file update chain that is cached for a mod. A mod
// that has nothing cached has an empty chain, in which every file is the
// latest.
func (r *CacheReader) GetUpdateChain(gameDomain string, modID int64) (updatechain.Chain, error) {
	rows, err := r.GetNexusFileUpdateChain(gameDomain, modID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return updatechain.Chain{}, err
	}

	updates := make([]updatechain.Update, 0, len(rows))
	for _, row := range rows {
		updates = append(updates, updatechain.Update{
			OldFileID: row.OldFileID,
			NewFileID: row.NewFileID,
		})
	}

	return updatechain.New(updates), nil
}

func (r *CacheReader) GetNexusModInfo(gameDomain string, modID int64) (*dbc.NexusModInfo, error) {
	q := dbc.New(r.db)
	row, err := q.GetNexusModInfo(r.ctx, dbc.GetNexusModInfoParams{
		NexusGameDomain: gameDomain,
		NexusModID:      modID,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *CacheReader) GetNexusFileInfoFetchedAt(gameDomain string, modID int64) (string, error) {
	q := dbc.New(r.db)
	return q.GetNexusFileInfoFetchedAt(r.ctx, dbc.GetNexusFileInfoFetchedAtParams{
		NexusGameDomain: gameDomain,
		NexusModID:      modID,
	})
}
