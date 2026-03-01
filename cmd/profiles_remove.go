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
	profilesRemoveGame    string
	profilesRemoveProfile string
)

var profilesRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a mod version from a profile",
	Long: `Remove a mod file version from a profile.

This permanently removes the version from the profile (opposite of "add").
It does not change files on disk; changes take effect the next time you apply.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		versionID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || versionID <= 0 {
			return fmt.Errorf("invalid mod_file_version_id %q (expected a positive integer)", args[0])
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
		if profilesRenameGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesRenameGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesRenameGame)
		if err != nil {
			return err
		}

		p, err := internal.ResolveProfileArg(ctx, q, &gi, profilesAddProfile)
		if err != nil {
			return err
		}

		// Locate the profile item row
		id, err := q.GetProfileItemIDByVersion(ctx, dbq.GetProfileItemIDByVersionParams{
			ProfileID:        p.ID,
			ModFileVersionID: versionID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("version %d is not in profile %q", versionID, p.Name)
			}
			return fmt.Errorf("lookup profile item: %w", err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("error starting transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		if err := qtx.DeleteProfileItemByID(ctx, id); err != nil {
			return fmt.Errorf("remove from profile: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		fmt.Printf("Removed version %d from profile %q\n", versionID, p.Name)
		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesRemoveCmd)

	profilesRemoveCmd.Flags().StringVarP(&profilesRemoveGame, "game", "g", "",
		"Override the currently active game")
	profilesRemoveCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesRemoveCmd.Flags().StringVar(&profilesRemoveProfile, "profile", "p",
		"Override the currently active profile")
	profilesRemoveCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

}
