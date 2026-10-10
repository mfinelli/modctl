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
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/exporter"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	exportGame          string
	exportOutput        string
	exportSkipInventory bool
	exportNoVerify      bool
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export modctl state to a portable bundle",
	Long: `Export modctl state to a portable bundle.

By default performs a full export of all games, mods, profiles, and blobs.
Use --game to export only the data relevant to a single game install.

The bundle is a zstd-compressed tar archive containing a database snapshot,
all referenced blob files, and a manifest. It can be restored with 'import'.

Game-scoped bundles do not include backup blobs, as those only have meaning
on the machine where the game is installed.

Examples:
  modctl export
  modctl export --game steam:1091500 --output cyberpunk-backup.tar.zst
  modctl export --game steam:1091500 --skip-inventory`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

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

		q := dbq.New(db)

		bs := blobstore.Store{
			ArchivesDir:  viper.GetString("archives_dir"),
			BackupsDir:   viper.GetString("backups_dir"),
			OverridesDir: viper.GetString("overrides_dir"),
		}

		opts := exporter.Options{
			ModctlVersion: rootCmd.Version,
			SkipInventory: exportSkipInventory,
			NoVerify:      exportNoVerify,
			CacheDBPath:   filepath.Join(viper.GetString("cache_dir"), "nexus_cache.db"),
			Progress:      printExportProgress,
		}

		date := time.Now().Format("20060102")

		if exportGame == "" {
			// Full export
			if exportOutput == "" {
				exportOutput = fmt.Sprintf("modctl-export-%s.tar.zst", date)
			}
			opts.OutputPath = exportOutput

			style.Println(style.Bold.Render("Exporting (full)"))
			style.Println(style.Subtle.Render("  output: " + exportOutput))
			style.Println()

			start := time.Now()
			result, err := exporter.Full(ctx, db, q, bs, opts)
			if err != nil {
				return fmt.Errorf("export: %w", err)
			}
			warnSkippedBlobs(result)

			st, _ := os.Stat(exportOutput)
			style.Println(style.Success.Render(fmt.Sprintf("  ✓ export complete in %.1fs", time.Since(start).Seconds())))
			if st != nil {
				style.Println(style.Subtle.Render(fmt.Sprintf("  size: %s", style.Bytes(st.Size()))))
			}
			return nil
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, exportGame)
		if err != nil {
			return err
		}

		if exportOutput == "" {
			slug := exporter.Slugify(gi.DisplayName)
			exportOutput = fmt.Sprintf("modctl-export-%s-%s.tar.zst", slug, date)
		}
		opts.OutputPath = exportOutput

		style.Println(style.Bold.Render(fmt.Sprintf("Exporting %s", gi.DisplayName)))
		style.Println(style.Subtle.Render("  output: " + exportOutput))
		if exportSkipInventory {
			style.Println(style.Subtle.Render("  inventory: skipped"))
		}
		style.Println()

		start := time.Now()
		result, err := exporter.Game(ctx, db, q, bs, gi, opts)
		if err != nil {
			return fmt.Errorf("export: %w", err)
		}
		warnSkippedBlobs(result)

		st, _ := os.Stat(exportOutput)
		style.Println(style.Success.Render(fmt.Sprintf("  ✓ export complete in %.1fs", time.Since(start).Seconds())))
		if st != nil {
			style.Println(style.Subtle.Render(fmt.Sprintf("  size: %s", style.Bytes(st.Size()))))
		}

		return nil
	},
}

// printExportProgress shows blob verification as a single updating line.
func printExportProgress(p exporter.Progress) {
	switch p.Kind {
	case exporter.VerifyStarted:
		style.Printf("  verifying blobs (0/%d)", p.Total)
	case exporter.VerifyBlob:
		style.Printf("\r  verifying blobs (%d/%d)", p.Done, p.Total)
	case exporter.VerifyFinished:
		style.Printf("\r%-60s\r", "")
		style.Printf("  verified %d blob(s)\n", p.Total)
	case exporter.VerifyFailed:
		// end the progress line so the error starts on a line of its own
		style.Print("\n")
	}
}

// warnSkippedBlobs tells the user about blobs that were left out of an export
// because they were missing from disk.
func warnSkippedBlobs(r exporter.Result) {
	for _, sha := range r.SkippedBlobs {
		style.Fprintf(os.Stderr, "warning: blob %s missing from disk, skipped in export\n", style.ShortSha(sha))
	}
}

func init() {
	rootCmd.AddCommand(exportCmd)

	exportCmd.Flags().StringVarP(&exportGame, "game", "g", "",
		"Export only data for this game install")
	exportCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "",
		"Output file path (default: modctl-export-<date>.tar.zst)")
	exportCmd.Flags().BoolVar(&exportSkipInventory, "skip-inventory", false,
		"Omit archive inventory entries from the export")
	exportCmd.Flags().BoolVar(&exportNoVerify, "no-verify", false,
		"Skip blob integrity verification before exporting")
}
