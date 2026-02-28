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

var profilesListGame string

var profilesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List profiles for the current game",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract these somewhere else
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

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
		if profilesListGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesListGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesListGame)
		if err != nil {
			return err
		}

		rows, err := q.ListProfilesByGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list profiles: %w", err)
		}

		if len(rows) == 0 {
			fmt.Println(subtleStyle.Render("No profiles found"))
			return nil
		}

		fmt.Println(headerStyle.Render("Profiles"))
		fmt.Println()

		for _, p := range rows {
			prefix := "  "
			if p.IsActive != 0 {
				prefix = okStyle.Render("  * ")
			}
			fmt.Printf("%s%s\n", prefix, p.Name)

			if p.Description.Valid && p.Description.String != "" {
				fmt.Println(subtleStyle.Render("    " + p.Description.String))
			}
		}

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesListCmd)

	profilesListCmd.Flags().StringVarP(&profilesListGame, "game", "g", "",
		"Override the currently active game")
	profilesListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
