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

var (
	profilesDeleteGame      string
	profilesDeleteForce     bool
	profilesDeleteYesReally bool
)

var profilesDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a profile for the current game",
	Long: `Delete a profile for the current game install.

Deleting a profile removes its definition from modctl (profile items, overrides,
and related metadata). It does not change the filesystem; any installed files
remain tracked via installed_files until you run apply/unapply later.

Safety checks:
- If the profile is currently active (the default profile for commands), you
  must pass --force.
- If the profile is the last applied profile for this game, you must pass
  --delete-applied.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.ProfileNames(cmd, toComplete)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		profileName := args[0]

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
		if profilesDeleteGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesDeleteGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesDeleteGame)
		if err != nil {
			return err
		}

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

		// Check applied profile guard.
		appliedID, err := q.GetAppliedProfileIDForGame(ctx, gi.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("game install %d not found", gi.ID)
			}
			return fmt.Errorf("get applied profile: %w", err)
		}

		isApplied := appliedID.Valid && appliedID.Int64 == p.ID
		isActive := p.IsActive != 0

		// Enforce safety flags.
		if isActive && !profilesDeleteForce {
			return fmt.Errorf("profile %q is currently active; pass --force to delete it", p.Name)
		}

		if isApplied && !profilesDeleteYesReally {
			return fmt.Errorf(
				"profile %q appears to be the last applied profile for this game; deleting it will not change files on disk and cannot be undone. pass --delete-applied to confirm",
				p.Name,
			)
		}

		// print a warning when doing the dangerous thing.
		if isApplied && profilesDeleteYesReally {
			fmt.Fprintf(os.Stderr,
				"warning: deleting applied profile %q will not change files on disk; state will be considered unknown until apply/unapply reconciles it\n",
				p.Name,
			)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("error starting transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		if err := qtx.DeleteProfileByID(ctx, p.ID); err != nil {
			return fmt.Errorf("delete profile: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		fmt.Printf("Deleted profile %q\n", p.Name)

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesDeleteCmd)

	profilesDeleteCmd.Flags().StringVarP(&profilesDeleteGame, "game", "g", "",
		"Override the currently active game")
	profilesDeleteCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesDeleteCmd.Flags().BoolVar(&profilesDeleteForce, "force", false,
		"Allow deleting the profile even if it is currently active")
	profilesDeleteCmd.Flags().BoolVar(&profilesDeleteYesReally, "delete-applied", false,
		"Allow deleting the profile even if it is the last applied profile for this game")
}
