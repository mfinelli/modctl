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

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var gamesSetActiveCmd = &cobra.Command{
	Use:   "set-active",
	Short: "Set the active game install for future commands",
	Long: `Set the active game install used by modctl commands.

Accepts either a numeric install ID or a selector:

  steam:1091500
  steam:1091500#default

If the instance is omitted and multiple installs exist, you must specify the
desired instance explicitly.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.GameInstallSelectors(cmd, toComplete)
	},
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
		gi, err := internal.ResolveGameInstallArg(ctx, q, args[0])
		if err != nil {
			return err
		}

		return persistActiveGameInstall(gi)
	},
}

func init() {
	gamesCmd.AddCommand(gamesSetActiveCmd)
}

func persistActiveGameInstall(gi dbq.GameInstall) error {
	a, err := state.LoadActive()
	if err != nil {
		return err
	}

	fullSel := internal.FullSelector(gi.StoreID, gi.StoreGameID, gi.InstanceID)

	a.ActiveStoreID = gi.StoreID // keeps store context in sync
	a.ActiveGameInstallID = gi.ID
	a.ActiveGameInstallSelector = fullSel

	if err := state.SaveActive(a); err != nil {
		return err
	}

	fmt.Printf("Active game set to %s (%s)\n", fullSel, gi.DisplayName)
	return nil
}
