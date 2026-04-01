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

	"github.com/mattn/go-sqlite3"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"go.finelli.dev/util"
)

var (
	profilesAddGame    string
	profilesAddProfile string
	profilesAddTarget  string

	profilesAddPriority int64
	profilesAddDisabled bool
)

var profilesAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a mod version to a profile",
	Long: `Add a pinned mod file version to a profile.

By default, this adds to the active profile for the current game. You can
override the target profile with --profile.

If --priority is not provided, modctl assigns the next highest priority in the
profile. Higher priority wins conflicts.

Use --target to specify which install target the mod should be deployed to
(e.g. "game_dir", "proton_prefix", or a custom target name). Defaults to
"game_dir".`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
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

		// Resolve game install id: --game overrides active selection
		if profilesAddGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesAddGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesAddGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesAddProfile)
		if err != nil {
			return err
		}

		mfv, err := internal.ResolveModFileVersionArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		// Resolve target: default to game_dir.
		targetName := profilesAddTarget
		if targetName == "" {
			targetName = "game_dir"
		}

		target, err := q.GetTargetByGameInstallAndName(ctx, dbq.GetTargetByGameInstallAndNameParams{
			GameInstallID: gi.ID,
			Name:          targetName,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("target %q not found for game %q; run `modctl games targets list` to see available targets", targetName, gi.DisplayName)
			}
			return fmt.Errorf("resolve target %q: %w", targetName, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("error starting transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		// Validate mod_file_version exists (nicer than FK failure).
		if _, err := qtx.ExistsModFileVersion(ctx, mfv.ID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("mod file version %d not found", mfv.ID)
			}
			return fmt.Errorf("check mod file version: %w", err)
		}

		// Compute priority if not explicitly provided.
		priority := profilesAddPriority
		if profilesAddPriority == 0 {
			maxPrio, err := qtx.GetMaxPriorityForProfile(ctx, p.ID)
			if err != nil {
				return fmt.Errorf("get max priority: %w", err)
			}
			priority = maxPrio + 1
		} else {
			// Explicit priority: ensure it isn't already in use for this profile.
			_, err := qtx.IsPriorityTaken(ctx, dbq.IsPriorityTakenParams{
				ProfileID: p.ID,
				Priority:  profilesAddPriority,
			})
			if err == nil {
				return fmt.Errorf("priority %d is already used in profile %q", profilesAddPriority, p.Name)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("check priority: %w", err)
			}
		}

		enabledVal := util.SqliteBoolToInt(true) // default enabled=true
		if profilesAddDisabled {
			enabledVal = util.SqliteBoolToInt(false)
		}

		itemID, err := qtx.CreateProfileItem(ctx, dbq.CreateProfileItemParams{
			ProfileID:        p.ID,
			ModFileVersionID: mfv.ID,
			TargetID:         target.ID,
			Enabled:          enabledVal,
			Priority:         priority,
		})
		if err != nil {
			// NOTE: This tells us it was a UNIQUE constraint, but not which one.
			// We rely on the explicit IsPriorityTaken check for a clear duplicate-priority message.
			var se sqlite3.Error
			if errors.As(err, &se) {
				if se.Code == sqlite3.ErrConstraint && se.ExtendedCode == sqlite3.ErrConstraintUnique {
					// Most common case: duplicate version in profile
					// (UNIQUE(profile_id, mod_file_version_id)).
					// If user provided a priority on the command line
					// duplicate priority should have been caught above
					// unless a race occurred.
					if profilesAddPriority != 0 {
						return fmt.Errorf("could not add version %d to profile %q (duplicate version or priority conflict)", mfv.ID, p.Name)
					}
					return fmt.Errorf("version %d is already in profile %q", mfv.ID, p.Name)
				}
				if se.Code == sqlite3.ErrConstraint && se.ExtendedCode == sqlite3.ErrConstraintForeignKey {
					// Should be prevented by ExistsModFileVersion,
					// but keep a friendly message anyway.
					return fmt.Errorf("invalid reference while adding version %d to profile %q", mfv.ID, p.Name)
				}
			}
			return fmt.Errorf("add to profile: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		fmt.Printf("Added version %d to profile %q (item_id=%d, priority=%d, enabled=%t, target=%s)\n",
			mfv.ID, p.Name, itemID, priority, enabledVal != 0, target.Name)

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesAddCmd)

	profilesAddCmd.Flags().StringVarP(&profilesAddGame, "game", "g", "",
		"Override the currently active game")
	profilesAddCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesAddCmd.Flags().StringVarP(&profilesAddProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesAddCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	profilesAddCmd.Flags().StringVarP(&profilesAddTarget, "target", "t", "",
		`Install target to deploy this mod to (default "game_dir")`)
	profilesAddCmd.RegisterFlagCompletionFunc("target",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.TargetNames(cmd, toComplete)
		})

	profilesAddCmd.Flags().Int64Var(&profilesAddPriority, "priority", 0,
		"Priority (higher wins conflicts). Defaults to next available.")

	profilesAddCmd.Flags().BoolVar(&profilesAddDisabled, "disable", false,
		"Add the item disabled (enabled=false)")
}
