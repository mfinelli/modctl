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
	profilesOverridesCopyGame    string
	profilesOverridesCopyProfile string
	profilesOverridesCopyForce   bool
)

var profilesOverridesCopyCmd = &cobra.Command{
	Use:   "copy <src-profile>",
	Short: "Copy overrides from another profile into the active profile",
	Long: `Copy all overrides from a source profile into the active profile.

Overrides are independent after copying and can diverge freely.
To re-sync, delete overrides from the destination and copy again.

Errors if the active profile already has overrides at any of the same
paths unless --force is passed, in which case conflicting overrides
are replaced.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.ProfileNames(cmd, toComplete)
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

		if profilesOverridesCopyGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesCopyGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesCopyGame)
		if err != nil {
			return err
		}

		// dst is the active profile
		dst, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesCopyProfile)
		if err != nil {
			return err
		}

		// src is the named argument
		src, err := argresolver.ResolveProfileArg(ctx, q, &gi, args[0])
		if err != nil {
			return fmt.Errorf("source profile: %w", err)
		}

		if src.ID == dst.ID {
			return fmt.Errorf("source and destination profiles are the same")
		}

		srcOverrides, err := q.ListOverridesByProfile(ctx, src.ID)
		if err != nil {
			return fmt.Errorf("list source overrides: %w", err)
		}

		if len(srcOverrides) == 0 {
			fmt.Printf("source profile %q has no overrides\n", src.Name)
			return nil
		}

		// check for conflicts unless --force
		if !profilesOverridesCopyForce {
			dstOverrides, err := q.ListOverridesByProfile(ctx, dst.ID)
			if err != nil {
				return fmt.Errorf("list destination overrides: %w", err)
			}
			dstPaths := make(map[string]struct{}, len(dstOverrides))
			for _, o := range dstOverrides {
				dstPaths[o.Relpath] = struct{}{}
			}
			var conflicts []string
			for _, o := range srcOverrides {
				if _, ok := dstPaths[o.Relpath]; ok {
					conflicts = append(conflicts, o.Relpath)
				}
			}
			if len(conflicts) > 0 {
				fmt.Printf("destination profile %q already has overrides at:\n", dst.Name)
				for _, c := range conflicts {
					fmt.Printf("  %s\n", c)
				}
				return fmt.Errorf("pass --force to replace conflicting overrides")
			}
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		if err := qtx.CopyOverridesToProfile(ctx, dbq.CopyOverridesToProfileParams{
			ProfileID:   src.ID,
			ProfileID_2: dst.ID,
		}); err != nil {
			return fmt.Errorf("copy overrides: %w", err)
		}

		if err := qtx.CopyOverridePatchEntriesToProfile(ctx, dbq.CopyOverridePatchEntriesToProfileParams{
			ProfileID:   src.ID,
			ProfileID_2: dst.ID,
		}); err != nil {
			return fmt.Errorf("copy patch entries: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		fmt.Printf("copied %d override(s) from profile %q to %q\n",
			len(srcOverrides), src.Name, dst.Name)
		if !profilesOverridesCopyForce {
			_ = sql.ErrNoRows // suppress unused import if needed
		}
		return nil
	},
}

func init() {
	profilesOverridesCmd.AddCommand(profilesOverridesCopyCmd)

	profilesOverridesCopyCmd.Flags().StringVarP(&profilesOverridesCopyGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesCopyCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesOverridesCopyCmd.Flags().StringVarP(&profilesOverridesCopyProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesCopyCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
	profilesOverridesCopyCmd.Flags().BoolVar(&profilesOverridesCopyForce, "force", false,
		"Replace conflicting overrides in the destination profile")
}
