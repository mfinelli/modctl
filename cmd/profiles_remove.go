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

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/style"
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
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completer().ModFileVersionIDs(cmd, toComplete)
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		db, err := openDB(ctx)
		if err != nil {
			return err
		}
		defer db.Close()

		q := dbq.New(db)

		// Resolve game install id: --game overrides active selection
		gi, err := gameInstall(ctx, q, profilesRenameGame)
		if err != nil {
			return err
		}

		p, err := argresolver.Profile(ctx, q, gi, profilesAddProfile)
		if err != nil {
			return err
		}

		mfv, err := argresolver.ModFileVersion(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		// Locate the profile item row
		id, err := q.GetProfileItemIDByVersion(ctx, dbq.GetProfileItemIDByVersionParams{
			ProfileID:        p.ID,
			ModFileVersionID: mfv.ID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("version %d is not in profile %q", mfv.ID, p.Name)
			}
			return fmt.Errorf("lookup profile item: %w", err)
		}

		err = database.WithTx(ctx, db, func(qtx *dbq.Queries) error {
			if err := qtx.DeleteProfileItemByID(ctx, id); err != nil {
				return fmt.Errorf("remove from profile: %w", err)
			}

			return nil
		})
		if err != nil {
			return err
		}

		style.Printf("Removed version %d from profile %q\n", mfv.ID, p.Name)
		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesRemoveCmd)

	profilesRemoveCmd.Flags().StringVarP(&profilesRemoveGame, "game", "g", "",
		"Override the currently active game")
	profilesRemoveCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().GameInstallSelectors(cmd, toComplete)
		})

	profilesRemoveCmd.Flags().StringVarP(&profilesRemoveProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesRemoveCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().ProfileNames(cmd, toComplete)
		})

}
