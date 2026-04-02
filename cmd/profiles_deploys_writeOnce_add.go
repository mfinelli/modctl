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
	profilesDeploysWriteOnceAddGame    string
	profilesDeploysWriteOnceAddProfile string
)

var profilesDeploysWriteOnceAddCmd = &cobra.Command{
	Use:   "add <mod_file_version_id> <pattern>",
	Short: "Add a write-once pattern for a mod version",
	Long: `Add a write-once pattern for a mod version in a profile.

Files matching the pattern (evaluated against the final remapped destination
path) are deployed on first apply and then left untouched on subsequent
applies, preserving any in-game changes made after the initial deployment.

If a matched file is missing from disk it will be re-deployed regardless.
If the underlying mod file changes, profiles status will surface a warning.

Examples:
  modctl profiles deploys write-once add 42 "settings.ini"
  modctl profiles deploys write-once add 42 "Config/*.cfg"`,
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

		if profilesDeploysWriteOnceAddGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesDeploysWriteOnceAddGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesDeploysWriteOnceAddGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesDeploysWriteOnceAddProfile)
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

		if err := q.AddWriteOncePattern(ctx, dbq.AddWriteOncePatternParams{
			ProfileItemID: itemID,
			Pattern:       pattern,
		}); err != nil {
			if isUniqueConstraintError(err) {
				return fmt.Errorf("pattern %q already exists for version %d in profile %q", pattern, mfv.ID, p.Name)
			}
			return fmt.Errorf("add write-once pattern: %w", err)
		}

		fmt.Printf("Added write-once pattern %q for version %d in profile %q\n", pattern, mfv.ID, p.Name)
		return nil
	},
}

func init() {
	profilesDeploysWriteOnceCmd.AddCommand(profilesDeploysWriteOnceAddCmd)

	profilesDeploysWriteOnceAddCmd.PersistentFlags().StringVarP(&profilesDeploysWriteOnceAddGame, "game", "g", "",
		"Override the currently active game")
	profilesDeploysWriteOnceAddCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesDeploysWriteOnceAddCmd.PersistentFlags().StringVarP(&profilesDeploysWriteOnceAddProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesDeploysWriteOnceAddCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
