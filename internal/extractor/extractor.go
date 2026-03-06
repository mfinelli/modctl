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

// Package extractor handles extraction of mod archives to a staging directory
// and deployment of files into the game target directory.
package extractor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/planner"
)

// Extractor handles archive extraction and file deployment
type Extractor struct {
	BsdtarPath string
	BlobStore  blobstore.Store
	StagingDir string
}

// DeployFileResult is the outcome of deploying a single file during apply
type DeployFileResult struct {
	DestPath      string
	ContentSha256 string
	SizeBytes     int64
	// WasBackedUp is true if a pre-existing file was backed up before writing
	WasBackedUp bool
	// BackupSha256 is set when WasBackedUp is true
	BackupSha256 string
}

// RemoveFileResult is the outcome of removing a single file during apply
type RemoveFileResult struct {
	DestPath string
}

// RestoreFileResult is the outcome of restoring a backup during apply
type RestoreFileResult struct {
	DestPath string
}

// StagingPathFor returns the staging directory for a given archive sha256
func (e Extractor) StagingPathFor(archiveSha256 string) string {
	return filepath.Join(e.StagingDir, "staging", archiveSha256)
}

// ClearStaging removes and recreates the staging root directory.
// Call this at the start of each apply run.
func (e Extractor) ClearStaging(ctx context.Context) error {
	stagingRoot := filepath.Join(e.StagingDir, "staging")
	if err := os.RemoveAll(stagingRoot); err != nil {
		return fmt.Errorf("clear staging: %w", err)
	}
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return fmt.Errorf("recreate staging root: %w", err)
	}
	return nil
}

// CleanupStaging removes the staging root directory entirely.
// Call this after a successful apply run unless --keep-staging is set.
func (e Extractor) CleanupStaging(ctx context.Context) error {
	stagingRoot := filepath.Join(e.StagingDir, "staging")
	if err := os.RemoveAll(stagingRoot); err != nil {
		return fmt.Errorf("cleanup staging: %w", err)
	}
	return nil
}

// ExtractArchive extracts all contents of the archive identified by
// archiveSha256 into a per-archive staging directory.
// Returns the staging directory path.
func (e Extractor) ExtractArchive(ctx context.Context, archiveSha256 string) (string, error) {
	archivePath, err := e.BlobStore.PathFor(blobstore.KindArchive, archiveSha256)
	if err != nil {
		return "", fmt.Errorf("resolve archive path: %w", err)
	}

	stagingPath := e.StagingPathFor(archiveSha256)
	if err := os.MkdirAll(stagingPath, 0o755); err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}

	bin := e.BsdtarPath
	if bin == "" {
		bin = "bsdtar"
	}

	cmd := exec.CommandContext(ctx, bin, "-xf", archivePath, "-C", stagingPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("bsdtar extract %q: %w\nstderr: %s",
			archivePath, err, stderr.String())
	}

	return stagingPath, nil
}

