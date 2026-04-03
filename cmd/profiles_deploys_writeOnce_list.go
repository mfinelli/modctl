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
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesDeploysWriteOnceListGame    string
	profilesDeploysWriteOnceListProfile string
)

var profilesDeploysWriteOnceListCmd = &cobra.Command{
	Use:   "list <mod_file_version_id>",
	Short: "List write-once patterns for a mod version",
	Long:  `List all write-once patterns for a mod version in a profile.`,
	Args:  cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completion.ModFileVersionIDs(cmd, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
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

		if profilesDeploysWriteOnceListGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesDeploysWriteOnceListGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesDeploysWriteOnceListGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesDeploysWriteOnceListProfile)
		if err != nil {
			return err
		}

		mfv, err := internal.ResolveModFileVersionArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		itemID, err := internal.ResolveProfileItemByVersion(ctx, &p, q, mfv.ID)
		if err != nil {
			return err
		}

		patterns, err := q.ListWriteOncePatterns(ctx, itemID)
		if err != nil {
			return fmt.Errorf("list write-once patterns: %w", err)
		}

		if len(patterns) == 0 {
			fmt.Println(subtleStyle.Render(fmt.Sprintf("  no write-once patterns for version %d in profile %q", mfv.ID, p.Name)))
			return nil
		}

		fmt.Println(boldStyle.Render(fmt.Sprintf("Write-once patterns for version %d in profile %q:", mfv.ID, p.Name)))
		for _, row := range patterns {
			fmt.Printf("  %s\n", row.Pattern)
		}
		return nil
	},
}

func init() {
	profilesDeploysWriteOnceCmd.AddCommand(profilesDeploysWriteOnceListCmd)

	profilesDeploysWriteOnceListCmd.PersistentFlags().StringVarP(&profilesDeploysWriteOnceListGame, "game", "g", "",
		"Override the currently active game")
	profilesDeploysWriteOnceListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesDeploysWriteOnceListCmd.PersistentFlags().StringVarP(&profilesDeploysWriteOnceListProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesDeploysWriteOnceListCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
