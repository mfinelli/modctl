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

var gamesTargetsEnableGame string

var gamesTargetsEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable an install target",
	Long:  `Enable an install target that was disabled with 'games targets disable'.`,
	Args:  cobra.ExactArgs(1),
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

		if gamesTargetsEnableGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			gamesTargetsEnableGame = fmt.Sprintf("%d", active.ActiveGameInstallID)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, gamesTargetsEnableGame)
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

		if internal.TargetEnabled(target) {
			fmt.Printf("Target %q is already enabled.\n", name)
			return nil
		}

		if err := q.SetTargetEnabled(ctx, dbq.SetTargetEnabledParams{
			Enabled: 1,
			ID:      target.ID,
		}); err != nil {
			return fmt.Errorf("enable target: %w", err)
		}

		fmt.Printf("Enabled target %q.\n", name)
		return nil
	},
}

func init() {
	gamesTargetsCmd.AddCommand(gamesTargetsEnableCmd)

	gamesTargetsEnableCmd.Flags().StringVarP(&gamesTargetsEnableGame, "game", "g", "",
		"Override the currently active game")
	gamesTargetsEnableCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
