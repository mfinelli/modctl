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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var deepCheck bool
var doctorRehash bool

var SampleTarGz []byte

// doctorCmd represents the doctor command
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run health checks on the modctl state, database, and dependencies",
	Long: `Run a read-only health check to confirm modctl can operate safely.

Doctor verifies:
  - State directory layout and writability (archives/, backups/, overrides/,
    tmp/)
  - Database is present and usable (SELECT 1), and reports pending migrations
  - SQLite integrity checks (quick_check by default; integrity_check +
    foreign_key_check with --deep)
  - External dependencies (bsdtar present, --version works, and can list a
    built-in test archive)
  - (TODO) Steam readiness when the Steam store is enabled (locates Steam root
    and parses libraryfolders.vdf)
  - Integrity of blobs stored on disk (presence, size, hash)

Doctor does not modify Steam or your game installs. It may read files to
validate integrity.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		run := func() error {
			if err := checkDb(ctx); err != nil {
				return err
			}
			if err := checkPaths(); err != nil {
				return err
			}
			if err := checkBsdtar(ctx); err != nil {
				return err
			}
			if err := checkSteamStatus(); err != nil {
				return err
			}
			if err := checkBlobs(ctx); err != nil {
				return err
			}
			return nil
		}

		if err := run(); err != nil {
			if errors.Is(err, context.Canceled) {
				return fmt.Errorf("cancelled")
			}
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)

	doctorCmd.Flags().BoolVar(&deepCheck, "full", false, "Runs a more complete database check")
	doctorCmd.Flags().BoolVar(&doctorRehash, "recheck", false, "Rehashes all blobs in the blob store to ensure integrity")
}

// checkDb verifies the DB exists and is usable, and warns if migrations
// are pending. Returns error only for non-recoverable failures.
func checkDb(ctx context.Context) error {
	// TODO: extract these somewhere else
	headerStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("63"))
	subtleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))
	errStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("1"))
	okStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))
	warnStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("3"))

	fmt.Println(headerStyle.Render("Database Checks"))
	fmt.Println(subtleStyle.Render("  db: " + viper.GetString("database")))
	fmt.Println()

	// 1) DB file existence
	dbPath := viper.GetString("database")
	info, err := os.Stat(dbPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println(errStyle.Render("  ✗ database does not exist"))
			fmt.Println(subtleStyle.Render("    run `modctl init` to create the state directory and database"))
			fmt.Println()
			return fmt.Errorf("database missing: %s", dbPath)
		}
		fmt.Println(errStyle.Render("  ✗ could not stat database file"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		fmt.Println()
		return fmt.Errorf("cannot stat database: %w", err)
	}
	if info.IsDir() {
		fmt.Println(errStyle.Render("  ✗ database path is a directory, expected a file"))
		fmt.Println()
		return fmt.Errorf("database path is a directory: %s", dbPath)
	}
	fmt.Println(okStyle.Render("  ✓ database file exists"))

	// Keep doctor snappy.
	ctxT, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	// 2) Open DB + trivial query
	db, err := internal.SetupDB()
	if err != nil {
		fmt.Println(errStyle.Render("  ✗ could not open database"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		fmt.Println()
		return fmt.Errorf("cannot open database: %w", err)
	}
	defer db.Close()

	var one int
	if err := db.QueryRowContext(ctxT, "SELECT 1").Scan(&one); err != nil || one != 1 {
		fmt.Println(errStyle.Render("  ✗ basic query failed (SELECT 1)"))
		if err != nil {
			fmt.Println(subtleStyle.Render("    " + err.Error()))
		}
		fmt.Println()
		return fmt.Errorf("database not usable: %w", err)
	}
	fmt.Println(okStyle.Render("  ✓ basic query OK (SELECT 1)"))

	// 3) migrations status
	p, err := internal.GooseProvider(db)
	if err != nil {
		// if we can't determine migration state treat it as fatal
		fmt.Println(errStyle.Render("  ✗ could not determine migration status"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		fmt.Println()
		return fmt.Errorf("cannot determine migration status: %w", err)
	}

	pending, err := p.HasPending(ctx)
	if err != nil {
		// if we can't determine migration state treat it as fatal
		fmt.Println(errStyle.Render("  ✗ could not determine migration status"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		fmt.Println()
		return fmt.Errorf("cannot determine migration status: %w", err)
	}

	if pending {
		current, target, verr := p.GetVersions(ctx)
		if verr == nil {
			fmt.Println(warnStyle.Render(fmt.Sprintf(
				"  ⚠ pending migrations (db=%d, target=%d)",
				current, target,
			)))
		} else {
			fmt.Println(warnStyle.Render("  ⚠ pending migrations — other commands will auto-migrate"))
		}
	} else {
		fmt.Println(okStyle.Render("  ✓ migrations up to date"))
	}

	// 4) quick_check or integrity_check and foreign_key_check
	pragma := "PRAGMA quick_check;"
	label := "quick_check"
	if deepCheck {
		pragma = "PRAGMA integrity_check;"
		label = "integrity_check"
	}

	rows, err := db.QueryContext(ctx, pragma)
	if err != nil {
		fmt.Println(errStyle.Render(fmt.Sprintf("  ✗ %s failed", label)))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		return fmt.Errorf("%s failed: %w", label, err)
	}
	defer rows.Close()

	var problems []string
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return err
		}
		if result != "ok" {
			problems = append(problems, result)
		}
	}

	if len(problems) == 0 {
		fmt.Println(okStyle.Render(fmt.Sprintf("  ✓ %s OK", label)))
	} else {
		fmt.Println(errStyle.Render(fmt.Sprintf("  ✗ %s reported corruption", label)))
		for _, p := range problems {
			fmt.Println(subtleStyle.Render("    " + p))
		}
		return fmt.Errorf("database integrity check failed")
	}

	if deepCheck {
		rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check;")
		if err != nil {
			fmt.Println(errStyle.Render("  ✗ foreign_key_check failed"))
			fmt.Println(subtleStyle.Render("    " + err.Error()))
			return fmt.Errorf("foreign_key_check failed: %w", err)
		}
		defer rows.Close()

		var violations []string

		for rows.Next() {
			var table string
			var rowid int64
			var parent string
			var fkid int64

			if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
				return err
			}

			violations = append(violations,
				fmt.Sprintf("table=%s rowid=%d parent=%s fkid=%d",
					table, rowid, parent, fkid,
				),
			)
		}

		if len(violations) == 0 {
			fmt.Println(okStyle.Render("  ✓ foreign_key_check OK"))
		} else {
			fmt.Println(errStyle.Render("  ✗ foreign_key_check reported violations"))
			for _, v := range violations {
				fmt.Println(subtleStyle.Render("    " + v))
			}
			return fmt.Errorf("foreign key violations detected")
		}
	}

	fmt.Println()

	return nil
}

func checkPaths() error {
	// TODO: extract these somewhere else
	headerStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("63"))
	subtleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))
	errStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("1"))
	okStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))

	fmt.Println(headerStyle.Render("State Directory Checks"))
	fmt.Println(subtleStyle.Render("  root: " + filepath.Join(xdg.DataHome, "modctl")))
	fmt.Println()

	required := []string{
		viper.GetString("archives_dir"),
		viper.GetString("backups_dir"),
		viper.GetString("overrides_dir"),
		viper.GetString("tmp_dir"),
	}

	var fatalErr error

	for _, path := range required {
		name := filepath.Base(path)
		info, err := os.Stat(path)
		if err != nil {
			fmt.Println(errStyle.Render(fmt.Sprintf("  ✗ %s: does not exist (%s)", name, path)))
			fatalErr = errors.New("missing required state directory")
			continue
		}

		if !info.IsDir() {
			fmt.Println(errStyle.Render(fmt.Sprintf("  ✗ %s: not a directory (%s)", name, path)))
			fatalErr = errors.New("invalid state directory type")
			continue
		}

		// Test writability by creating a temp file
		testFile := filepath.Join(path, ".modctl-doctor-write-test")
		if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
			fmt.Println(errStyle.Render(fmt.Sprintf("  ✗ %s: not writable (%s)", name, path)))
			fatalErr = errors.New("state directory not writable")
			continue
		}
		_ = os.Remove(testFile)

		fmt.Println(okStyle.Render(fmt.Sprintf("  ✓ %s: OK (%s)", name, path)))
	}

	fmt.Println()

	return fatalErr
}

func checkBsdtar(ctx context.Context) error {
	// TODO: extract these somewhere else
	headerStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("63"))
	subtleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))
	errStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("1"))
	okStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))

	bsdtar := viper.GetString("bsdtar")
	fmt.Println(headerStyle.Render("bsdtar Checks"))
	fmt.Println(subtleStyle.Render("  search: " + bsdtar))
	fmt.Println()

	resolvedPath, err := exec.LookPath(bsdtar)
	if err != nil {
		fmt.Println(errStyle.Render("  ✗ bsdtar not found in PATH"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		return fmt.Errorf("bsdtar not found: %w", err)
	}

	fmt.Println(okStyle.Render("  ✓ bsdtar found: " + resolvedPath))

	// Use short timeout for all subprocess calls
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	versionCmd := exec.CommandContext(cmdCtx, resolvedPath, "--version")
	versionOutput, err := versionCmd.CombinedOutput()
	if err != nil {
		fmt.Println(errStyle.Render("  ✗ bsdtar --version failed"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		return fmt.Errorf("bsdtar --version failed: %w", err)
	}

	fmt.Println(okStyle.Render("  ✓ bsdtar --version OK"))
	fmt.Println(subtleStyle.Render("      " + strings.TrimSpace(string(versionOutput))))

	tmpFile, err := os.CreateTemp("", "modctl-bsdtar-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	defer tmpFile.Close()

	if _, err := tmpFile.Write(SampleTarGz); err != nil {
		return fmt.Errorf("failed to write sample archive: %w", err)
	}

	listCmd := exec.CommandContext(cmdCtx, resolvedPath, "-t", "-f", tmpPath)
	listOutput, err := listCmd.CombinedOutput()
	if err != nil {
		fmt.Println(errStyle.Render("  ✗ bsdtar failed to list sample archive"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		return fmt.Errorf("bsdtar test archive failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(listOutput)), "\n")

	if len(lines) != 1 {
		fmt.Println(errStyle.Render("  ✗ unexpected archive contents"))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("    expected 1 entry, got %d", len(lines))))
		for _, e := range lines {
			fmt.Println(subtleStyle.Render("    " + e))
		}
		return fmt.Errorf("invalid sample archive contents")
	}

	if lines[0] != "hello.txt" {
		fmt.Println(errStyle.Render("  ✗ archive entry mismatch"))
		fmt.Println(subtleStyle.Render("    expected: hello.txt"))
		fmt.Println(subtleStyle.Render("    got:      " + lines[0]))
		return fmt.Errorf("archive contents incorrect")
	}

	fmt.Println(okStyle.Render("  ✓ bsdtar archive test OK"))

	fmt.Println()

	return nil
}

func checkSteamStatus() error {
	// TODO loop through game installs and ensure that we can write into them
	return nil
}

// checkBlobsPresence scans blob records and ensures each expected blob file
// exists on disk at the derived content-addressed path.
//
// For now this is "presence + size sanity". If rehashCheck is enabled we’ll
// add a second pass later to stream-hash and update verified_at.
func checkBlobs(ctx context.Context) error {
	// TODO: extract these somewhere else
	headerStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("63"))
	subtleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))
	errStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("1"))
	okStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))
	warnStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("3"))

	fmt.Println(headerStyle.Render("Blob Store Checks"))
	fmt.Println(subtleStyle.Render("  archives:  " + viper.GetString("archives_dir")))
	fmt.Println(subtleStyle.Render("  backups:   " + viper.GetString("backups_dir")))
	fmt.Println(subtleStyle.Render("  overrides: " + viper.GetString("overrides_dir")))
	fmt.Println()

	db, err := internal.SetupDB()
	if err != nil {
		fmt.Println(errStyle.Render("  ✗ could not open database"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		fmt.Println()
		return fmt.Errorf("cannot open database: %w", err)
	}
	defer db.Close()

	q := dbq.New(db)

	bs := blobstore.Store{
		ArchivesDir:  viper.GetString("archives_dir"),
		BackupsDir:   viper.GetString("backups_dir"),
		OverridesDir: viper.GetString("overrides_dir"),
	}

	kinds := []blobstore.Kind{
		blobstore.KindArchive,
		blobstore.KindBackup,
		blobstore.KindOverride,
	}

	for _, kind := range kinds {
		rows, err := q.ListBlobsByKind(ctx, string(kind))
		if err != nil {
			fmt.Println(errStyle.Render(fmt.Sprintf("  ✗ %s: failed to list blobs", kind)))
			fmt.Println(subtleStyle.Render("    " + err.Error()))
			fmt.Println()
			return fmt.Errorf("list blobs kind=%s: %w", kind, err)
		}

		var missing int
		for _, b := range rows {
			path, perr := bs.PathFor(kind, b.Sha256)
			if perr != nil {
				return fmt.Errorf("derive blob path kind=%s sha=%s: %w", kind, b.Sha256, perr)
			}

			st, serr := os.Stat(path)
			if serr != nil {
				if errors.Is(serr, os.ErrNotExist) {
					// TODO: there should be a way to surface to the user
					//       _which_ blobs are missing (eg original filename or
					//       which games a blob is associated with)
					missing++
					continue
				}
				return fmt.Errorf("stat blob kind=%s sha=%s path=%s: %w", kind, b.Sha256, path, serr)
			}

			// size sanity: if it exists but size differs, something is wrong
			if st.Size() != b.SizeBytes {
				return fmt.Errorf(
					"blob size mismatch kind=%s sha=%s path=%s db=%d disk=%d",
					kind, b.Sha256, path, b.SizeBytes, st.Size(),
				)
			}
		}

		switch {
		case len(rows) == 0:
			fmt.Println(okStyle.Render(fmt.Sprintf("  ✓ %s: no blobs recorded", kind)))
		case missing == 0:
			fmt.Println(okStyle.Render(fmt.Sprintf("  ✓ %s: %d/%d present", kind, len(rows), len(rows))))
		default:
			fmt.Println(warnStyle.Render(fmt.Sprintf("  ⚠ %s: %d/%d present (%d missing)", kind, len(rows)-missing, len(rows), missing)))
		}
	}

	if doctorRehash {
		fmt.Println()
		for _, kind := range kinds {
			if err := rehashBlobs(ctx, q, bs, kind, subtleStyle); err != nil {
				return err
			}
		}
	}

	fmt.Println()

	return nil
}

func rehashBlobs(
	ctx context.Context,
	q *dbq.Queries,
	bs blobstore.Store,
	kind blobstore.Kind,
	subtleStyle lipgloss.Style,
) error {
	blobs, err := q.ListBlobsByKind(ctx, string(kind))
	if err != nil {
		return fmt.Errorf("list blobs kind=%s: %w", kind, err)
	}

	total := len(blobs)
	if total == 0 {
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  %s: (no blobs)", kind)))
		return nil
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	buf := make([]byte, 1024*1024) // 1MiB

	var hashed int
	var skippedMissing int

	label := fmt.Sprintf("  %s: rehash", kind)
	// Print an initial line so \r updates have something to overwrite
	fmt.Printf("%s (0/%d)", label, total)

	for i, b := range blobs {
		select {
		case <-ctx.Done():
			fmt.Print("\n")
			return ctx.Err()
		default:
		}

		// Progress update (overwrite same line).
		fmt.Printf("\r%s (%d/%d)", label, i+1, total)

		path, perr := bs.PathFor(kind, b.Sha256)
		if perr != nil {
			fmt.Print("\n")
			return fmt.Errorf("derive blob path kind=%s sha=%s: %w", kind, b.Sha256, perr)
		}

		st, serr := os.Stat(path)
		if serr != nil {
			if errors.Is(serr, os.ErrNotExist) {
				skippedMissing++
				continue
			}
			fmt.Print("\n")
			return fmt.Errorf("stat blob kind=%s sha=%s path=%s: %w", kind, b.Sha256, path, serr)
		}
		if st.Size() != b.SizeBytes {
			fmt.Print("\n")
			return fmt.Errorf(
				"blob size mismatch kind=%s sha=%s path=%s db=%d disk=%d",
				kind, b.Sha256, path, b.SizeBytes, st.Size(),
			)
		}

		f, err := os.Open(path)
		if err != nil {
			fmt.Print("\n")
			return fmt.Errorf("open blob kind=%s sha=%s path=%s: %w", kind, b.Sha256, path, err)
		}

		h := sha256.New()
		_, cerr := blobstore.CopyWithContext(ctx, h, f, buf)
		_ = f.Close()
		if cerr != nil {
			fmt.Print("\n")
			return fmt.Errorf("hash blob kind=%s sha=%s path=%s: %w", kind, b.Sha256, path, cerr)
		}

		sumHex := hex.EncodeToString(h.Sum(nil))
		if sumHex != b.Sha256 {
			fmt.Print("\n")
			return fmt.Errorf(
				"blob hash mismatch kind=%s expected=%s got=%s path=%s",
				kind, b.Sha256, sumHex, path,
			)
		}

		// only after a successful rehash do we update verified_at
		if err := q.TouchBlobVerifiedAt(ctx, dbq.TouchBlobVerifiedAtParams{
			VerifiedAt: sql.NullString{String: now, Valid: true},
			Sha256:     b.Sha256,
		}); err != nil {
			fmt.Print("\n")
			return fmt.Errorf("update verified_at sha=%s: %w", b.Sha256, err)
		}

		hashed++
	}

	// Finish the progress line and print a summary
	fmt.Print("\r") // return to start of line
	fmt.Printf("%s (%d/%d)", label, total, total)
	fmt.Print("\n")
	if skippedMissing > 0 {
		fmt.Println(subtleStyle.Render(fmt.Sprintf("    skipped %d missing blobs", skippedMissing)))
	}
	fmt.Println(subtleStyle.Render(fmt.Sprintf("    verified %d blobs", hashed)))

	return nil
}
