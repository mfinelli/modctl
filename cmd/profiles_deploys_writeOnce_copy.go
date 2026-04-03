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
	profilesDeploysWriteOnceCopyGame    string
	profilesDeploysWriteOnceCopyProfile string
)

var profilesDeploysWriteOnceCopyCmd = &cobra.Command{
	Use:   "copy <src_mod_file_version_id> <dst_mod_file_version_id>",
	Short: "Copy write-once patterns from one mod version to another in a profile",
	Long: `Copy write-once patterns from one mod version to another within the same profile.

If the destination already has write-once patterns they will be replaced.
If the source has no write-once patterns this is a no-op.

This is useful when manually swapping mod versions and you want to preserve
the write-once configuration from the old version.`,
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

		if profilesDeploysWriteOnceCopyGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesDeploysWriteOnceCopyGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesDeploysWriteOnceCopyGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesDeploysWriteOnceCopyProfile)
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

		return internal.CopyWriteOncePatterns(ctx, db, q, srcItemID, dstItemID, mfvSrc.ID, mfvDst.ID, p.Name)
	},
}

func init() {
	profilesDeploysWriteOnceCmd.AddCommand(profilesDeploysWriteOnceCopyCmd)

	profilesDeploysWriteOnceCopyCmd.PersistentFlags().StringVarP(&profilesDeploysWriteOnceCopyGame, "game", "g", "",
		"Override the currently active game")
	profilesDeploysWriteOnceCopyCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesDeploysWriteOnceCopyCmd.PersistentFlags().StringVarP(&profilesDeploysWriteOnceCopyProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesDeploysWriteOnceCopyCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
