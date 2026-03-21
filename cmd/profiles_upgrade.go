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
	"strconv"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesUpgradeGame    string
	profilesUpgradeProfile string
	profilesUpgradeTo      string
)

var profilesUpgradeCmd = &cobra.Command{
	Use:   "upgrade <mod-page>",
	Short: "Swap a mod version in a profile for a newer one",
	Long: `Swap the mod file version currently in a profile for a newer one,
preserving the existing priority slot, enabled state, and remap rules.

Without --to, modctl picks the most recently imported version of the same
mod file that is not already in the profile.

With --to, the specified version is used directly.

The old version is removed from the profile but its database record is
preserved.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.ModPageIDs(cmd, toComplete)
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

		if profilesUpgradeGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesUpgradeGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesUpgradeGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesUpgradeProfile)
		if err != nil {
			return err
		}

		mp, err := internal.ResolveModPageArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		// Find the current profile item(s) for this mod page
		items, err := q.GetProfileItemsForModPage(ctx, dbq.GetProfileItemsForModPageParams{
			ProfileID: p.ID,
			ID:        mp.ID,
		})
		if err != nil {
			return fmt.Errorf("lookup profile item: %w", err)
		}
		if len(items) == 0 {
			return fmt.Errorf("mod %q is not in profile %q", mp.Name, p.Name)
		}
		if len(items) > 1 {
			return fmt.Errorf(
				"mod %q has multiple file slots in profile %q; pass --to with a specific version id to disambiguate",
				mp.Name, p.Name,
			)
		}
		item := items[0]

		// Resolve the target version
		var newVersionID int64
		var newVersionString sql.NullString
		var newOriginalName sql.NullString

		if profilesUpgradeTo != "" {
			mfvID, err := internal.ResolveModFileVersionArg(ctx, q, gi, profilesUpgradeTo)
			if err != nil {
				return err
			}

			mfv, err := q.GetModFileVersionByIDForUpgrade(ctx, mfvID.ID)
			if err != nil {
				return err
			}

			// Validate it belongs to the same mod_file
			if mfv.ModFileID != item.ModFileID {
				return fmt.Errorf(
					"version %d does not belong to the same mod file as the current version",
					mfv.ID,
				)
			}
			if mfv.ID == item.ModFileVersionID {
				return fmt.Errorf("version %d is already in profile %q", mfv.ID, p.Name)
			}
			newVersionID = mfv.ID
			newVersionString = mfv.VersionString
			newOriginalName = mfv.OriginalName
		} else {
			candidate, err := q.GetLatestUnusedModFileVersion(ctx, dbq.GetLatestUnusedModFileVersionParams{
				ModFileID: item.ModFileID,
				ProfileID: p.ID,
			})
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf(
						"no unused version found for mod %q; import a new version first or pass --to",
						mp.Name,
					)
				}
				return fmt.Errorf("find latest version: %w", err)
			}
			newVersionID = candidate.ID
			newVersionString = candidate.VersionString
			newOriginalName = candidate.OriginalName
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		if err := qtx.UpdateProfileItemModFileVersion(ctx, dbq.UpdateProfileItemModFileVersionParams{
			ModFileVersionID: newVersionID,
			ID:               item.ID,
		}); err != nil {
			return fmt.Errorf("update profile item: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		oldDesc := fmt.Sprintf("version %d", item.ModFileVersionID)
		newDesc := fmt.Sprintf("version %d", newVersionID)
		if newVersionString.Valid && newVersionString.String != "" {
			newDesc += fmt.Sprintf(" (%s)", newVersionString.String)
		} else if newOriginalName.Valid && newOriginalName.String != "" {
			newDesc += fmt.Sprintf(" (%s)", newOriginalName.String)
		}

		fmt.Printf("Upgraded %q in profile %q\n", mp.Name, p.Name)
		fmt.Printf("  %s → %s (priority %d preserved)\n", oldDesc, newDesc, item.Priority)
		fmt.Println("  run 'modctl apply' to apply changes to disk")

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesUpgradeCmd)

	profilesUpgradeCmd.Flags().StringVarP(&profilesUpgradeGame, "game", "g", "",
		"Override the currently active game")
	profilesUpgradeCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesUpgradeCmd.Flags().StringVarP(&profilesUpgradeProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesUpgradeCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	profilesUpgradeCmd.Flags().StringVar(&profilesUpgradeTo, "to", "",
		"Specific mod file version ID to upgrade to")
	profilesUpgradeCmd.RegisterFlagCompletionFunc("to",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ModFileVersionIDs(cmd, toComplete)
		})
}
