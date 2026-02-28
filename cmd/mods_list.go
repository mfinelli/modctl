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
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var modsListGame string

var modsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List imported mods for the current game",
	Long: `List imported mods (mod pages) for a game install.

For each mod page, this shows the latest imported version and how many versions
have been imported in total.

TODO:
  - Show latest version information from the Nexus API for Nexus-linked mods.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract these somewhere else
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

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

		// Resolve game install id: --game overrides active selection
		if modsListGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			modsListGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, modsListGame)
		if err != nil {
			return err
		}

		rows, err := q.ListModsByGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list mods: %w", err)
		}

		if len(rows) == 0 {
			fmt.Println(subtleStyle.Render("No mods imported for this game yet."))
			fmt.Println(subtleStyle.Render("Use `modctl mods import <archive>` to add one."))
			return nil
		}

		fmt.Println(headerStyle.Render("Mods"))
		fmt.Println()

		for _, r := range rows {
			nexusRef := ""
			if r.NexusGameDomain.Valid && r.NexusModID.Valid {
				nexusRef = fmt.Sprintf("%s:%d", r.NexusGameDomain.String, r.NexusModID.Int64)
			}

			latestID := "-"
			if r.ModFileVersionID.Valid {
				latestID = fmt.Sprintf("%d", r.ModFileVersionID.Int64)
			}

			importedAt := "-"
			if r.ImportedAt.Valid && r.ImportedAt.String != "" {
				importedAt = r.ImportedAt.String
			}

			shaShort := "-"
			if r.ArchiveSha256.Valid && r.ArchiveSha256.String != "" {
				shaShort = r.ArchiveSha256.String
				if len(shaShort) > 12 {
					shaShort = shaShort[:12]
				}
			}

			verStr := ""
			if r.VersionString.Valid && r.VersionString.String != "" {
				verStr = r.VersionString.String
			}

			// TODO: nexus latest placeholder
			nexusLatest := "-"
			_ = nexusLatest

			// Primary line
			fmt.Printf("%d  %s\n", r.ModPageID, r.ModName)

			// Details line
			details := fmt.Sprintf(
				"  source=%s  versions=%d  latest_version_id=%s  imported_at=%s  sha=%s",
				r.SourceKind, r.VersionsCount, latestID, importedAt, shaShort,
			)
			if verStr != "" {
				details += fmt.Sprintf("  version=%q", verStr)
			}
			if nexusRef != "" {
				details += fmt.Sprintf("  nexus=%s", nexusRef)
				// TODO: details += fmt.Sprintf("  nexus_latest=%s", nexusLatest)
			}

			fmt.Println(subtleStyle.Render(details))
			fmt.Println()
		}

		return nil
	},
}

func init() {
	modsCmd.AddCommand(modsListCmd)

	modsListCmd.Flags().StringVarP(&modsListGame, "game", "g", "",
		"Override the currently active game")
	modsListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
