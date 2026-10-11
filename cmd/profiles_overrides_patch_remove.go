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

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/style"
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
		relpath, err := argresolver.ResolveRelpathArg(args[0])
		if err != nil {
			return err
		}
		entryKey := args[1]

		db, err := database.Open(ctx)
		if err != nil {
			return err
		}
		defer db.Close()

		q := dbq.New(db)

		gi, err := argresolver.ResolveGameInstall(ctx, q, profilesOverridesPatchRemoveGame)
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

		var overrideRemoved bool
		err = database.WithTx(ctx, db, func(qtx *dbq.Queries) error {
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

			overrideRemoved = false
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

			return nil
		})
		if err != nil {
			return err
		}

		style.Printf("removed patch entry %q from override for %q in profile %q\n",
			entryKey, relpath, p.Name)
		if overrideRemoved {
			style.Println("  override removed (no remaining patch entries)")
		}

		return nil
	},
}

func init() {
	profilesOverridesPatchCmd.AddCommand(profilesOverridesPatchRemoveCmd)

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
