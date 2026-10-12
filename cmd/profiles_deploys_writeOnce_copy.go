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

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/style"
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

		gi, err := gameInstall(ctx, q, profilesDeploysWriteOnceCopyGame)
		if err != nil {
			return err
		}

		p, err := argresolver.Profile(ctx, q, gi, profilesDeploysWriteOnceCopyProfile)
		if err != nil {
			return err
		}

		mfvSrc, err := argresolver.ModFileVersion(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		mfvDst, err := argresolver.ModFileVersion(ctx, q, gi, args[1])
		if err != nil {
			return err
		}

		srcItemID, err := argresolver.ProfileItemByVersion(ctx, q, p, mfvSrc.ID)
		if err != nil {
			return fmt.Errorf("source: %w", err)
		}

		dstItemID, err := argresolver.ProfileItemByVersion(ctx, q, p, mfvDst.ID)
		if err != nil {
			return fmt.Errorf("destination: %w", err)
		}

		copied, err := internal.CopyWriteOncePatterns(ctx, db, q, srcItemID, dstItemID)
		if err != nil {
			return err
		}
		if copied == 0 {
			style.Printf("Version %d in profile %q has no write-once patterns to copy\n", mfvSrc.ID, p.Name)
			return nil
		}
		style.Printf("Copied %d write-once pattern(s) from version %d to version %d in profile %q\n",
			copied, mfvSrc.ID, mfvDst.ID, p.Name)
		return nil
	},
}

func init() {
	profilesDeploysWriteOnceCmd.AddCommand(profilesDeploysWriteOnceCopyCmd)

	profilesDeploysWriteOnceCopyCmd.PersistentFlags().StringVarP(&profilesDeploysWriteOnceCopyGame, "game", "g", "",
		"Override the currently active game")
	profilesDeploysWriteOnceCopyCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().GameInstallSelectors(cmd, toComplete)
		})
	profilesDeploysWriteOnceCopyCmd.PersistentFlags().StringVarP(&profilesDeploysWriteOnceCopyProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesDeploysWriteOnceCopyCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().ProfileNames(cmd, toComplete)
		})
}
