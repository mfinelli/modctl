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

	"github.com/mattn/go-sqlite3"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesOrderSetGame    string
	profilesOrderSetProfile string
)

var profilesOrderSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set the priority of a mod version in a profile",
	Long: `Set the numeric priority of a mod file version within a profile.

Higher priority wins conflicts. Priorities must be unique within a profile.`,
	Args: cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completion.ModFileVersionIDs(cmd, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		newPrio, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || newPrio <= 0 {
			return fmt.Errorf("invalid priority %q (expected a positive integer)", args[1])
		}

		err = internal.EnsureDBExists()
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
		if profilesOrderSetGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOrderSetGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesOrderSetGame)
		if err != nil {
			return err
		}

		p, err := internal.ResolveProfileArg(ctx, q, &gi, profilesOrderSetProfile)
		if err != nil {
			return err
		}

		mfv, err := internal.ResolveModFileVersionArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("error starting transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		// Find the item and current priority.
		item, err := qtx.GetProfileItemByVersionForOrder(ctx, dbq.GetProfileItemByVersionForOrderParams{
			ProfileID:        p.ID,
			ModFileVersionID: mfv.ID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("version %d is not in profile %q", mfv.ID, p.Name)
			}
			return fmt.Errorf("lookup profile item: %w", err)
		}

		if item.Priority == newPrio {
			fmt.Printf("Priority for version %d is already %d in profile %q\n", mfv.ID, newPrio, p.Name)
			return nil
		}

		// Ensure the target priority isn't already taken
		_, err = qtx.IsPriorityTaken(ctx, dbq.IsPriorityTakenParams{
			ProfileID: p.ID,
			Priority:  newPrio,
		})
		if err == nil {
			return fmt.Errorf("priority %d is already used in profile %q", newPrio, p.Name)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check priority: %w", err)
		}

		if err := qtx.SetProfileItemPriorityByID(ctx, dbq.SetProfileItemPriorityByIDParams{
			Priority: newPrio,
			ID:       item.ID,
		}); err != nil {
			// Race-proof fallback: unique constraint could still trip
			var se sqlite3.Error
			if errors.As(err, &se) && se.Code == sqlite3.ErrConstraint && se.ExtendedCode == sqlite3.ErrConstraintUnique {
				return fmt.Errorf("priority %d is already used in profile %q", newPrio, p.Name)
			}
			return fmt.Errorf("set priority: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		fmt.Printf("Set priority for version %d to %d in profile %q\n", mfv.ID, newPrio, p.Name)

		return nil
	},
}

func init() {
	profilesOrderCmd.AddCommand(profilesOrderSetCmd)

	profilesOrderSetCmd.Flags().StringVarP(&profilesOrderSetGame, "game", "g", "",
		"Override the currently active game")
	profilesOrderSetCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesOrderSetCmd.Flags().StringVarP(&profilesOrderSetProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOrderSetCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
