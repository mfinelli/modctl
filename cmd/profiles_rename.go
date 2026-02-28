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
	"strconv"
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
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
			return completion.ProfileNames(cmd, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if profilesRenameGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesRenameGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesRenameGame)
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
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return fmt.Errorf("profile %q already exists for this game", newName)
			}
			return fmt.Errorf("rename profile: %w", err)
		}

		fmt.Printf("Renamed profile %q -> %q\n", oldName, newName)

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesRenameCmd)

	profilesRenameCmd.Flags().StringVarP(&profilesListGame, "game", "g", "",
		"Override the currently active game")
	profilesRenameCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
