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
	"fmt"
	"os"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
)

// Full performs a full export of the entire modctl state.
func Full(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	bs blobstore.Store,
	opts Options,
) error {
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

	// 1. Snapshot the database into a temp file
	dbPath, dbSha256, err := snapshotDB(ctx, db)
	if err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	defer os.Remove(dbPath)

	// 2. Get schema version
	schemaVersion, err := currentSchemaVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("get schema version: %w", err)
	}

	// 3. Collect all blobs
	archiveBlobs, err := q.ListBlobsByKind(ctx, string(blobstore.KindArchive))
	if err != nil {
		return fmt.Errorf("list archive blobs: %w", err)
	}
	backupBlobs, err := q.ListBlobsByKind(ctx, string(blobstore.KindBackup))
	if err != nil {
		return fmt.Errorf("list backup blobs: %w", err)
	}

	// 4. Write manifest
	manifest := Manifest{
		ExportFormatVersion: ExportFormatVersion,
		ExportKind:          ExportKindFull,
		ExportedAt:          time.Now().UTC(),
		ModctlVersion:       opts.ModctlVersion,
		SchemaVersion:       schemaVersion,
		DBSha256:            dbSha256,
		Counts: ManifestCounts{
			Archives: len(archiveBlobs),
			Backups:  len(backupBlobs),
		},
	}
	if err := writeManifest(tw, manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// 5. Write database snapshot
	if err := writeFileToTar(tw, dbPath, DatabaseFilename); err != nil {
		return fmt.Errorf("write database: %w", err)
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

	// 7. Write backup blobs
	for _, b := range backupBlobs {
		skip, err := writeBlobToTar(ctx, tw, bs, blobstore.KindBackup, b.Sha256)
		if err != nil {
			return fmt.Errorf("write backup blob %s: %w", b.Sha256, err)
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

// snapshotDB uses the SQLite backup API to create a consistent snapshot.
// Returns the path to the temp file and its sha256.
func snapshotDB(ctx context.Context, db *sql.DB) (path string, sha256hex string, err error) {
	tmp, err := os.CreateTemp("", "modctl-export-db-*.sqlite")
	if err != nil {
		return "", "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()

	// Use SQLite's VACUUM INTO for a clean consistent copy
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", tmpPath); err != nil {
		os.Remove(tmpPath)
		return "", "", fmt.Errorf("vacuum into: %w", err)
	}

	sha, err := hashFile(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", "", fmt.Errorf("hash snapshot: %w", err)
	}

	return tmpPath, sha, nil
}
