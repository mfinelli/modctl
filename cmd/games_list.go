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

	"github.com/charmbracelet/lipgloss/table"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/spf13/cobra"
	"go.finelli.dev/util"
)

var gamesListAll bool
var gamesListStore string

var gamesListCmd = &cobra.Command{
	Use:   "list",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

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
		var games []dbq.GameInstall

		if gamesListAll {
			games, err = q.ListAllGameInstalls(ctx)
		} else if gamesListStore != "" {
			games, err = q.ListGameInstallsByStore(ctx, gamesListStore)
		} else {
			// TODO read active-store if it exists and is set

			// we default to steam for now since it's the only
			// store that we support (TODO when we add more stores)
			games, err = q.ListGameInstallsByStore(ctx, "steam")
		}
		if err != nil {
			return fmt.Errorf("error listing games: %w", err)
		}

		rows := [][]string{}
		for _, game := range games {
			present := "✗"
			if util.SqliteIntToBool(game.IsPresent) {
				present = "✓"
			}

			lastSeen := ""
			if game.LastSeenAt.Valid {
				lastSeen = game.LastSeenAt.String
			}

			rows = append(rows, []string{
				fmt.Sprintf(" %d ", game.ID),
				fmt.Sprintf(" %s ", internal.FullSelector(game.StoreID, game.StoreGameID, game.InstanceID)),
				fmt.Sprintf(" %s ", game.DisplayName),
				fmt.Sprintf(" %s ", game.InstallRoot),
				fmt.Sprintf(" %s ", present),
				fmt.Sprintf(" %s ", lastSeen),
			})
		}

		t := table.New().
			Headers(" ID ", " Selector ", " Name ", " Path ", " Present ", " Last Seen ").
			Rows(rows...)

		fmt.Println(t)

		return nil
	},
}

func init() {
	gamesCmd.AddCommand(gamesListCmd)

	gamesListCmd.Flags().BoolVarP(&gamesListAll, "all", "A", false,
		"List games from all stores")

	gamesListCmd.Flags().StringVarP(&gamesListStore, "store", "s", "",
		"List games from the given store")
	gamesListCmd.RegisterFlagCompletionFunc("store",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.StoreIDs(cmd, toComplete)
		})

	gamesListCmd.MarkFlagsMutuallyExclusive("all", "store")
}
