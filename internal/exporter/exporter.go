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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/blobstore"
)

const ExportFormatVersion = 1

type ExportKind string

type Options struct {
	// ModctlVersion is injected from rootCmd.Version
	ModctlVersion string
	// SkipInventory omits archive_inventory_entries from game-scoped exports
	SkipInventory bool
	// OutputPath is the destination file
	OutputPath string
	// NoVerify skips rehashing blobs before export
	NoVerify bool
	// CacheDBPath is the full path to the nexus_cache.db
	CacheDBPath string
}

const (
	ExportKindFull ExportKind = "full"
	ExportKindGame ExportKind = "game"
)

const DatabaseFilename = "modctl.db"

type ManifestGame struct {
	StoreID     string `json:"store_id"`
	StoreGameID string `json:"store_game_id"`
	DisplayName string `json:"display_name"`
}

type ManifestCounts struct {
	Archives  int `json:"archives"`
	Backups   int `json:"backups"`
	Overrides int `json:"overrides"`
}

type Manifest struct {
	ExportFormatVersion int            `json:"export_format_version"`
	ExportKind          ExportKind     `json:"export_kind"`
	ExportedAt          time.Time      `json:"exported_at"`
	ModctlVersion       string         `json:"modctl_version"`
	SchemaVersion       int64          `json:"schema_version"`
	DBSha256            string         `json:"db_sha256"`
	NexusCacheSha256    string         `json:"nexus_cache_sha256"`
	Counts              ManifestCounts `json:"counts"`
	Game                *ManifestGame  `json:"game,omitempty"`
}

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

// TODO: we already have two other copies of this export it somewhere...
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

func writeManifest(tw *tar.Writer, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	hdr := &tar.Header{
		Name:    "manifest.json",
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: m.ExportedAt,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}

func writeFileToTar(tw *tar.Writer, srcPath, tarName string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}

	hdr := &tar.Header{
		Name:    tarName,
		Mode:    0o644,
		Size:    st.Size(),
		ModTime: st.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

func writeBlobToTar(ctx context.Context, tw *tar.Writer, bs blobstore.Store, kind blobstore.Kind, sha256hex string) (bool, error) {
	path, err := bs.PathFor(kind, sha256hex)
	if err != nil {
		return false, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// blob missing from disk - skip with no error; doctor surfaces this
			return true, nil
		}
		return false, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return false, err
	}

	fan := sha256hex[:2]
	tarPath := fmt.Sprintf("%s/%s/%s", string(kind)+"s", fan, sha256hex)

	hdr := &tar.Header{
		Name:    tarPath,
		Mode:    0o644,
		Size:    st.Size(),
		ModTime: st.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return false, err
	}

	buf := make([]byte, 1024*1024)
	_, err = blobstore.CopyWithContext(ctx, tw, f, buf)
	return false, err
}

// slugify converts a game display name to a safe filename slug.
//
// e.g. "Cyberpunk 2077" -> "cyberpunk2077"
//
//	"The Elder Scrolls V: Skyrim" -> "the-elder-scrolls-v-skyrim"
func Slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else {
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	result := strings.TrimRight(b.String(), "-")
	if len(result) > 100 {
		result = result[:100]
		result = strings.TrimRight(result, "-")
	}
	return result
}

type blobToVerify struct {
	sha256 string
	kind   blobstore.Kind
}

// verifyBlobs hashes all blobs of the given kinds against their on-disk files,
// updates verified_at on success, and returns an error if any hash mismatches.
// Progress is printed as a single updating line.
func verifyBlobs(
	ctx context.Context,
	q *dbq.Queries,
	bs blobstore.Store,
	blobs []blobToVerify,
) error {
	total := len(blobs)
	if total == 0 {
		return nil
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	buf := make([]byte, 1024*1024)

	fmt.Printf("  verifying blobs (0/%d)", total)

	for i, b := range blobs {
		select {
		case <-ctx.Done():
			fmt.Print("\n")
			return ctx.Err()
		default:
		}

		fmt.Printf("\r  verifying blobs (%d/%d)", i+1, total)

		path, err := bs.PathFor(b.kind, b.sha256)
		if err != nil {
			fmt.Print("\n")
			return fmt.Errorf("derive path for %s: %w", b.sha256, err)
		}

		f, err := os.Open(path)
		if err != nil {
			fmt.Print("\n")
			if os.IsNotExist(err) {
				return fmt.Errorf(
					"blob %s... is missing from disk; run 'doctor' to check blob integrity",
					b.sha256[:16],
				)
			}
			return fmt.Errorf("open blob %s: %w", b.sha256[:16], err)
		}

		h := sha256.New()
		_, cerr := blobstore.CopyWithContext(ctx, h, f, buf)
		f.Close()
		if cerr != nil {
			fmt.Print("\n")
			return fmt.Errorf("hash blob %s: %w", b.sha256[:16], cerr)
		}

		actual := hex.EncodeToString(h.Sum(nil))
		if actual != b.sha256 {
			fmt.Print("\n")
			return fmt.Errorf(
				"blob integrity check failed: expected %s got %s - run 'doctor' to investigate",
				b.sha256, actual,
			)
		}

		if err := q.TouchBlobVerifiedAt(ctx, dbq.TouchBlobVerifiedAtParams{
			VerifiedAt: sql.NullString{String: now, Valid: true},
			Sha256:     b.sha256,
		}); err != nil {
			fmt.Print("\n")
			return fmt.Errorf("update verified_at %s: %w", b.sha256[:16], err)
		}
	}

	fmt.Printf("\r%-60s\r", "")
	fmt.Printf("  verified %d blob(s)\n", total)
	return nil
}
