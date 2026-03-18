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
	profilesOverridesPatchUnsetGame    string
	profilesOverridesPatchUnsetProfile string
	profilesOverridesPatchUnsetSection string
	profilesOverridesPatchUnsetType    string
	profilesOverridesPatchUnsetClear   bool
)

var profilesOverridesPatchUnsetCmd = &cobra.Command{
	Use:   "unset <path> <key>",
	Short: "Mark a key for removal when the patch override is applied",
	Long: `Mark a key for removal in a patch override in the active profile.

When the override is applied, the specified key will be removed from the
file. If no patch override exists for this path, one is created
automatically.

If a set entry already exists for this key it is converted to an unset
entry. To remove the patch entry entirely (stop patching this key),
use 'profiles overrides patch remove'.

For ini patches, use --section to target a key in a specific section.

For xml patches, use --clear to empty the node's content rather than
removing the node entirely.`,
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

		if profilesOverridesPatchUnsetGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesPatchUnsetGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesPatchUnsetGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesPatchUnsetProfile)
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

		// check for existing override, auto-create if needed
		existing, existingErr := q.GetOverride(ctx, dbq.GetOverrideParams{
			ProfileID: p.ID,
			TargetID:  target.ID,
			Relpath:   relpath,
		})

		var overrideID int64
		var overrideType string

		if existingErr != nil {
			// no override exists - infer type from extension and create
			var err error
			overrideType, err = resolvePatchType(relpath, profilesOverridesPatchUnsetType)
			if err != nil {
				return err
			}

			var srcArchiveSha256 sql.NullString
			var srcRawPath sql.NullString
			var srcContentSha256 sql.NullString

			anchor, anchorErr := q.GetCurrentWinnerForPath(ctx, dbq.GetCurrentWinnerForPathParams{
				ProfileID: p.ID,
				RawPath:   sql.NullString{String: relpath, Valid: true},
			})
			if anchorErr == nil {
				srcArchiveSha256 = sql.NullString{String: anchor.ArchiveSha256, Valid: true}
				srcRawPath = anchor.RawPath
				srcContentSha256 = anchor.ContentSha256
			}

			newOverride, err := q.UpsertOverride(ctx, dbq.UpsertOverrideParams{
				ProfileID:           p.ID,
				TargetID:            target.ID,
				Relpath:             relpath,
				BlobSha256:          sql.NullString{},
				OverrideType:        overrideType,
				SourceArchiveSha256: srcArchiveSha256,
				SourceRawPath:       srcRawPath,
				SourceContentSha256: srcContentSha256,
				Notes:               sql.NullString{},
			})
			if err != nil {
				return fmt.Errorf("create patch override: %w", err)
			}
			overrideID = newOverride.ID
		} else {
			if existing.OverrideType == "full_file" {
				return fmt.Errorf(
					"override for %q is a full-file override; use 'profiles overrides unset' to remove it",
					relpath,
				)
			}
			overrideID = existing.ID
			overrideType = existing.OverrideType
		}

		if profilesOverridesPatchUnsetType != "" {
			expectedType, err := resolvePatchType(relpath, profilesOverridesPatchUnsetType)
			if err != nil {
				return err
			}
			if expectedType != existing.OverrideType {
				return fmt.Errorf(
					"override for %q is type %s, not %s; remove --type or use the correct type",
					relpath, formatOverrideType(existing.OverrideType),
					formatOverrideType(expectedType),
				)
			}
		}

		var entrySection sql.NullString
		if profilesOverridesPatchUnsetSection != "" {
			if overrideType != "ini_patch" {
				return fmt.Errorf("--section is only valid for ini patch overrides")
			}
			entrySection = sql.NullString{String: profilesOverridesPatchUnsetSection, Valid: true}
		}

		// determine unset patch type: "ini_patch" -> "ini_unset" etc.
		var unsetPatchType string
		if profilesOverridesPatchUnsetClear {
			if overrideType != "xml_patch" {
				return fmt.Errorf("--clear is only valid for xml patch overrides")
			}
			unsetPatchType = "xml_clear"
		} else {
			// "ini_patch" -> "ini_unset", "xml_patch" -> "xml_unset", etc.
			unsetPatchType = overrideType[:len(overrideType)-len("_patch")] + "_unset"
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		existingEntry, entryErr := qtx.GetOverridePatchEntryByKey(ctx, dbq.GetOverridePatchEntryByKeyParams{
			OverrideID:   overrideID,
			EntryKey:     entryKey,
			EntrySection: entrySection,
		})

		if entryErr != nil {
			// no existing entry: append new unset entry
			maxPos, err := qtx.GetMaxOverridePatchPosition(ctx, overrideID)
			if err != nil {
				return fmt.Errorf("get max position: %w", err)
			}
			if _, err := qtx.InsertOverridePatchEntry(ctx, dbq.InsertOverridePatchEntryParams{
				OverrideID:   overrideID,
				Position:     maxPos + 1,
				PatchType:    unsetPatchType,
				EntrySection: entrySection,
				EntryKey:     entryKey,
				EntryValue:   sql.NullString{},
			}); err != nil {
				return fmt.Errorf("insert unset patch entry: %w", err)
			}
			fmt.Printf("marked key %q for removal in override for %q in profile %q\n",
				entryKey, relpath, p.Name)
		} else {
			// existing entry: update type to unset and clear value
			if err := qtx.UpdateOverridePatchEntryTypeAndValue(ctx, dbq.UpdateOverridePatchEntryTypeAndValueParams{
				PatchType:  unsetPatchType,
				EntryValue: sql.NullString{},
				ID:         existingEntry.ID,
			}); err != nil {
				return fmt.Errorf("update patch entry: %w", err)
			}
			fmt.Printf("converted patch entry %q to unset in override for %q in profile %q\n",
				entryKey, relpath, p.Name)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		return nil
	},
}

func init() {
	profilesOverridesPatchCmd.AddCommand(profilesOverridesPatchUnsetCmd)

	profilesOverridesPatchUnsetCmd.Flags().StringVarP(&profilesOverridesPatchUnsetGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesPatchUnsetCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesOverridesPatchUnsetCmd.Flags().StringVarP(&profilesOverridesPatchUnsetProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesPatchUnsetCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	profilesOverridesPatchUnsetCmd.Flags().BoolVar(&profilesOverridesPatchUnsetClear, "clear", false,
		"Clear node content instead of removing it (xml patches only)")
	profilesOverridesPatchUnsetCmd.Flags().StringVar(&profilesOverridesPatchUnsetSection, "section", "",
		"Section name (ini patches only)")
	profilesOverridesPatchUnsetCmd.Flags().StringVar(&profilesOverridesPatchUnsetType, "type", "",
		"Patch type: ini, json, xml, or yaml (inferred from file extension if not specified)")
}
