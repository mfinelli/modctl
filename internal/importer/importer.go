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

package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
)

type ImportOptions struct {
	GameInstallID int64
	ArchivePath   string

	NexusURL        *string // optional nexus link
	NexusGameDomain *string
	NexusModID      *int64

	PageID    *int64  // optional attach to existing mod_page
	ModName   *string // optional override for mod_pages.name
	FileLabel *string // optional override for mod_files.label

	Wrapped     bool
	WrappedFrom string
	MemberName  string

	// what to store into blobs.original_name / mod_file_versions.original_name
	OriginalBasename string
}

func ImportArchive(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	bs blobstore.Store,
	opts ImportOptions,
) (pageID, fileID, versionID int64, sha string, size int64, err error) {
	// 1) Ingest archive into blob store (outside TX - filesystem first)
	// Why ingest happens before the transaction:
	//   - Filesystem is authoritative for blob content
	//   - We don't want a DB row if ingest fails
	//   - If DB fails after ingest, blob is just an unreferenced archive
	//     and future GC can handle it
	res, err := bs.IngestFile(ctx, blobstore.KindArchive, opts.ArchivePath)
	if err != nil {
		return 0, 0, 0, "", 0, fmt.Errorf("ingest archive: %w", err)
	}

	sha = res.SHA256Hex
	size = res.SizeBytes

	// Derive original filename
	base := filepath.Base(opts.ArchivePath)

	// 2) Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, "", 0, fmt.Errorf("error starting transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	// 3) Ensure blob row exists
	if err := blobstore.EnsureBlobRecorded(
		ctx,
		qtx,
		sha,
		string(blobstore.KindArchive),
		size,
		&base,
	); err != nil {
		return 0, 0, 0, "", 0, err
	}

	// 4) Determine mod page name
	pageName := base
	if opts.ModName != nil && *opts.ModName != "" {
		pageName = *opts.ModName
	}

	sourceKind := "local"
	if opts.NexusGameDomain != nil && opts.NexusModID != nil {
		sourceKind = "nexus"
	}

	// 5) Decide mod_page_id (create mod_page if necessary)
	switch {
	case opts.PageID != nil && *opts.PageID != 0:
		// Validate the page belongs to the game install
		p, err := qtx.GetModPageForGame(ctx, dbq.GetModPageForGameParams{
			ID:            *opts.PageID,
			GameInstallID: opts.GameInstallID,
		})
		if err != nil {
			if err == sql.ErrNoRows {
				return 0, 0, 0, "", 0, fmt.Errorf("mod page %d not found for this game", *opts.PageID)
			}
			return 0, 0, 0, "", 0, fmt.Errorf("get mod page: %w", err)
		}
		pageID = p.ID

	default:
		// If nexus-url provided (and parsed), attempt to reuse the existing nexus page
		if opts.NexusGameDomain != nil && opts.NexusModID != nil {
			p, err := qtx.GetModPageByNexus(ctx, dbq.GetModPageByNexusParams{
				GameInstallID:   opts.GameInstallID,
				NexusGameDomain: nullString(opts.NexusGameDomain),
				NexusModID:      nullInt64(opts.NexusModID),
			})
			if err == nil {
				pageID = p.ID
			} else if err != sql.ErrNoRows {
				return 0, 0, 0, "", 0, fmt.Errorf("lookup nexus mod page: %w", err)
			}
		}

		// If not found, create a new page
		if pageID == 0 {
			pageID, err = qtx.CreateModPage(ctx, dbq.CreateModPageParams{
				GameInstallID:   opts.GameInstallID,
				Name:            pageName,
				SourceKind:      sourceKind, // "nexus" if nexus fields set, else "local"
				SourceUrl:       nullString(opts.NexusURL),
				SourceRef:       sql.NullString{Valid: false},
				NexusGameDomain: nullString(opts.NexusGameDomain),
				NexusModID:      nullInt64(opts.NexusModID),
				Notes:           sql.NullString{Valid: false},
				Metadata:        sql.NullString{Valid: false},
			})
			if err != nil {
				return 0, 0, 0, "", 0, fmt.Errorf("create mod_page: %w", err)
			}
		}
	}

	// 6) Create mod_file
	label := "Main File"
	if opts.FileLabel != nil && *opts.FileLabel != "" {
		label = *opts.FileLabel
	}

	// Decide mod_file_id by label (find-or-create)
	mf, err := qtx.GetModFileByLabel(ctx, dbq.GetModFileByLabelParams{
		ModPageID: pageID,
		Label:     label,
	})
	if err == nil {
		fileID = mf.ID
	} else if err != sql.ErrNoRows {
		return 0, 0, 0, "", 0, fmt.Errorf("lookup mod_file: %w", err)
	} else {
		// is_primary=true only for the first file created under this page
		cnt, err := qtx.CountModFilesForPage(ctx, pageID)
		if err != nil {
			return 0, 0, 0, "", 0, fmt.Errorf("count mod_files: %w", err)
		}
		isPrimary := int64(0)
		if cnt == 0 {
			isPrimary = 1
		}

		fileID, err = qtx.CreateModFile(ctx, dbq.CreateModFileParams{
			ModPageID:   pageID,
			Label:       label,
			IsPrimary:   isPrimary,
			NexusFileID: sql.NullInt64{Valid: false}, // we don't have file_id from nexus-url
			SourceUrl:   nullString(opts.NexusURL),
			Metadata:    sql.NullString{Valid: false},
		})
		if err != nil {
			return 0, 0, 0, "", 0, fmt.Errorf("create mod_file: %w", err)
		}
	}

	var m sql.NullString
	if opts.Wrapped {
		meta := map[string]any{
			"wrapped":             true,
			"wrapped_from":        opts.WrappedFrom,
			"wrapped_member_name": opts.MemberName,
		}
		b, jerr := json.Marshal(meta)
		m = sql.NullString{String: string(b), Valid: true}
		if jerr != nil {
			return 0, 0, 0, "", 0, fmt.Errorf("creating wrapped json: %w", err)
		}
	}

	// 7) Create mod_file_version
	versionID, err = qtx.CreateModFileVersion(ctx, dbq.CreateModFileVersionParams{
		ModFileID:     fileID,
		ArchiveSha256: sha,
		OriginalName:  nullString(&opts.OriginalBasename),
		VersionString: sql.NullString{Valid: false},
		UploadedAt:    sql.NullString{Valid: false},
		UpstreamNotes: sql.NullString{Valid: false},
		Notes:         sql.NullString{Valid: false},
		Metadata:      m,
	})
	if err != nil {
		return 0, 0, 0, "", 0, fmt.Errorf("create mod_file_version: %w", err)
	}

	// 8) Commit
	if err := tx.Commit(); err != nil {
		return 0, 0, 0, "", 0, fmt.Errorf("commit import: %w", err)
	}

	return pageID, fileID, versionID, sha, size, nil
}

func nullString(s *string) sql.NullString {
	if s == nil || *s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullInt64(i *int64) sql.NullInt64 {
	if i == nil {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: *i, Valid: true}
}
