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
	"github.com/mfinelli/modctl/internal"
	"github.com/spf13/cobra"
)

// gamesRefreshCmd represents the gamesRefresh command
var gamesRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Discover installed games from enabled stores",
	Long: `Scan all enabled stores and update the list of discovered game installs.

This command detects installed games, updates their install paths, and marks
missing installs as not present.

It is safe to run multiple times.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract...
		boldStyle := lipgloss.NewStyle().Bold(true)
		styles := internal.RefreshStyles{
			Bold:   boldStyle,
			Subtle: lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
			Warn:   lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
			Green:  lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
			Red:    lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
			Yellow: lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
			Cyan:   lipgloss.NewStyle().Foreground(lipgloss.Color("14")),
		}

		ctx := cmd.Context()

		err := internal.EnsureDBExists()
		if err != nil {
			return err
		}

		db, err := internal.SetupDB()
		if err != nil {
			return err
		}
		defer db.Close()

		err = internal.MigrateDB(ctx, db)
		if err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		fmt.Println(boldStyle.Render("Scanning stores..."))
		fmt.Println()

		result, err := internal.ScanStores(ctx, db, styles)
		if err != nil {
			return err
		}

		// Summary
		fmt.Println()
		fmt.Println(boldStyle.Render("Done."))
		total := len(result.New) + len(result.Updated) + len(result.Returned)
		fmt.Printf("  %d game(s) found", total)
		if len(result.New) > 0 {
			fmt.Printf(", %d new", len(result.New))
		}
		if len(result.Returned) > 0 {
			fmt.Printf(", %d returned", len(result.Returned))
		}
		if len(result.Missing) > 0 {
			fmt.Printf(", %d missing", len(result.Missing))
		}
		if len(result.Skipped) > 0 {
			fmt.Printf(", %d skipped", len(result.Skipped))
		}
		fmt.Println()

		return nil
	},
}

func init() {
	gamesCmd.AddCommand(gamesRefreshCmd)
}
