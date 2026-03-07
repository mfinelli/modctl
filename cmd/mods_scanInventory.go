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

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/archivescanner"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var modsScanInventoryCmd = &cobra.Command{
	Use:   "scan-inventory",
	Short: "Scan archive contents for all mods that have not yet been inventoried",
	Long: `Scan archive contents for all mods that have not yet been inventoried.

Runs bsdtar against each un-inventoried archive and records its contents in
the database. This allows conflict planning and status checks to operate
without re-reading archives from disk.

Scanning is performed automatically during 'mods import'. Use this command
to populate inventory for archives that were skipped during import, or if a
previous scan was interrupted.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract these somewhere else
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		boldStyle := lipgloss.NewStyle().Bold(true)

		ctx := cmd.Context()

		err := internal.EnsureDBExists()
		if err != nil {
			return err
		}

		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()

		err = internal.MigrateDB(ctx, db)
		if err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		q := dbq.New(db)

		bs := blobstore.Store{
			ArchivesDir: viper.GetString("archives_dir"),
		}

		scanner := archivescanner.Scanner{
			BsdtarPath: viper.GetString("bsdtar"),
		}

		result, err := archivescanner.ScanAll(
			ctx,
			db,
			q,
			bs,
			scanner,
			logger,
		)
		if err != nil {
			return fmt.Errorf("scan-inventory: %w", err)
		}

		if result.Scanned == 0 && result.Failed == 0 {
			fmt.Println(subtleStyle.Render("  all archives already inventoried, nothing to do"))
			return nil
		}

		fmt.Println(boldStyle.Render("Inventory scan complete:"))
		fmt.Printf("  scanned: %d\n", result.Scanned)

		if result.Failed > 0 {
			fmt.Println(warnStyle.Render(fmt.Sprintf("  failed:  %d (see logs for details)", result.Failed)))
			return fmt.Errorf("%d archive(s) failed to scan", result.Failed)
		}

		return nil
	},
}

func init() {
	modsCmd.AddCommand(modsScanInventoryCmd)
}
