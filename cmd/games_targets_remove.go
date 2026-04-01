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
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var gamesTargetsRemoveGame string

var gamesTargetsRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a custom install target",
	Long: `Remove a user-defined install target.

Discovered targets (game_dir, proton_prefix) cannot be removed.

The target cannot be removed if any installed files still reference it.
Unapply any mods using this target first.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.TargetNames(cmd, toComplete)
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		name := args[0]

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

		if gamesTargetsRemoveGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			gamesTargetsRemoveGame = fmt.Sprintf("%d", active.ActiveGameInstallID)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, gamesTargetsRemoveGame)
		if err != nil {
			return err
		}

		target, err := q.GetTargetByGameInstallAndName(ctx, dbq.GetTargetByGameInstallAndNameParams{
			GameInstallID: gi.ID,
			Name:          name,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("target %q not found", name)
			}
			return fmt.Errorf("get target: %w", err)
		}

		if target.Origin == "discovered" {
			return fmt.Errorf("target %q is auto-discovered and cannot be removed", name)
		}

		installedCount, err := q.CountInstalledFilesForTarget(ctx, target.ID)
		if err != nil {
			return fmt.Errorf("check installed files: %w", err)
		}
		if installedCount > 0 {
			return fmt.Errorf("target %q has %d installed file(s); unapply the profile before removing this target", name, installedCount)
		}

		if err := q.DeleteTarget(ctx, target.ID); err != nil {
			return fmt.Errorf("delete target: %w", err)
		}

		fmt.Printf("Removed target %q.\n", name)
		return nil
	},
}

func init() {
	gamesTargetsCmd.AddCommand(gamesTargetsRemoveCmd)

	gamesTargetsRemoveCmd.Flags().StringVarP(&gamesTargetsRemoveGame, "game", "g", "",
		"Override the currently active game")
	gamesTargetsRemoveCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
