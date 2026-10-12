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

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
)

var gamesTargetsListGame string

var gamesTargetsListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List install targets for a game",
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		db, err := openDB(ctx)
		if err != nil {
			return err
		}
		defer db.Close()

		q := dbq.New(db)

		gi, err := gameInstall(ctx, q, gamesTargetsListGame)
		if err != nil {
			return err
		}

		targets, err := q.ListTargetsForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list targets: %w", err)
		}

		if len(targets) == 0 {
			style.Println("No targets found.")
			return nil
		}

		rows := [][]string{}
		for _, t := range targets {
			targetState := "enabled"
			if !internal.TargetEnabled(t) {
				targetState = "disabled"
			}
			rows = append(rows, []string{
				fmt.Sprintf(" %d ", t.ID),
				fmt.Sprintf(" %s ", t.Name),
				fmt.Sprintf(" %s ", t.RootPath),
				fmt.Sprintf(" %s ", t.Origin),
				fmt.Sprintf(" %s ", targetState),
			})
		}

		style.Println(style.Table([]string{" ID ", " Name ", " Path ", " Origin ", " State "}, rows))
		return nil
	},
}

func init() {
	gamesTargetsCmd.AddCommand(gamesTargetsListCmd)

	gamesTargetsListCmd.Flags().StringVarP(&gamesTargetsListGame, "game", "g", "",
		"Override the currently active game")
	gamesTargetsListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().GameInstallSelectors(cmd, toComplete)
		})
}