// DeployFile moves a single winning file from staging into the target
// directory. It:
//   - backs up any pre-existing non-tool-owned file if op.NeedsBackup is true
//   - copies the staged file to the target, hashing it in the process
//   - updates archive_inventory_entries.content_sha256 if not already set
//   - upserts the installed_files row
//   - inserts an operation_changes row
//
// All DB writes are performed in a single transaction.
func (e Extractor) DeployFile(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	op planner.PlanOp,
	stagingPath string,
	targetRoot string,
	gameInstallID int64,
	targetID int64,
	profileID int64,
	operationID int64,
) (DeployFileResult, error) {
	winner := op.File.Winner()
	srcPath := filepath.Join(stagingPath, winner.Entry.SourcePath)
	absDestPath := filepath.Join(targetRoot, op.DestPath)

	var result DeployFileResult
	result.DestPath = op.DestPath

	// Back up pre-existing file if needed
	if op.NeedsBackup {
		backupResult, err := blobstore.IngestBackup(
			ctx,
			db,
			q,
			e.BlobStore,
			absDestPath,
			gameInstallID,
			targetID,
			op.DestPath,
			sql.NullInt64{Int64: operationID, Valid: true},
			"",
		)
		if err != nil {
			return DeployFileResult{}, fmt.Errorf("backup %q: %w", op.DestPath, err)
		}
		result.WasBackedUp = true
		result.BackupSha256 = backupResult.SHA256Hex
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(absDestPath), 0o755); err != nil {
		return DeployFileResult{}, fmt.Errorf("mkdir for %q: %w", op.DestPath, err)
	}

	// Copy staged file to target, hashing in the process
	contentSha256, sizeBytes, err := copyAndHash(ctx, srcPath, absDestPath)
	if err != nil {
		return DeployFileResult{}, fmt.Errorf("deploy %q: %w", op.DestPath, err)
	}
	result.ContentSha256 = contentSha256
	result.SizeBytes = sizeBytes

	// DB writes in a single transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return DeployFileResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := q.WithTx(tx)

	// Update archive_inventory_entries.content_sha256 if not already set
	if err := qtx.UpdateInventoryEntryContentSha256(ctx, dbq.UpdateInventoryEntryContentSha256Params{
		ContentSha256: sql.NullString{String: contentSha256, Valid: true},
		ArchiveSha256: winner.Entry.ArchiveSha256,
		Position:      winner.Entry.Position,
	}); err != nil {
		return DeployFileResult{}, fmt.Errorf("update inventory content sha256: %w", err)
	}

	// Upsert installed_files
	if err := qtx.UpsertInstalledFile(ctx, dbq.UpsertInstalledFileParams{
		GameInstallID:         gameInstallID,
		TargetID:              targetID,
		Relpath:               op.DestPath,
		ContentSha256:         contentSha256,
		SizeBytes:             sizeBytes,
		OwnerModFileVersionID: sql.NullInt64{Int64: winner.ModFileVersionID, Valid: true},
		OwnerOverrideID:       sql.NullInt64{},
		OwnerProfileID:        sql.NullInt64{Int64: profileID, Valid: true},
		LastOperationID:       sql.NullInt64{Int64: operationID, Valid: true},
	}); err != nil {
		return DeployFileResult{}, fmt.Errorf("upsert installed file: %w", err)
	}

	// Determine action for operation_changes
	action := "write"
	if op.Kind == planner.PlanOpOverwrite {
		action = "overwrite"
	}

	// Build old_content_sha256 for operation_changes
	// For NeedsBackup cases we have the backup sha which is the pre-existing
	// content. For overwrite of tool-owned files we don't rehash so leave NULL.
	var oldSha sql.NullString
	var oldSize sql.NullInt64
	if op.NeedsBackup && result.BackupSha256 != "" {
		oldSha = sql.NullString{String: result.BackupSha256, Valid: true}
	}

	// Insert operation_changes
	if err := qtx.InsertOperationChange(ctx, dbq.InsertOperationChangeParams{
		OperationID:      operationID,
		GameInstallID:    gameInstallID,
		TargetID:         targetID,
		Relpath:          op.DestPath,
		Action:           action,
		OldContentSha256: oldSha,
		OldSizeBytes:     oldSize,
		NewContentSha256: sql.NullString{String: contentSha256, Valid: true},
		NewSizeBytes:     sql.NullInt64{Int64: sizeBytes, Valid: true},
		ModFileVersionID: sql.NullInt64{Int64: winner.ModFileVersionID, Valid: true},
		BackupBlobSha256: sql.NullString{String: result.BackupSha256, Valid: result.WasBackedUp},
		Notes:            sql.NullString{},
	}); err != nil {
		return DeployFileResult{}, fmt.Errorf("insert operation change: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return DeployFileResult{}, fmt.Errorf("commit: %w", err)
	}

	return result, nil
}

// RemoveFile deletes a tool-managed file from disk and removes its
// installed_files row and inserts an operation_changes row.
// All DB writes are performed in a single transaction.
func (e Extractor) RemoveFile(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	op planner.PlanOp,
	targetRoot string,
	gameInstallID int64,
	targetID int64,
	operationID int64,
) (RemoveFileResult, error) {
	absDestPath := filepath.Join(targetRoot, op.DestPath)

	// Hash the file before removing for operation_changes old_content_sha256.
	// Best-effort: if the file is already gone we still clean up the DB.
	var oldSha sql.NullString
	var oldSize sql.NullInt64
	if info, err := os.Stat(absDestPath); err == nil {
		if sha, err := hashFile(absDestPath); err == nil {
			oldSha = sql.NullString{String: sha, Valid: true}
			oldSize = sql.NullInt64{Int64: info.Size(), Valid: true}
		}
		if err := os.Remove(absDestPath); err != nil && !os.IsNotExist(err) {
			return RemoveFileResult{}, fmt.Errorf("remove %q: %w", op.DestPath, err)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RemoveFileResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := q.WithTx(tx)

	if err := qtx.DeleteInstalledFile(ctx, dbq.DeleteInstalledFileParams{
		GameInstallID: gameInstallID,
		TargetID:      targetID,
		Relpath:       op.DestPath,
	}); err != nil {
		return RemoveFileResult{}, fmt.Errorf("delete installed file: %w", err)
	}

	if err := qtx.InsertOperationChange(ctx, dbq.InsertOperationChangeParams{
		OperationID:      operationID,
		GameInstallID:    gameInstallID,
		TargetID:         targetID,
		Relpath:          op.DestPath,
		Action:           "remove",
		OldContentSha256: oldSha,
		OldSizeBytes:     oldSize,
		NewContentSha256: sql.NullString{},
		NewSizeBytes:     sql.NullInt64{},
		ModFileVersionID: sql.NullInt64{},
		BackupBlobSha256: sql.NullString{},
		Notes:            sql.NullString{},
	}); err != nil {
		return RemoveFileResult{}, fmt.Errorf("insert operation change: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return RemoveFileResult{}, fmt.Errorf("commit: %w", err)
	}

	return RemoveFileResult{DestPath: op.DestPath}, nil
}

// RestoreFile copies a backup blob back to the target path, removes the
// installed_files row, deletes the backups row, and inserts an
// operation_changes row. All DB writes are in a single transaction.
func (e Extractor) RestoreFile(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	op planner.PlanOp,
	targetRoot string,
	gameInstallID int64,
	targetID int64,
	operationID int64,
) (RestoreFileResult, error) {
	absDestPath := filepath.Join(targetRoot, op.DestPath)

	backupPath, err := e.BlobStore.PathFor(blobstore.KindBackup, op.BackupSha256)
	if err != nil {
		return RestoreFileResult{}, fmt.Errorf("resolve backup path: %w", err)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(absDestPath), 0o755); err != nil {
		return RestoreFileResult{}, fmt.Errorf("mkdir for %q: %w", op.DestPath, err)
	}

	// Copy backup blob to target. We don't rehash since the blob store
	// is content-addressed and user editing of blobs is unsupported.
	if err := copyFile(ctx, backupPath, absDestPath); err != nil {
		return RestoreFileResult{}, fmt.Errorf("restore %q: %w", op.DestPath, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RestoreFileResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := q.WithTx(tx)

	// Delete the installed_files row - the file is no longer tool-owned
	if err := qtx.DeleteInstalledFile(ctx, dbq.DeleteInstalledFileParams{
		GameInstallID: gameInstallID,
		TargetID:      targetID,
		Relpath:       op.DestPath,
	}); err != nil {
		return RestoreFileResult{}, fmt.Errorf("delete installed file: %w", err)
	}

	// Delete the backups row - the backup has been consumed.
	if err := qtx.DeleteBackup(ctx, dbq.DeleteBackupParams{
		GameInstallID: gameInstallID,
		TargetID:      targetID,
		Relpath:       op.DestPath,
	}); err != nil {
		return RestoreFileResult{}, fmt.Errorf("delete backup row: %w", err)
	}

	if err := qtx.InsertOperationChange(ctx, dbq.InsertOperationChangeParams{
		OperationID:      operationID,
		GameInstallID:    gameInstallID,
		TargetID:         targetID,
		Relpath:          op.DestPath,
		Action:           "restore_backup",
		OldContentSha256: sql.NullString{},
		OldSizeBytes:     sql.NullInt64{},
		NewContentSha256: sql.NullString{}, // TODO: we should calculate this for completeness
		NewSizeBytes:     sql.NullInt64{},  // TODO: we should calculate this for completeness
		ModFileVersionID: sql.NullInt64{},
		BackupBlobSha256: sql.NullString{String: op.BackupSha256, Valid: true},
		Notes:            sql.NullString{},
	}); err != nil {
		return RestoreFileResult{}, fmt.Errorf("insert operation change: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return RestoreFileResult{}, fmt.Errorf("commit: %w", err)
	}

	return RestoreFileResult{DestPath: op.DestPath}, nil
}

// copyAndHash copies src to dst atomically via a temp file in dst's directory
// and computes the sha256 of the content.
// Returns the hex sha256 and size in bytes.
func copyAndHash(ctx context.Context, src, dst string) (string, int64, error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return "", 0, fmt.Errorf("open src: %w", err)
	}
	defer srcFile.Close()

	dstDir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dstDir, ".deploy-*")
	if err != nil {
		return "", 0, fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // no-op if rename succeeded
	}()

	h := sha256.New()
	w := io.MultiWriter(tmp, h)

	buf := make([]byte, 1024*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		default:
		}
		nr, readErr := srcFile.Read(buf)
		if nr > 0 {
			nw, writeErr := w.Write(buf[:nr])
			written += int64(nw)
			if writeErr != nil {
				return "", 0, fmt.Errorf("write: %w", writeErr)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", 0, fmt.Errorf("read: %w", readErr)
		}
	}

	if err := tmp.Sync(); err != nil {
		return "", 0, fmt.Errorf("fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", 0, fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return "", 0, fmt.Errorf("rename into place: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), written, nil
}

// copyFile copies src to dst atomically via a temp file in dst's directory.
// Used for restore operations where we don't need to hash the content.
func copyFile(ctx context.Context, src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer srcFile.Close()

	dstDir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dstDir, ".restore-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	buf := make([]byte, 1024*1024)
	if _, err := blobstore.CopyWithContext(ctx, tmp, srcFile, buf); err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}

	return nil
}

// hashFile computes the sha256 digest of the file at path and returns it as
// a lowercase hex string.
// TODO this is also in the planner -- extract to internal
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for hashing: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file contents: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
