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

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var modsIncompatibleRemoveGame string

var modsIncompatibleRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove an incompatibility flag between two mods",
	Long: `Remove an incompatibility flag between two mods.

The order of the two IDs does not matter.`,
	Args: cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 2 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.ModPageIDs(cmd, toComplete)
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		// Resolve game install id: --game overrides active selection
		if modsIncompatibleRemoveGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			modsIncompatibleRemoveGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, modsIncompatibleRemoveGame)
		if err != nil {
			return err
		}

		mpA, err := internal.ResolveModPageArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		mpB, err := internal.ResolveModPageArg(ctx, q, gi, args[1])
		if err != nil {
			return err
		}

		if mpA.ID == mpB.ID {
			return fmt.Errorf("mod-page-id-a and mod-page-id-b must be different")
		}

		// Verify both mod pages exist and belong to the current game install
		pageA, err := q.GetModPage(ctx, mpA.ID)
		if err != nil {
			return fmt.Errorf("mod page %d not found", mpA.ID)
		}
		pageB, err := q.GetModPage(ctx, mpB.ID)
		if err != nil {
			return fmt.Errorf("mod page %d not found", mpB.ID)
		}
		if pageA.GameInstallID != gi.ID || pageB.GameInstallID != gi.ID {
			return fmt.Errorf("both mod pages must belong to the current game install")
		}

		n, err := q.RemoveModIncompatibility(ctx, dbq.RemoveModIncompatibilityParams{
			ModPageIDA: mpA.ID,
			ModPageIDB: mpB.ID,
		})
		if err != nil {
			return fmt.Errorf("removing incompatibility: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("no incompatibility flag found between mod pages %d and %d", mpA.ID, mpB.ID)
		}

		fmt.Printf("Removed incompatibility flag between mod pages %d and %d\n", mpA.ID, mpB.ID)
		return nil
	},
}

func init() {
	modsIncompatibleCmd.AddCommand(modsIncompatibleRemoveCmd)

	modsIncompatibleRemoveCmd.Flags().StringVarP(&modsIncompatibleRemoveGame, "game", "g", "",
		"Override the currently active game")
	modsIncompatibleRemoveCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
