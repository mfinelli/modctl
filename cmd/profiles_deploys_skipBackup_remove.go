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
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesDeploysSkipBackupRemoveGame    string
	profilesDeploysSkipBackupRemoveProfile string
)

var profilesDeploysSkipBackupRemoveCmd = &cobra.Command{
	Use:   "remove <mod_file_version_id> <pattern>",
	Short: "Remove a skip-backup pattern for a mod version",
	Long: `Remove a skip-backup pattern for a mod version in a profile.

Use 'skip-backup list' to see current patterns.`,
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

		if profilesDeploysSkipBackupRemoveGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesDeploysSkipBackupRemoveGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesDeploysSkipBackupRemoveGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesDeploysSkipBackupRemoveProfile)
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

		rows, err := q.RemoveSkipBackupPattern(ctx, dbq.RemoveSkipBackupPatternParams{
			ProfileItemID: itemID,
			Pattern:       pattern,
		})
		if err != nil {
			return fmt.Errorf("remove skip-backup pattern: %w", err)
		}
		if rows == 0 {
			return fmt.Errorf("pattern %q not found for version %d in profile %q", pattern, mfv.ID, p.Name)
		}

		fmt.Printf("Removed skip-backup pattern %q for version %d in profile %q\n", pattern, mfv.ID, p.Name)
		return nil
	},
}

func init() {
	profilesDeploysSkipBackupCmd.AddCommand(profilesDeploysSkipBackupRemoveCmd)

	profilesDeploysSkipBackupRemoveCmd.PersistentFlags().StringVarP(&profilesDeploysSkipBackupRemoveGame, "game", "g", "",
		"Override the currently active game")
	profilesDeploysSkipBackupRemoveCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesDeploysSkipBackupRemoveCmd.PersistentFlags().StringVarP(&profilesDeploysSkipBackupRemoveProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesDeploysSkipBackupRemoveCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
