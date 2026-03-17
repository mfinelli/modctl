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
	"path/filepath"
	"strconv"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesOverridesUnsetGame    string
	profilesOverridesUnsetProfile string
)

var profilesOverridesUnsetCmd = &cobra.Command{
	Use:   "unset <path>",
	Short: "Remove an override for a path in a profile",
	Long: `Remove the override for a path in the active profile.

The override blob becomes eligible for garbage collection.
If the override was applied, the next apply will reconcile the
path back to the mod winner or remove it if no mod provides it.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		relpath := filepath.Clean(args[0])

		if filepath.IsAbs(relpath) {
			return fmt.Errorf("path must be relative, got %q", relpath)
		}

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

		if profilesOverridesUnsetGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesUnsetGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesUnsetGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesUnsetProfile)
		if err != nil {
			return err
		}

		target, err := q.GetTargetByName(ctx, dbq.GetTargetByNameParams{
			GameInstallID: gi.ID,
			Name:          "game_dir",
		})
		if err != nil {
			return fmt.Errorf("resolve game_dir target: %w", err)
		}

		// verify it exists before deleting so we can give a clear error
		_, err = q.GetOverride(ctx, dbq.GetOverrideParams{
			ProfileID: p.ID,
			TargetID:  target.ID,
			Relpath:   relpath,
		})
		if err != nil {
			return fmt.Errorf("no override found for path %q in profile %q", relpath, p.Name)
		}

		if err := q.DeleteOverride(ctx, dbq.DeleteOverrideParams{
			ProfileID: p.ID,
			TargetID:  target.ID,
			Relpath:   relpath,
		}); err != nil {
			return fmt.Errorf("delete override: %w", err)
		}

		fmt.Printf("override removed for %q in profile %q\n", relpath, p.Name)
		fmt.Println("  run 'modctl apply' to reconcile the game directory")
		return nil
	},
}

func init() {
	profilesOverridesCmd.AddCommand(profilesOverridesUnsetCmd)

	profilesOverridesUnsetCmd.Flags().StringVarP(&profilesOverridesUnsetGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesUnsetCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesOverridesUnsetCmd.Flags().StringVarP(&profilesOverridesUnsetProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesUnsetCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
