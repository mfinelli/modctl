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
	profilesRemapRemoveGame    string
	profilesRemapRemoveProfile string
)

var profilesRemapRemoveCmd = &cobra.Command{
	Use:   "remove", // <mod_file_version_id> <position>
	Short: "Remove a remap rule at a given position",
	Long: `Remove a remap rule at the given position for a mod version in a profile.

Use 'remap list' to see current rules and their positions.`,
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

		position, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || position < 0 {
			return fmt.Errorf("invalid position %q (expected a non-negative integer)", args[1])
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

		if profilesRemapRemoveGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesRemapRemoveGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesRemapRemoveGame)
		if err != nil {
			return err
		}

		p, err := internal.ResolveProfileArg(ctx, q, &gi, profilesRemapRemoveProfile)
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

		configID, err := q.GetProfileItemRemapConfigID(ctx, itemID)
		if err != nil {
			return fmt.Errorf("get remap config: %w", err)
		}
		if !configID.Valid {
			return fmt.Errorf("version %d in profile %q has no remap rules", mfv.ID, p.Name)
		}

		if err := q.DeleteRemapRule(ctx, dbq.DeleteRemapRuleParams{
			RemapConfigID: configID.Int64,
			Position:      position,
		}); err != nil {
			return fmt.Errorf("delete remap rule: %w", err)
		}

		fmt.Printf("Removed remap rule at position %d for version %d in profile %q\n",
			position, mfv.ID, p.Name)

		return nil
	},
}

func init() {
	profilesRemapCmd.AddCommand(profilesRemapRemoveCmd)

	profilesRemapRemoveCmd.PersistentFlags().StringVarP(&profilesRemapRemoveGame, "game", "g", "",
		"Override the currently active game")
	profilesRemapRemoveCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesRemapRemoveCmd.PersistentFlags().StringVarP(&profilesRemapRemoveProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesRemapRemoveCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
