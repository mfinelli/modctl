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
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-sqlite3"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	gcDryRun       bool
	gcNoArchives   bool
	gcNoBackups    bool
	gcMinAge       string
	gcCleanMissing bool
	gcSkipOrphans  bool
)

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Garbage collect unreferenced blobs from the blob store",
	Long: `Garbage collect unreferenced blobs from the blob store.

By default, both archive and backup blobs are collected. Use --no-archives
or --no-backups to restrict which kinds are processed.

A blob is eligible for collection when no database row references it:
  - Archives: not referenced by any mod_file_versions row
  - Backups:  not referenced by any backups row

On-disk files with no corresponding database row (orphans) are also removed
by default. These can appear when an import was interrupted before the
database entry was written. Use --skip-orphans to leave them in place.

If a blob is recorded in the database but missing from disk, a warning is
printed. Pass --clean-missing to also remove those dangling database rows.

Use --min-age to protect recently created blobs from collection, e.g.
--min-age 7d skips any blob created within the last 7 days.

Use --dry-run to preview what would be removed without making any changes.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract styles
		boldStyle := lipgloss.NewStyle().Bold(true)
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		dryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		if err := internal.EnsureDBExists(); err != nil {
			return err
		}
		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()
		if err := internal.MigrateDB(ctx, db); err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		var minAge time.Duration
		if gcMinAge != "" {
			d, err := parseGCDuration(gcMinAge)
			if err != nil {
				return fmt.Errorf("invalid --min-age %q: %w", gcMinAge, err)
			}
			minAge = d
		}

		bs := blobstore.Store{
			ArchivesDir:  viper.GetString("archives_dir"),
			BackupsDir:   viper.GetString("backups_dir"),
			OverridesDir: viper.GetString("overrides_dir"),
		}

		q := dbq.New(db)

		if gcDryRun {
			fmt.Println(boldStyle.Render("Garbage collection (dry run)"))
		} else {
			fmt.Println(boldStyle.Render("Garbage collection"))
		}
		if minAge > 0 {
			fmt.Println(subtleStyle.Render(fmt.Sprintf("  min-age: %s (skipping blobs newer than %s)",
				gcMinAge, minAge.String())))
		}
		fmt.Println()

		var kinds []blobstore.Kind
		if !gcNoArchives {
			kinds = append(kinds, blobstore.KindArchive)
		}
		if !gcNoBackups {
			kinds = append(kinds, blobstore.KindBackup)
		}

		if len(kinds) == 0 {
			fmt.Println(subtleStyle.Render("  nothing to do (--no-archives and --no-backups both set)"))
			return nil
		}

		var totalRemoved int
		var totalRemovedBytes int64
		var totalOrphans int
		var totalOrphanBytes int64
		var totalMissingCleaned int

		for _, kind := range kinds {
			fmt.Println(boldStyle.Render(fmt.Sprintf("%s blobs", kind)))

			res, err := runGC(ctx, q, bs, kind, gcOptions{
				dryRun:       gcDryRun,
				minAge:       minAge,
				cleanMissing: gcCleanMissing,
				skipOrphans:  gcSkipOrphans,
			}, subtleStyle, okStyle, warnStyle, redStyle, dryStyle)
			if err != nil {
				return fmt.Errorf("gc %s: %w", kind, err)
			}

			// per-kind summary
			fmt.Println()
			if res.removed == 0 && res.orphans == 0 && res.missingCleaned == 0 {
				fmt.Println(subtleStyle.Render("  nothing collected"))
			} else {
				if res.removed > 0 {
					action := "removed"
					if gcDryRun {
						action = "would remove"
					}
					fmt.Printf("  %s\n",
						okStyle.Render(fmt.Sprintf("%s %d unreferenced blob(s) (%s)",
							action, res.removed, formatBytes(res.removedBytes))))
				}
				if res.orphans > 0 {
					action := "removed"
					if gcDryRun {
						action = "would remove"
					}
					fmt.Printf("  %s\n",
						okStyle.Render(fmt.Sprintf("%s %d orphan file(s) (%s)",
							action, res.orphans, formatBytes(res.orphanBytes))))
				}
				if res.missingCleaned > 0 {
					action := "cleaned"
					if gcDryRun {
						action = "would clean"
					}
					fmt.Printf("  %s\n",
						warnStyle.Render(fmt.Sprintf("%s %d missing blob DB row(s)",
							action, res.missingCleaned)))
				}
			}
			for _, w := range res.warnings {
				fmt.Println(warnStyle.Render("  ⚠  " + w))
			}
			fmt.Println()

			totalRemoved += res.removed
			totalRemovedBytes += res.removedBytes
			totalOrphans += res.orphans
			totalOrphanBytes += res.orphanBytes
			totalMissingCleaned += res.missingCleaned
		}

		// overall summary if we processed more than one kind
		if len(kinds) > 1 {
			fmt.Println(boldStyle.Render("Total"))
			freed := totalRemovedBytes + totalOrphanBytes
			if gcDryRun {
				fmt.Printf("  would free %s across %d blob(s)\n",
					formatBytes(freed), totalRemoved+totalOrphans)
			} else {
				fmt.Printf("  freed %s across %d blob(s)\n",
					formatBytes(freed), totalRemoved+totalOrphans)
			}
			if totalMissingCleaned > 0 {
				fmt.Printf("  %s\n",
					warnStyle.Render(fmt.Sprintf("cleaned %d missing DB row(s)", totalMissingCleaned)))
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(gcCmd)

	gcCmd.Flags().BoolVar(&gcDryRun, "dry-run", false,
		"Preview what would be removed without making any changes")
	gcCmd.Flags().BoolVar(&gcNoArchives, "no-archives", false,
		"Skip garbage collection of archive blobs")
	gcCmd.Flags().BoolVar(&gcNoBackups, "no-backups", false,
		"Skip garbage collection of backup blobs")
	gcCmd.Flags().StringVar(&gcMinAge, "min-age", "",
		"Skip blobs created more recently than this duration (e.g. 24h, 7d, 2w)")
	gcCmd.Flags().BoolVar(&gcCleanMissing, "clean-missing", false,
		"Remove database rows for blobs that are missing from disk")
	gcCmd.Flags().BoolVar(&gcSkipOrphans, "skip-orphans", false,
		"Skip on-disk files with no database row (orphans are removed by default)")
}

type gcOptions struct {
	dryRun       bool
	minAge       time.Duration
	cleanMissing bool
	skipOrphans  bool
}

type gcResult struct {
	removed        int
	removedBytes   int64
	orphans        int
	orphanBytes    int64
	missing        int
	missingCleaned int
	warnings       []string
}

func runGC(
	ctx context.Context,
	q *dbq.Queries,
	bs blobstore.Store,
	kind blobstore.Kind,
	opts gcOptions,
	subtleStyle, okStyle, warnStyle, redStyle, dryStyle lipgloss.Style,
) (gcResult, error) {
	var res gcResult

	cutoff := time.Time{}
	if opts.minAge > 0 {
		cutoff = time.Now().UTC().Add(-opts.minAge)
	}

	// Step 1: unreferenced DB rows
	blobs, err := q.ListUnreferencedBlobs(ctx, string(kind))
	if err != nil {
		return res, fmt.Errorf("list unreferenced blobs: %w", err)
	}

	for _, b := range blobs {
		// min-age guard
		if !cutoff.IsZero() {
			createdAt, err := time.Parse("2006-01-02T15:04:05.000Z", b.CreatedAt)
			if err == nil && createdAt.After(cutoff) {
				fmt.Printf("  %s %s %s\n",
					subtleStyle.Render("~"),
					b.Sha256[:16]+"...",
					subtleStyle.Render("(skipped, too new)"))
				continue
			}
		}

		path, err := bs.PathFor(kind, b.Sha256)
		if err != nil {
			return res, fmt.Errorf("derive path for %s: %w", b.Sha256, err)
		}

		st, statErr := os.Stat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				res.missing++
				fmt.Printf("  %s %s %s\n",
					warnStyle.Render("?"),
					b.Sha256[:16]+"...",
					warnStyle.Render("(db row exists but file missing from disk)"))
				if opts.cleanMissing {
					if !opts.dryRun {
						if err := q.DeleteBlob(ctx, b.Sha256); err != nil {
							var se sqlite3.Error
							if errors.As(err, &se) {
								if se.Code == sqlite3.ErrConstraint && se.ExtendedCode == sqlite3.ErrConstraintForeignKey {
									return res, fmt.Errorf("blob %s is still referenced by another row; skipping", b.Sha256)
								}
							}
							return res, fmt.Errorf("delete missing blob row %s: %w", b.Sha256, err)
						}
					}
					res.missingCleaned++
					fmt.Printf("  %s %s\n",
						dryStyle.Render("↳"),
						subtleStyle.Render("db row removed"))
				}
				continue
			}
			return res, fmt.Errorf("stat %s: %w", path, statErr)
		}

		name := b.Sha256[:16] + "..."
		if b.OriginalName.Valid {
			name = fmt.Sprintf("%s %s", b.OriginalName.String, subtleStyle.Render("("+b.Sha256[:16]+"...)"))
		}

		if opts.dryRun {
			fmt.Printf("  %s %s %s\n",
				dryStyle.Render("-"),
				name,
				subtleStyle.Render(formatBytes(st.Size())))
		} else {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return res, fmt.Errorf("remove blob file %s: %w", path, err)
			}
			// best-effort: remove fan-out dir if now empty; ignore error
			_ = os.Remove(filepath.Dir(path))
			if err := q.DeleteBlob(ctx, b.Sha256); err != nil {
				var se sqlite3.Error
				if errors.As(err, &se) {
					if se.Code == sqlite3.ErrConstraint && se.ExtendedCode == sqlite3.ErrConstraintForeignKey {
						return res, fmt.Errorf("blob %s is still referenced by another row; skipping", b.Sha256)
					}
				}
				return res, fmt.Errorf("delete blob row %s: %w", b.Sha256, err)
			}
			fmt.Printf("  %s %s %s\n",
				redStyle.Render("-"),
				name,
				subtleStyle.Render(formatBytes(st.Size())))
		}

		res.removed++
		res.removedBytes += st.Size()
	}

	// Step 2: orphaned on-disk files
	if !opts.skipOrphans {
		root, err := bs.RootFor(kind)
		if err != nil {
			return res, fmt.Errorf("root for kind %s: %w", kind, err)
		}

		orphans, err := findOrphans(ctx, q, root)
		if err != nil {
			return res, fmt.Errorf("find orphans: %w", err)
		}

		for _, o := range orphans {
			// min-age guard using file mtime for orphans (no DB row to check)
			if !cutoff.IsZero() && o.modTime.After(cutoff) {
				fmt.Printf("  %s %s %s\n",
					subtleStyle.Render("~"),
					o.sha256[:16]+"...",
					subtleStyle.Render("(orphan skipped, too new)"))
				continue
			}

			fmt.Printf("  %s %s %s %s\n",
				warnStyle.Render("!"),
				o.sha256[:16]+"...",
				subtleStyle.Render(formatBytes(o.size)),
				warnStyle.Render("(orphan: no db row)"))

			if opts.dryRun {
				fmt.Printf("  %s %s\n",
					dryStyle.Render("↳"),
					subtleStyle.Render("would remove"))
			} else {
				if err := os.Remove(o.path); err != nil && !os.IsNotExist(err) {
					return res, fmt.Errorf("remove orphan %s: %w", o.path, err)
				}
				fmt.Printf("  %s %s\n",
					redStyle.Render("↳"),
					subtleStyle.Render("removed"))
			}

			res.orphans++
			res.orphanBytes += o.size
		}
	}

	return res, nil
}

type orphanFile struct {
	sha256  string
	path    string
	size    int64
	modTime time.Time
}

// findOrphans walks the blob store root directory and returns any files
// whose sha256 filename has no corresponding row in the blobs table.
func findOrphans(ctx context.Context, q *dbq.Queries, root string) ([]orphanFile, error) {
	var orphans []orphanFile

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
		// blob filenames are 64-char lowercase hex sha256
		if len(name) != 64 {
			return nil
		}

		_, dbErr := q.GetBlob(ctx, name)
		if dbErr == nil {
			// known blob, not an orphan
			return nil
		}
		if !errors.Is(dbErr, sql.ErrNoRows) {
			return fmt.Errorf("get blob %s: %w", name, dbErr)
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat orphan %s: %w", path, err)
		}

		orphans = append(orphans, orphanFile{
			sha256:  name,
			path:    path,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		return nil
	})

	if err != nil {
		return nil, err
	}
	return orphans, nil
}

// parseGCDuration parses a duration string supporting h, d, and w suffixes.
// Examples: "24h", "7d", "2w"
func parseGCDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// find where the numeric part ends
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("no numeric value found")
	}
	if i == len(s) {
		return 0, fmt.Errorf("missing unit (use h, d, or w)")
	}

	var n int64
	fmt.Sscanf(s[:i], "%d", &n)
	if n <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}

	unit := strings.ToLower(s[i:])
	switch unit {
	case "h":
		return time.Duration(n) * time.Hour, nil
	case "d":
		return time.Duration(n) * 24 * time.Hour, nil
	case "w":
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown unit %q (use h, d, or w)", unit)
	}
}
