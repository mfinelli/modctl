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

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var profilesSetActiveGame string

var profilesSetActiveCmd = &cobra.Command{
	Use:   "set-active",
	Short: "Set the active profile for the current game",
	Long: `Set which profile is active for the current game install.

Exactly one profile may be active per game at a time. Commands that operate on
profile contents default to the active profile unless --profile is provided.

The current active game is used unless --game is provided.`,
	Args: cobra.ExactArgs(1),
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
		if profilesSetActiveGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesSetActiveGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesSetActiveGame)
		if err != nil {
			return err
		}

		profileName := args[0]

		p, err := q.GetProfileByName(ctx, dbq.GetProfileByNameParams{
			GameInstallID: gi.ID,
			Name:          profileName,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("profile %q not found for this game", profileName)
			}
			return fmt.Errorf("lookup profile: %w", err)
		}
		if p.IsActive != 0 {
			// Already active: treat as idempotent.
			fmt.Printf("Profile %q is already active\n", profileName)
			return nil
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("error starting transaction: %w", err)
		}
		defer tx.Rollback()

		qtx := q.WithTx(tx)

		if err := qtx.DeactivateProfilesForGame(ctx, gi.ID); err != nil {
			return fmt.Errorf("deactivate existing active profile: %w", err)
		}

		if err := qtx.ActivateProfileByName(ctx, dbq.ActivateProfileByNameParams{
			GameInstallID: gi.ID,
			Name:          profileName,
		}); err != nil {
			return fmt.Errorf("activate profile: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		fmt.Printf("Active profile set to %q\n", profileName)

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesSetActiveCmd)

	profilesListCmd.Flags().StringVarP(&profilesListGame, "game", "g", "",
		"Override the currently active game")
	profilesListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
