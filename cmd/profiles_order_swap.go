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
	"strconv"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesOrderSwapGame    string
	profilesOrderSwapProfile string
)

var profilesOrderSwapCmd = &cobra.Command{
	Use:   "swap",
	Short: "Swap the priorities of two mod versions in a profile",
	Long: `Swap the numeric priorities of two mod file versions within a profile.

This is a safe way to reorder under the "unique priority per profile" rule,
without having to choose unused priority numbers.`,
	Args: cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 2 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.ModFileVersionIDs(cmd, toComplete)
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
		if profilesAddGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesAddGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesAddGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesAddProfile)
		if err != nil {
			return err
		}

		mfvA, err := internal.ResolveModFileVersionArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		mfvB, err := internal.ResolveModFileVersionArg(ctx, q, gi, args[1])
		if err != nil {
			return err
		}
		if mfvA.ID == mfvB.ID {
			return fmt.Errorf("swap requires two different mod file version ids")
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("error starting transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		// Look up both items in the profile.
		a, err := qtx.GetProfileItemByVersionForOrder(ctx, dbq.GetProfileItemByVersionForOrderParams{
			ProfileID:        p.ID,
			ModFileVersionID: mfvA.ID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("version %d is not in profile %q", mfvA.ID, p.Name)
			}
			return fmt.Errorf("lookup version %d: %w", mfvA.ID, err)
		}

		b, err := qtx.GetProfileItemByVersionForOrder(ctx, dbq.GetProfileItemByVersionForOrderParams{
			ProfileID:        p.ID,
			ModFileVersionID: mfvB.ID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("version %d is not in profile %q", mfvB.ID, p.Name)
			}
			return fmt.Errorf("lookup version %d: %w", mfvB.ID, err)
		}

		if a.Priority == b.Priority {
			// Should be impossible with the unique index, but just in case...
			return fmt.Errorf("cannot swap: both versions have the same priority (%d)", a.Priority)
		}

		// Use a sentinel priority that is guaranteed free.
		maxPrio, err := qtx.GetMaxPriorityForProfile(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("get max priority: %w", err)
		}
		sentinel := maxPrio + 1

		// A -> sentinel
		if err := qtx.SetProfileItemPriorityByID(ctx, dbq.SetProfileItemPriorityByIDParams{
			Priority: sentinel,
			ID:       a.ID,
		}); err != nil {
			return fmt.Errorf("swap (step 1): %w", err)
		}

		// B -> old A
		if err := qtx.SetProfileItemPriorityByID(ctx, dbq.SetProfileItemPriorityByIDParams{
			Priority: a.Priority,
			ID:       b.ID,
		}); err != nil {
			return fmt.Errorf("swap (step 2): %w", err)
		}

		// A -> old B
		if err := qtx.SetProfileItemPriorityByID(ctx, dbq.SetProfileItemPriorityByIDParams{
			Priority: b.Priority,
			ID:       a.ID,
		}); err != nil {
			return fmt.Errorf("swap (step 3): %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		fmt.Printf("Swapped priorities in profile %q: %d(%d) <-> %d(%d)\n",
			p.Name, mfvA.ID, b.Priority, mfvB.ID, a.Priority)

		return nil
	},
}

func init() {
	profilesOrderCmd.AddCommand(profilesOrderSwapCmd)

	profilesOrderSwapCmd.Flags().StringVarP(&profilesOrderSwapGame, "game", "g", "",
		"Override the currently active game")
	profilesOrderSwapCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesOrderSwapCmd.Flags().StringVarP(&profilesOrderSwapProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOrderSwapCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
