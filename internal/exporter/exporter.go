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
}

const (
	ExportKindFull ExportKind = "full"
	ExportKindGame ExportKind = "game"
)

type ManifestGame struct {
	StoreID     string `json:"store_id"`
	StoreGameID string `json:"store_game_id"`
	DisplayName string `json:"display_name"`
}

type ManifestCounts struct {
	Archives int `json:"archives"`
	Backups  int `json:"backups"`
}

type Manifest struct {
	ExportFormatVersion int            `json:"export_format_version"`
	ExportKind          ExportKind     `json:"export_kind"`
	ExportedAt          time.Time      `json:"exported_at"`
	ModctlVersion       string         `json:"modctl_version"`
	SchemaVersion       int64          `json:"schema_version"`
	DBSha256            string         `json:"db_sha256"`
	Counts              ManifestCounts `json:"counts"`
	Game                *ManifestGame  `json:"game,omitempty"`
}

func currentSchemaVersion(ctx context.Context, db *sql.DB) (int64, error) {
	var version int64
	// TODO use the goose provider
	err := db.QueryRowContext(ctx,
		`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1`,
	).Scan(&version)
	return version, err
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

func writeBlobToTar(ctx context.Context, tw *tar.Writer, bs blobstore.Store, kind blobstore.Kind, sha256hex string) error {
	path, err := bs.PathFor(kind, sha256hex)
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// blob missing from disk - skip with no error; doctor surfaces this
			return nil
		}
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
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
		return err
	}

	buf := make([]byte, 1024*1024)
	_, err = blobstore.CopyWithContext(ctx, tw, f, buf)
	return err
}

// slugify converts a game display name to a safe filename slug.
// e.g. "Cyberpunk 2077" -> "cyberpunk2077"
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
