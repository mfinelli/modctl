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
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/exporter"
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

Examples:
  modctl export
  modctl export --game steam:1091500 --output cyberpunk-backup.tar.zst
  modctl export --game steam:1091500 --skip-inventory`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract styles
		boldStyle := lipgloss.NewStyle().Bold(true)
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

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
		}

		date := time.Now().Format("20060102")

		if exportGame == "" {
			// Full export
			if exportOutput == "" {
				exportOutput = fmt.Sprintf("modctl-export-%s.tar.zst", date)
			}
			opts.OutputPath = exportOutput

			fmt.Println(boldStyle.Render("Exporting (full)"))
			fmt.Println(subtleStyle.Render("  output: " + exportOutput))
			fmt.Println()

			start := time.Now()
			if err := exporter.Full(ctx, db, q, bs, opts); err != nil {
				return fmt.Errorf("export: %w", err)
			}

			st, _ := os.Stat(exportOutput)
			fmt.Println(okStyle.Render(fmt.Sprintf("  ✓ export complete in %.1fs", time.Since(start).Seconds())))
			if st != nil {
				fmt.Println(subtleStyle.Render(fmt.Sprintf("  size: %s", formatBytes(st.Size()))))
			}
			return nil
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, exportGame)
		if err != nil {
			return err
		}

		if exportOutput == "" {
			slug := exporter.Slugify(gi.DisplayName)
			exportOutput = fmt.Sprintf("modctl-export-%s-%s.tar.zst", slug, date)
		}
		opts.OutputPath = exportOutput

		fmt.Println(boldStyle.Render(fmt.Sprintf("Exporting %s", gi.DisplayName)))
		fmt.Println(subtleStyle.Render("  output: " + exportOutput))
		if exportSkipInventory {
			fmt.Println(subtleStyle.Render("  inventory: skipped"))
		}
		fmt.Println()

		start := time.Now()
		if err := exporter.Game(ctx, db, q, bs, gi, opts); err != nil {
			return fmt.Errorf("export: %w", err)
		}

		st, _ := os.Stat(exportOutput)
		fmt.Println(okStyle.Render(fmt.Sprintf("  ✓ export complete in %.1fs", time.Since(start).Seconds())))
		if st != nil {
			fmt.Println(subtleStyle.Render(fmt.Sprintf("  size: %s", formatBytes(st.Size()))))
		}

		return nil
	},
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
