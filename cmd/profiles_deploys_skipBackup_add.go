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
	"errors"
	"fmt"
	"strconv"

	"github.com/mattn/go-sqlite3"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesDeploysSkipBackupAddGame    string
	profilesDeploysSkipBackupAddProfile string
)

var profilesDeploysSkipBackupAddCmd = &cobra.Command{
	Use:   "add <mod_file_version_id> <pattern>",
	Short: "Add a skip-backup pattern for a mod version",
	Long: `Add a skip-backup pattern for a mod version in a profile.

Files matching the pattern (evaluated against the final remapped destination
path) will never be backed up during apply, even if drift is detected or a
pre-existing file would otherwise be saved to the backup store.

The user accepts that matched paths cannot be restored by modctl on unapply;
Steam's "verify integrity of game files" can be used to recover game-owned
files if needed.

Examples:
  modctl profiles deploys skip-backup add 42 "*.cache"
  modctl profiles deploys skip-backup add 42 "Cache/*"`,
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

		pattern := args[1]

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

		if profilesDeploysSkipBackupAddGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesDeploysSkipBackupAddGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesDeploysSkipBackupAddGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesDeploysSkipBackupAddProfile)
		if err != nil {
			return err
		}

		mfv, err := internal.ResolveModFileVersionArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		itemID, err := internal.ResolveProfileItemByVersion(ctx, &p, q, mfv.ID)
		if err != nil {
			return err
		}

		if err := q.AddSkipBackupPattern(ctx, dbq.AddSkipBackupPatternParams{
			ProfileItemID: itemID,
			Pattern:       pattern,
		}); err != nil {
			if isUniqueConstraintError(err) {
				return fmt.Errorf("pattern %q already exists for version %d in profile %q", pattern, mfv.ID, p.Name)
			}
			return fmt.Errorf("add skip-backup pattern: %w", err)
		}

		fmt.Printf("Added skip-backup pattern %q for version %d in profile %q\n", pattern, mfv.ID, p.Name)
		return nil
	},
}

func init() {
	profilesDeploysSkipBackupCmd.AddCommand(profilesDeploysSkipBackupAddCmd)

	profilesDeploysSkipBackupAddCmd.PersistentFlags().StringVarP(&profilesDeploysSkipBackupAddGame, "game", "g", "",
		"Override the currently active game")
	profilesDeploysSkipBackupAddCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesDeploysSkipBackupAddCmd.PersistentFlags().StringVarP(&profilesDeploysSkipBackupAddProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesDeploysSkipBackupAddCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}

func isUniqueConstraintError(err error) bool {
	var se sqlite3.Error
	return errors.As(err, &se) &&
		se.Code == sqlite3.ErrConstraint &&
		se.ExtendedCode == sqlite3.ErrConstraintUnique
}
