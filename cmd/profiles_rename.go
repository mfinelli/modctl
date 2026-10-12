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
	"database/sql"
	"errors"
	"fmt"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
)

var profilesRenameGame string

var profilesRenameCmd = &cobra.Command{
	Use:   "rename",
	Short: "Rename a profile for the current game",
	Long: `Rename an existing profile for the current game install.

Profiles are named mod sets scoped to a single game. This command updates the
profile name; it does not change which mods are in the profile.

Profile names must be unique per game.`,
	Args: cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Complete only the first positional arg (old profile name).
		if len(args) == 0 {
			return completer().ProfileNames(cmd, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		db, err := openDB(ctx)
		if err != nil {
			return err
		}
		defer db.Close()

		q := dbq.New(db)

		// Resolve game install id: --game overrides active selection
		gi, err := gameInstall(ctx, q, profilesRenameGame)
		if err != nil {
			return err
		}

		oldName := args[0]
		newName := args[1]

		// Friendly "not found" error
		p, err := q.GetProfileByName(ctx, dbq.GetProfileByNameParams{
			GameInstallID: gi.ID,
			Name:          oldName,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("profile %q not found for this game", oldName)
			}
			return fmt.Errorf("lookup profile: %w", err)
		}

		// Attempt rename
		if err := q.RenameProfile(ctx, dbq.RenameProfileParams{
			Name: newName,
			ID:   p.ID,
		}); err != nil {
			if database.IsUniqueConstraint(err) {
				return fmt.Errorf("profile %q already exists for this game", newName)
			}
			return fmt.Errorf("rename profile: %w", err)
		}

		style.Printf("Renamed profile %q -> %q\n", oldName, newName)

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesRenameCmd)

	profilesRenameCmd.Flags().StringVarP(&profilesListGame, "game", "g", "",
		"Override the currently active game")
	profilesRenameCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().GameInstallSelectors(cmd, toComplete)
		})
}
