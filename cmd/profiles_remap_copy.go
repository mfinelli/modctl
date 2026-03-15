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
	profilesRemapCopyGame    string
	profilesRemapCopyProfile string
)

var profilesRemapCopyCmd = &cobra.Command{
	Use:   "copy", // <src_mod_file_version_id> <dst_mod_file_version_id>
	Short: "Copy remap rules from one mod version to another in a profile",
	Long: `Copy remap rules from one mod version to another within the same profile.

If the destination already has remap rules they will be replaced.
If the source has no remap rules this is a no-op.

This is useful when upgrading a mod: copy the remap rules from the old
version to the new version before removing the old version from the profile.`,
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

		if profilesRemapCopyGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesRemapCopyGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesRemapCopyGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesRemapCopyProfile)
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

		return internal.CopyRemapConfig(ctx, db, q, srcItemID, dstItemID, mfvSrc.ID, mfvDst.ID, p.Name)
	},
}

func init() {
	profilesRemapCmd.AddCommand(profilesRemapCopyCmd)

	profilesRemapCopyCmd.PersistentFlags().StringVarP(&profilesRemapCopyGame, "game", "g", "",
		"Override the currently active game")
	profilesRemapCopyCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesRemapCopyCmd.PersistentFlags().StringVarP(&profilesRemapCopyProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesRemapCopyCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
