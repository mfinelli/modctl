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

var (
	profilesOrderCompactGame    string
	profilesOrderCompactProfile string

	profilesOrderCompactMultiple int64
)

var profilesOrderCompactCmd = &cobra.Command{
	Use:   "compact",
	Short: "Renumber priorities in a profile",
	Long: `Renumber priorities to a compact sequence while preserving order.

By default, priorities are reassigned as 1, 2, 3, ... in the current order.
Use --multiple to assign priorities as N, 2N, 3N, ... (for example, --multiple
10).`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		step := int64(1)
		if profilesOrderCompactMultiple > 0 {
			step = profilesOrderCompactMultiple
		}
		if step <= 0 {
			return fmt.Errorf("--multiple must be > 0")
		}

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
		if profilesOrderCompactGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOrderCompactGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesOrderCompactGame)
		if err != nil {
			return err
		}

		p, err := internal.ResolveProfileArg(ctx, q, &gi, profilesOrderCompactProfile)
		if err != nil {
			return err
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("error starting transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		items, err := qtx.ListProfileItemsForOrder(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("list profile items: %w", err)
		}
		if len(items) == 0 {
			fmt.Printf("Profile %q has no items\n", p.Name)
			return nil
		}

		if err := internal.RewriteProfilePriorities(ctx, qtx, p.ID, items, 1, step); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		if profilesOrderCompactMultiple > 0 {
			fmt.Printf("Compacted priorities in profile %q using multiple %d\n", p.Name, profilesOrderCompactMultiple)
		} else {
			fmt.Printf("Compacted priorities in profile %q\n", p.Name)
		}

		return nil
	},
}

func init() {
	profilesOrderCmd.AddCommand(profilesOrderCompactCmd)

	profilesOrderCompactCmd.Flags().StringVarP(&profilesOrderCompactGame, "game", "g", "",
		"Override the currently active game")
	profilesOrderCompactCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesOrderCompactCmd.Flags().StringVar(&profilesOrderCompactProfile, "profile", "p",
		"Override the currently active profile")
	profilesOrderCompactCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	profilesOrderCompactCmd.Flags().Int64VarP(&profilesOrderCompactMultiple, "multiple", "m", 1,
		"Assign priorities with a multiple")
}
