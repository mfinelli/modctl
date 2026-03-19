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
	profilesOverridesPatchRemoveGame    string
	profilesOverridesPatchRemoveProfile string
	profilesOverridesPatchRemoveSection string
)

var profilesOverridesPatchRemoveCmd = &cobra.Command{
	Use:   "remove <path> <key>",
	Short: "Remove a patch entry from a file override",
	Long: `Remove a patch entry from a file override in the active profile.

Removes the patch entry entirely (modctl will no longer patch this key
when applying the override). This is different from 'patch unset' which
marks the key for removal from the file on apply.

If removing the entry leaves the override with no remaining patch entries,
the override row itself is also removed.

For ini patches, use --section to identify entries in a specific section.`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		relpath := filepath.Clean(args[0])
		entryKey := args[1]

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

		if profilesOverridesPatchRemoveGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesPatchRemoveGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesPatchRemoveGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesPatchRemoveProfile)
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

		override, err := q.GetOverride(ctx, dbq.GetOverrideParams{
			ProfileID: p.ID,
			TargetID:  target.ID,
			Relpath:   relpath,
		})
		if err != nil {
			return fmt.Errorf("no patch override found for %q in profile %q", relpath, p.Name)
		}

		if override.OverrideType == "full_file" {
			return fmt.Errorf(
				"override for %q is a full-file override; use 'profiles overrides unset' to remove it",
				relpath,
			)
		}

		var entrySection sql.NullString
		if profilesOverridesPatchRemoveSection != "" {
			if override.OverrideType != "ini_patch" {
				return fmt.Errorf("--section is only valid for ini patch overrides")
			}
			entrySection = sql.NullString{String: profilesOverridesPatchRemoveSection, Valid: true}
		}

		entry, err := q.GetOverridePatchEntryByKey(ctx, dbq.GetOverridePatchEntryByKeyParams{
			OverrideID:   override.ID,
			EntryKey:     entryKey,
			EntrySection: entrySection,
		})
		if err != nil {
			if profilesOverridesPatchRemoveSection != "" {
				return fmt.Errorf("no patch entry found for key %q section %q in override for %q",
					entryKey, profilesOverridesPatchRemoveSection, relpath)
			}
			return fmt.Errorf("no patch entry found for key %q in override for %q",
				entryKey, relpath)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		if err := qtx.DeleteOverridePatchEntry(ctx, dbq.DeleteOverridePatchEntryParams{
			OverrideID: override.ID,
			Position:   entry.Position,
		}); err != nil {
			return fmt.Errorf("delete patch entry: %w", err)
		}

		// remove override row if no entries remain
		remaining, err := qtx.GetMaxOverridePatchPosition(ctx, override.ID)
		if err != nil {
			return fmt.Errorf("check remaining entries: %w", err)
		}

		overrideRemoved := false
		if remaining == -1 {
			// GetMaxOverridePatchPosition returns -1 via COALESCE when no rows exist
			if err := qtx.DeleteOverride(ctx, dbq.DeleteOverrideParams{
				ProfileID: p.ID,
				TargetID:  target.ID,
				Relpath:   relpath,
			}); err != nil {
				return fmt.Errorf("delete empty override: %w", err)
			}
			overrideRemoved = true
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		fmt.Printf("removed patch entry %q from override for %q in profile %q\n",
			entryKey, relpath, p.Name)
		if overrideRemoved {
			fmt.Println("  override removed (no remaining patch entries)")
		}

		return nil
	},
}

func init() {
	profilesOverridesCmd.AddCommand(profilesOverridesPatchRemoveCmd)

	profilesOverridesPatchRemoveCmd.Flags().StringVarP(&profilesOverridesPatchRemoveGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesPatchRemoveCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesOverridesPatchRemoveCmd.Flags().StringVarP(&profilesOverridesPatchRemoveProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesPatchRemoveCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
	profilesOverridesPatchRemoveCmd.Flags().StringVar(&profilesOverridesPatchRemoveSection, "section", "",
		"Section name (ini patches only)")
}
