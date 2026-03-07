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
	"errors"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/restore"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/mod/semver"
)

var (
	importForce         bool
	importDryRun        bool
	importSkipInventory bool
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import a modctl export bundle",
	Long: `Import a modctl export bundle.

Restores state from a bundle produced by 'modctl export'. Supports both
full and game-scoped bundles.

For full bundles, the destination database must be empty (beyond the
auto-seeded store rows). Use --force to wipe and restore into an existing
installation.

For game-scoped bundles, the game must not already exist in the destination
database. Use --force to overwrite an existing game install.

Note: import does not apply any profiles automatically. After importing,
run 'modctl profiles set-active' and 'modctl apply' to deploy mods.

Use --dry-run to preview what would be imported without making any changes.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract these
		boldStyle := lipgloss.NewStyle().Bold(true)
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

		ctx := cmd.Context()

		bundlePath := args[0]

		// Open and validate bundle before touching the DB
		fmt.Println(boldStyle.Render("Validating bundle..."))
		bundle, err := restore.OpenAndValidate(ctx, bundlePath)
		if err != nil {
			return fmt.Errorf("invalid bundle: %w", err)
		}
		defer bundle.Close()

		fmt.Println(subtleStyle.Render(fmt.Sprintf("  format version: %d", bundle.Manifest.ExportFormatVersion)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  export kind:    %s", bundle.Manifest.ExportKind)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  exported at:    %s", bundle.Manifest.ExportedAt.Format("2006-01-02 15:04:05 UTC"))))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  modctl version: %s", bundle.Manifest.ModctlVersion)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  schema version: %d", bundle.Manifest.SchemaVersion)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  archives:       %d", bundle.Manifest.Counts.Archives)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  backups:        %d", bundle.Manifest.Counts.Backups)))
		if bundle.Manifest.Game != nil {
			fmt.Println(subtleStyle.Render(fmt.Sprintf("  game:           %s (%s:%s)",
				bundle.Manifest.Game.DisplayName,
				bundle.Manifest.Game.StoreID,
				bundle.Manifest.Game.StoreGameID,
			)))
		}
		fmt.Println(okStyle.Render("  ✓ bundle integrity OK"))
		fmt.Println()

		if err := internal.EnsureDBExists(); err != nil {
			return err
		}
		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close() // NB if we do a full restore we'll close this
		//    twice, but it shouldn't be a problem
		if err := internal.MigrateDB(ctx, db); err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		q := dbq.New(db)

		bs := blobstore.Store{
			ArchivesDir:  viper.GetString("archives_dir"),
			BackupsDir:   viper.GetString("backups_dir"),
			OverridesDir: viper.GetString("overrides_dir"),
			TmpDir:       viper.GetString("tmp_dir"),
		}

		opts := restore.Options{
			Force:         importForce,
			DryRun:        importDryRun,
			SkipInventory: importSkipInventory,
		}

		// Warn if bundle modctl version is newer
		bundleVer := "v" + bundle.Manifest.ModctlVersion
		currentVer := "v" + rootCmd.Version
		if semver.IsValid(bundleVer) && semver.IsValid(currentVer) &&
			semver.Compare(bundleVer, currentVer) > 0 {
			fmt.Println(warnStyle.Render(fmt.Sprintf(
				"  ⚠ bundle was created with modctl %s (current: %s) - import may not work correctly",
				bundle.Manifest.ModctlVersion, rootCmd.Version,
			)))
			fmt.Println()
		}

		if importDryRun {
			fmt.Println(boldStyle.Render("Dry run - no changes will be made"))
			fmt.Println()
		}

		var result restore.Result
		var importErr error

		switch bundle.Manifest.ExportKind {
		case "full":
			fmt.Println(boldStyle.Render("Importing (full)..."))
			result, importErr = restore.Full(
				ctx, db, q, bs, bundle, opts,
				viper.GetString("database"),
				rootCmd.Version,
				logger,
			)
		case "game":
			fmt.Println(boldStyle.Render(fmt.Sprintf("Importing game: %s...",
				bundle.Manifest.Game.DisplayName)))
			result, importErr = restore.Game(ctx, db, q, bs, bundle, opts, rootCmd.Version, logger)
		default:
			return fmt.Errorf("unknown export kind %q", bundle.Manifest.ExportKind)
		}

		if importErr != nil {
			if errors.Is(importErr, context.Canceled) {
				return fmt.Errorf("cancelled")
			}
			return fmt.Errorf("import failed: %w", importErr)
		}

		fmt.Println()
		if importDryRun {
			fmt.Println(boldStyle.Render("Would import:"))
		} else {
			fmt.Println(boldStyle.Render("Import complete"))
		}
		fmt.Printf("  archives:  %d\n", result.Archives)
		fmt.Printf("  backups:   %d\n", result.Backups)
		if result.ModPages > 0 {
			fmt.Printf("  mod pages: %d\n", result.ModPages)
		}
		if result.Profiles > 0 {
			fmt.Printf("  profiles:  %d\n", result.Profiles)
		}
		if result.InventoryScanned > 0 {
			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"  inventoried %d archive(s)", result.InventoryScanned,
			)))
		}
		if result.InventoryFailed > 0 {
			fmt.Println(warnStyle.Render(fmt.Sprintf(
				"  ⚠ %d archive(s) failed inventory scan - run 'mods scan-inventory' to retry",
				result.InventoryFailed,
			)))
		}
		if !importDryRun {
			fmt.Println()
			fmt.Println(subtleStyle.Render(
				"  note: no profiles have been applied - run 'modctl profiles set-active' and 'modctl apply' to deploy mods",
			))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(importCmd)

	importCmd.Flags().BoolVar(&importForce, "force", false,
		"Overwrite existing data (wipe DB for full, overwrite game for game-scoped)")
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false,
		"Preview what would be imported without making any changes")
	importCmd.Flags().BoolVar(&importSkipInventory, "skip-inventory", false,
		"Skip scanning archives that have no inventory in the bundle")
}
