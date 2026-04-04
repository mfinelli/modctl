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

package cmd

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/restore"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify the integrity of a modctl export bundle",
	Long: `Verify the integrity of a modctl export bundle.

Performs a full integrity check without importing anything:
  - Verifies the database snapshot against the manifest sha256
  - Checks database internal integrity (quick_check, foreign_key_check)
  - Verifies every blob file hashes correctly against its filename
  - Checks that every blob referenced in the database is present in the bundle
  - Checks that every blob file in the bundle has a corresponding database row
  - Warns about version compatibility issues

Exits non-zero if any integrity issues are found. Version warnings
(newer modctl or schema version) do not affect the exit code.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract styles
		boldStyle := lipgloss.NewStyle().Bold(true)
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		errStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))

		ctx := cmd.Context()

		bundlePath := args[0]

		fmt.Println(boldStyle.Render("Verifying bundle: " + bundlePath))
		fmt.Println()

		bundle, err := restore.OpenAndValidate(ctx, bundlePath)
		if err != nil {
			return fmt.Errorf("bundle validation failed: %w", err)
		}
		defer bundle.Close()

		// manifest summary
		fmt.Println(boldStyle.Render("Manifest"))
		fmt.Println(okStyle.Render("  ✓ database integrity OK (sha256 matches manifest)"))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  format version: %d", bundle.Manifest.ExportFormatVersion)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  export kind:    %s", bundle.Manifest.ExportKind)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  exported at:    %s", bundle.Manifest.ExportedAt.Format("2006-01-02 15:04:05 UTC"))))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  modctl version: %s", bundle.Manifest.ModctlVersion)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  schema version: %d", bundle.Manifest.SchemaVersion)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  archives:       %d", bundle.Manifest.Counts.Archives)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  backups:        %d", bundle.Manifest.Counts.Backups)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  overrides:      %d", bundle.Manifest.Counts.Overrides)))
		if bundle.Manifest.Game != nil {
			fmt.Println(subtleStyle.Render(fmt.Sprintf("  game:           %s (%s:%s)",
				bundle.Manifest.Game.DisplayName,
				bundle.Manifest.Game.StoreID,
				bundle.Manifest.Game.StoreGameID,
			)))
		}
		fmt.Println()

		// version warnings (informational only, do not affect exit code)
		bundleVer := "v" + bundle.Manifest.ModctlVersion
		currentVer := "v" + rootCmd.Version
		if semver.IsValid(bundleVer) && semver.IsValid(currentVer) &&
			semver.Compare(bundleVer, currentVer) > 0 {
			fmt.Println(warnStyle.Render(fmt.Sprintf(
				"  ⚠ bundle was created with modctl %s (current: %s)",
				bundle.Manifest.ModctlVersion, rootCmd.Version,
			)))
		}

		// get current schema version for comparison
		// TODO: i'm not actually doing this yet... I'm not sure that
		//       i want to require a databse to verify a bundle
		// var currentSchema int64
		// if db, err := internal.SetupDB(); err == nil {
		// 	currentSchema, _ = restore.CurrentSchemaVersion(ctx, db)
		// 	db.Close()
		// }
		// if currentSchema > 0 && bundle.Manifest.SchemaVersion > currentSchema {
		// 	fmt.Println(warnStyle.Render(fmt.Sprintf(
		// 		"  ⚠ bundle schema version %d is newer than current %d - upgrade modctl before importing",
		// 		bundle.Manifest.SchemaVersion, currentSchema,
		// 	)))
		// }

		// collect all integrity issues
		var issues []string

		// database integrity checks
		fmt.Println(boldStyle.Render("Database Integrity"))
		bq := dbq.New(bundle.BundleDB)

		dbIssues := checkBundleDB(ctx, bundle.BundleDB)
		if len(dbIssues) == 0 {
			fmt.Println(okStyle.Render("  ✓ quick_check OK"))
			fmt.Println(okStyle.Render("  ✓ foreign_key_check OK"))
		} else {
			for _, iss := range dbIssues {
				fmt.Println(errStyle.Render("  ✗ " + iss))
				issues = append(issues, iss)
			}
		}
		fmt.Println()

		// nexus cache integrity check
		if bundle.Manifest.NexusCacheSha256 != "" {
			fmt.Println(boldStyle.Render("Nexus Cache Integrity"))
			cachePath := filepath.Join(bundle.BundleDir, "nexus_cache.db")
			cacheIssues := checkBundleCacheDB(ctx, cachePath)
			if len(cacheIssues) == 0 {
				fmt.Println(okStyle.Render("  ✓ quick_check OK"))
			} else {
				for _, iss := range cacheIssues {
					fmt.Println(errStyle.Render("  ✗ " + iss))
					issues = append(issues, iss)
				}
			}
			fmt.Println()
		} else {
			fmt.Println(boldStyle.Render("Nexus Cache Integrity"))
			fmt.Println(subtleStyle.Render("  (not present in bundle)"))
			fmt.Println()
		}

		// blob checks
		fmt.Println(boldStyle.Render("Blob Integrity"))
		blobIssues := checkBundleBlobs(ctx, bundle, bq)
		if len(blobIssues) == 0 {
			fmt.Println(okStyle.Render("  ✓ all blobs present, referenced, and hash correctly"))
		} else {
			for _, iss := range blobIssues {
				fmt.Println(errStyle.Render("  ✗ " + iss))
				issues = append(issues, iss)
			}
		}
		fmt.Println()

		// final summary
		fmt.Println(boldStyle.Render("Summary"))
		if len(issues) == 0 {
			fmt.Println(okStyle.Render("  ✓ bundle is valid and ready to import"))
			return nil
		}

		fmt.Println(errStyle.Render(fmt.Sprintf("  ✗ %d issue(s) found:", len(issues))))
		for _, iss := range issues {
			fmt.Println(errStyle.Render("    • " + iss))
		}

		// return a plain error with no message so cobra doesn't double-print
		// the issue list; the exit code will be non-zero
		return errors.New("bundle verification failed")
	},
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}

// checkBundleDB runs quick_check and foreign_key_check on the bundle database.
func checkBundleDB(ctx context.Context, db *sql.DB) []string {
	var issues []string

	rows, err := db.QueryContext(ctx, "PRAGMA quick_check;")
	if err != nil {
		return []string{"quick_check failed: " + err.Error()}
	}
	defer rows.Close()
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			issues = append(issues, "quick_check scan error: "+err.Error())
			continue
		}
		if result != "ok" {
			issues = append(issues, "quick_check: "+result)
		}
	}
	if err := rows.Err(); err != nil {
		issues = append(issues, "quick_check rows error: "+err.Error())
	}

	fkRows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check;")
	if err != nil {
		return append(issues, "foreign_key_check failed: "+err.Error())
	}
	defer fkRows.Close()
	for fkRows.Next() {
		var table string
		var rowid int64
		var parent string
		var fkid int64
		if err := fkRows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			issues = append(issues, "foreign_key_check scan error: "+err.Error())
			continue
		}
		issues = append(issues, fmt.Sprintf(
			"foreign key violation: table=%s rowid=%d parent=%s fkid=%d",
			table, rowid, parent, fkid,
		))
	}
	if err := fkRows.Err(); err != nil {
		issues = append(issues, "foreign_key_check rows error: "+err.Error())
	}

	return issues
}

// checkBundleBlobs verifies blob files in the bundle:
// - every blob referenced in the DB is present as a file
// - every blob file has a corresponding DB row
// - every blob file hashes correctly against its filename
func checkBundleBlobs(ctx context.Context, bundle *restore.Bundle, bq *dbq.Queries) []string {
	var issues []string

	// build set of all blobs recorded in bundle DB
	type dbBlob struct {
		sha256 string
		kind   string
	}
	dbBlobs := make(map[string]dbBlob)

	for _, kindStr := range []string{"archive", "backup", "override"} {
		rows, err := bq.ListBlobsByKind(ctx, kindStr)
		if err != nil {
			return []string{fmt.Sprintf("list blobs kind=%s: %s", kindStr, err)}
		}
		for _, b := range rows {
			dbBlobs[b.Sha256] = dbBlob{b.Sha256, kindStr}
		}
	}

	// build set of all blob files present in bundle
	kindDirs := map[string]blobstore.Kind{
		"archives":  blobstore.KindArchive,
		"backups":   blobstore.KindBackup,
		"overrides": blobstore.KindOverride,
	}

	fileBlobs := make(map[string]struct{})
	buf := make([]byte, 1024*1024)
	total := len(dbBlobs)
	checked := 0

	if total > 0 {
		fmt.Printf("  hashing blobs (0/%d)", total)
	}

	for dirName := range kindDirs {
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

			name := d.Name()
			if len(name) != 64 {
				return nil
			}

			fileBlobs[name] = struct{}{}
			checked++
			if total > 0 {
				fmt.Printf("\r  hashing blobs (%d/%d)", checked, total)
			}

			// hash and verify
			f, err := os.Open(path)
			if err != nil {
				issues = append(issues, fmt.Sprintf("open blob %s...: %s", name[:16], err))
				return nil
			}
			h := sha256.New()
			_, cerr := blobstore.CopyWithContext(ctx, h, f, buf)
			f.Close()
			if cerr != nil {
				issues = append(issues, fmt.Sprintf("hash blob %s...: %s", name[:16], cerr))
				return nil
			}
			actual := hex.EncodeToString(h.Sum(nil))
			if actual != name {
				issues = append(issues, fmt.Sprintf(
					"hash mismatch: filename=%s actual=%s", name, actual,
				))
			}

			return nil
		})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return []string{"cancelled"}
			}
			issues = append(issues, fmt.Sprintf("walk %s: %s", dirName, err))
		}
	}

	if total > 0 {
		fmt.Printf("\r%-60s\r", "")
		fmt.Printf("  hashed %d blob(s)\n", checked)
	}

	// check DB blobs are present as files
	for sha := range dbBlobs {
		if _, ok := fileBlobs[sha]; !ok {
			issues = append(issues, fmt.Sprintf(
				"blob %s... is in database but missing from bundle", sha[:16],
			))
		}
	}

	// check file blobs have DB rows
	for sha := range fileBlobs {
		if _, ok := dbBlobs[sha]; !ok {
			issues = append(issues, fmt.Sprintf(
				"blob %s... is in bundle but has no database row (orphan)", sha[:16],
			))
		}
	}

	return issues
}

// checkBundleCacheDB runs quick_check on the nexus cache database.
func checkBundleCacheDB(ctx context.Context, cachePath string) []string {
	cacheDB, err := sql.Open("sqlite3", cachePath+internal.DB_PRAGMAS+"&mode=ro")
	if err != nil {
		return []string{"open nexus cache db: " + err.Error()}
	}
	defer cacheDB.Close()

	rows, err := cacheDB.QueryContext(ctx, "PRAGMA quick_check;")
	if err != nil {
		return []string{"quick_check failed: " + err.Error()}
	}
	defer rows.Close()

	var issues []string
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			issues = append(issues, "quick_check scan error: "+err.Error())
			continue
		}
		if result != "ok" {
			issues = append(issues, "quick_check: "+result)
		}
	}
	if err := rows.Err(); err != nil {
		issues = append(issues, "quick_check rows error: "+err.Error())
	}
	return issues
}
