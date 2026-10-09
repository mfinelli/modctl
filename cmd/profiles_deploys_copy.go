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
	profilesDeploysCopyGame    string
	profilesDeploysCopyProfile string
)

var profilesDeploysCopyCmd = &cobra.Command{
	Use:   "copy <src_mod_file_version_id> <dst_mod_file_version_id>",
	Short: "Copy all deployment rules from one mod version to another in a profile",
	Long: `Copy all deployment rules, both skip-backup and write-once patterns, from one
mod version to another within the same profile. Both kinds are copied in a
single transaction, so either all of them are copied or none are.

For each kind that the source has patterns of, the patterns the destination
already has of that kind are replaced. A kind the source has none of is left as
it is on the destination.

This is useful when manually swapping mod versions and you want to preserve the
deployment rules from the old version. To copy only one kind, use
'deploys skip-backup copy' or 'deploys write-once copy'.`,
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

		if err := internal.EnsureDBExists(); err != nil {
			return err
		}
		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()
		if err := internal.MigrateDB(ctx, db); err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		q := dbq.New(db)

		if profilesDeploysCopyGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesDeploysCopyGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesDeploysCopyGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesDeploysCopyProfile)
		if err != nil {
			return err
		}

		mfvSrc, err := internal.ResolveModFileVersionArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		mfvDst, err := internal.ResolveModFileVersionArg(ctx, q, gi, args[1])
		if err != nil {
			return err
		}

		srcItemID, err := internal.ResolveProfileItemByVersion(ctx, &p, q, mfvSrc.ID)
		if err != nil {
			return fmt.Errorf("source: %w", err)
		}

		dstItemID, err := internal.ResolveProfileItemByVersion(ctx, &p, q, mfvDst.ID)
		if err != nil {
			return fmt.Errorf("destination: %w", err)
		}

		skipBackup, writeOnce, err := internal.CopyDeployRules(ctx, db, q, srcItemID, dstItemID)
		if err != nil {
			return err
		}
		if skipBackup == 0 && writeOnce == 0 {
			fmt.Printf("Version %d in profile %q has no deploy rules to copy\n", mfvSrc.ID, p.Name)
			return nil
		}
		if skipBackup > 0 {
			fmt.Printf("Copied %d skip-backup pattern(s) from version %d to version %d in profile %q\n",
				skipBackup, mfvSrc.ID, mfvDst.ID, p.Name)
		}
		if writeOnce > 0 {
			fmt.Printf("Copied %d write-once pattern(s) from version %d to version %d in profile %q\n",
				writeOnce, mfvSrc.ID, mfvDst.ID, p.Name)
		}
		return nil
	},
}

func init() {
	profilesDeploysCmd.AddCommand(profilesDeploysCopyCmd)

	profilesDeploysCopyCmd.PersistentFlags().StringVarP(&profilesDeploysCopyGame, "game", "g", "",
		"Override the currently active game")
	profilesDeploysCopyCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesDeploysCopyCmd.PersistentFlags().StringVarP(&profilesDeploysCopyProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesDeploysCopyCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
