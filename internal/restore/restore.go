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

package restore

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/mattn/go-sqlite3"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/archivescanner"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/exporter"
)

const supportedFormatVersion = 1

type Options struct {
	SkipInventory bool
	Force         bool
	DryRun        bool
	SameMachine   bool
	Game          string // "store_id:store_game_id", only set when extracting a game from a full bundle
}

type Result struct {
	// counts of what was imported
	Archives         int
	Backups          int
	Overrides        int
	ModPages         int
	Profiles         int
	InventoryScanned int
	InventoryFailed  int
}

// Bundle is the extracted, validated contents of an import bundle.
// It holds the temp directory and opened bundle DB until Close is called.
type Bundle struct {
	Manifest  exporter.Manifest
	BundleDir string // temp dir where bundle was extracted
	BundleDB  *sql.DB
}

func (b *Bundle) Close() {
	if b.BundleDB != nil {
		b.BundleDB.Close()
	}
	if b.BundleDir != "" {
		os.RemoveAll(b.BundleDir)
	}
}

// OpenAndValidate extracts the bundle, verifies integrity, and returns a
// validated Bundle ready for import. Caller must call Close() when done.
func OpenAndValidate(ctx context.Context, bundlePath string) (*Bundle, error) {
	// Extract bundle to temp dir
	tmpDir, err := os.MkdirTemp("", "modctl-import-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	if err := extractBundle(ctx, bundlePath, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("extract bundle: %w", err)
	}

	// Read and parse manifest
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest exporter.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	// Validate format version
	if manifest.ExportFormatVersion > supportedFormatVersion {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf(
			"bundle format version %d is newer than supported version %d; upgrade modctl to import this bundle",
			manifest.ExportFormatVersion, supportedFormatVersion,
		)
	}

	// Verify database integrity
	dbPath := filepath.Join(tmpDir, exporter.DatabaseFilename)
	dbSha, err := hashFile(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("hash bundle database: %w", err)
	}
	if dbSha != manifest.DBSha256 {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("bundle database integrity check failed: expected %s got %s",
			manifest.DBSha256, dbSha)
	}

	// Verify nexus cache integrity if present in bundle
	cachePath := filepath.Join(tmpDir, "nexus_cache.db")
	if manifest.NexusCacheSha256 != "" {
		cacheSha, err := hashFile(cachePath)
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("hash bundle nexus cache: %w", err)
		}
		if cacheSha != manifest.NexusCacheSha256 {
			os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("bundle nexus cache integrity check failed: expected %s got %s",
				manifest.NexusCacheSha256, cacheSha)
		}
	}

	// Open bundle DB
	bundleDB, err := sql.Open("sqlite3", dbPath+internal.DB_PRAGMAS+"&mode=ro")
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("open bundle database: %w", err)
	}

	return &Bundle{
		Manifest:  manifest,
		BundleDir: tmpDir,
		BundleDB:  bundleDB,
	}, nil
}

// TODO: we also have this in the exporter
func currentSchemaVersion(ctx context.Context, db *sql.DB) (int64, error) {
	p, err := internal.GooseProvider(db)
	if err != nil {
		return 0, fmt.Errorf("get goose provider: %w", err)
	}
	current, _, err := p.GetVersions(ctx)
	if err != nil {
		return 0, fmt.Errorf("get schema version: %w", err)
	}
	return current, nil
}

// TODO: we must have 4 copies of this now... just extract it already
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}

	success := false
	defer func() {
		out.Close()
		if !success {
			os.Remove(dst)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}

	success = true
	return nil
}

func scanMissingInventories(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	bs blobstore.Store,
	oldGameInstallID int64,
	bq *dbq.Queries,
	skipInventory bool,
	logger *slog.Logger,
) (scanned int, skipped int, err error) {
	if skipInventory {
		return 0, 0, nil
	}

	versions, err := bq.ExportGetModFileVersionsForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return 0, 0, fmt.Errorf("get mod file versions: %w", err)
	}

	for _, v := range versions {
		if v.InventoryScannedAt.Valid {
			continue // bundle already has inventory
		}

		select {
		case <-ctx.Done():
			return scanned, skipped, ctx.Err()
		default:
		}

		if err := archivescanner.ScanOne(
			ctx,
			db,
			q,
			bs,
			archivescanner.Scanner{},
			v.ArchiveSha256,
			logger,
		); err != nil {
			// non-fatal: warn and continue, user can run mods scan-inventory
			skipped++
			continue
		}
		scanned++
	}
	return scanned, skipped, nil
}

// extractBundle extracts the zstd-compressed tar bundle into destDir.
func extractBundle(ctx context.Context, bundlePath, destDir string) error {
	f, err := os.Open(bundlePath)
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer f.Close()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("create zstd reader: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	buf := make([]byte, 1024*1024)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// Safety: reject path traversal
		destPath := filepath.Join(destDir, filepath.Clean("/"+hdr.Name))
		if !strings.HasPrefix(destPath, destDir) {
			return fmt.Errorf("path traversal detected in bundle: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", destPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", destPath, err)
			}
			out, err := os.Create(destPath)
			if err != nil {
				return fmt.Errorf("create %s: %w", destPath, err)
			}
			if _, err := blobstore.CopyWithContext(ctx, out, tr, buf); err != nil {
				out.Close()
				return fmt.Errorf("extract %s: %w", destPath, err)
			}
			out.Close()
		}
	}
	return nil
}

// importBlobs extracts and verifies all blobs from the bundle,
// ingesting them into the blob store.
func importBlobs(ctx context.Context, bundle *Bundle, bs blobstore.Store) (archiveCount, backupCount, overrideCount int, err error) {
	kindDirs := map[string]blobstore.Kind{
		"archives":  blobstore.KindArchive,
		"backups":   blobstore.KindBackup,
		"overrides": blobstore.KindOverride,
	}

	for dirName, kind := range kindDirs {
		blobRoot := filepath.Join(bundle.BundleDir, dirName)
		if _, err := os.Stat(blobRoot); os.IsNotExist(err) {
			continue
		}

		err := filepath.WalkDir(blobRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			expectedSha := d.Name()
			if len(expectedSha) != 64 {
				return nil // not a blob file
			}

			// Verify blob integrity before ingesting
			actualSha, err := hashFile(path)
			if err != nil {
				return fmt.Errorf("hash blob %s: %w", expectedSha, err)
			}
			if actualSha != expectedSha {
				return fmt.Errorf(
					"blob integrity check failed: filename=%s actual=%s",
					expectedSha, actualSha,
				)
			}

			// IngestFile handles dedup and atomic rename
			_, err = bs.IngestFile(ctx, kind, path)
			if err != nil {
				return fmt.Errorf("ingest blob %s: %w", expectedSha, err)
			}

			switch kind {
			case blobstore.KindArchive:
				archiveCount++
			case blobstore.KindBackup:
				backupCount++
			case blobstore.KindOverride:
				overrideCount++
			}
			return nil
		})
		if err != nil {
			return 0, 0, 0, err
		}
	}
	return archiveCount, backupCount, overrideCount, nil
}

func isSQLiteUniqueConstraint(err error) bool {
	var se sqlite3.Error
	return errors.As(err, &se) &&
		se.Code == sqlite3.ErrConstraint &&
		se.ExtendedCode == sqlite3.ErrConstraintUnique
}
