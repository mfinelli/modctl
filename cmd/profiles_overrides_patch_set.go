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
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesOverridesPatchSetGame    string
	profilesOverridesPatchSetProfile string
	profilesOverridesPatchSetSection string
	profilesOverridesPatchSetType    string
)

var profilesOverridesPatchSetCmd = &cobra.Command{
	Use:   "set <path> <key> <value>",
	Short: "Set a patch entry for a file override",
	Long: `Set a patch entry for a file override in the active profile.

If no patch override exists for this path, one is created automatically.
The patch type is inferred from the file extension (.ini, .yaml/.yml, .json)
or can be specified explicitly with --type on first use.

If a patch entry already exists for the same key (and section for ini
patches), its value is updated in place. Otherwise a new entry is appended.

Examples:
  modctl profiles overrides patch set settings.ini MaxFramerate 60 --section Display
  modctl profiles overrides patch set config.yaml graphics.quality ultra
  modctl profiles overrides patch set config.json renderDistance 128`,
	Args:         cobra.ExactArgs(3),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		relpath := filepath.Clean(args[0])
		entryKey := args[1]
		entryValue := args[2]

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

		if profilesOverridesPatchSetGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesPatchSetGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesPatchSetGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesPatchSetProfile)
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

		// check for existing override
		existing, existingErr := q.GetOverride(ctx, dbq.GetOverrideParams{
			ProfileID: p.ID,
			TargetID:  target.ID,
			Relpath:   relpath,
		})

		var overrideID int64
		var overrideType string

		if existingErr != nil {
			// no override exists — create one
			// determine patch type from --type flag or file extension
			overrideType, err = resolvePatchType(relpath, profilesOverridesPatchSetType)
			if err != nil {
				return err
			}

			// capture source anchor
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
			// override exists — validate type
			if existing.OverrideType == "full_file" {
				return fmt.Errorf(
					"override for %q is a full-file override — use 'profiles overrides edit' or 'profiles overrides set'",
					relpath,
				)
			}
			if profilesOverridesPatchSetType != "" {
				expectedType, err := resolvePatchType(relpath, profilesOverridesPatchSetType)
				if err != nil {
					return err
				}
				if expectedType != existing.OverrideType {
					return fmt.Errorf(
						"override for %q is type %s, not %s — remove --type or use the correct type",
						relpath, formatOverrideType(existing.OverrideType),
						formatOverrideType(expectedType),
					)
				}
			}
			overrideID = existing.ID
			overrideType = existing.OverrideType
		}

		// resolve section
		var entrySection sql.NullString
		if profilesOverridesPatchSetSection != "" {
			if !strings.HasSuffix(overrideType, "_patch") || overrideType == "yaml_patch" || overrideType == "json_patch" {
				return fmt.Errorf("--section is only valid for ini patch overrides")
			}
			entrySection = sql.NullString{String: profilesOverridesPatchSetSection, Valid: true}
		}

		// look up existing entry by key+section for upsert
		existingEntry, entryErr := q.GetOverridePatchEntryByKey(ctx, dbq.GetOverridePatchEntryByKeyParams{
			OverrideID:   overrideID,
			EntryKey:     entryKey,
			EntrySection: entrySection,
		})

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		// determine patch type string for the entry
		setPatchType := overrideType[:len(overrideType)-len("_patch")] + "_set"
		// e.g. "ini_patch" -> "ini_set", "yaml_patch" -> "yaml_set"

		if entryErr != nil {
			// new entry: append at end
			maxPos, err := qtx.GetMaxOverridePatchPosition(ctx, overrideID)
			if err != nil {
				return fmt.Errorf("get max position: %w", err)
			}
			newPos := maxPos + 1

			if _, err := qtx.InsertOverridePatchEntry(ctx, dbq.InsertOverridePatchEntryParams{
				OverrideID:   overrideID,
				Position:     newPos,
				PatchType:    setPatchType,
				EntrySection: entrySection,
				EntryKey:     entryKey,
				EntryValue:   sql.NullString{String: entryValue, Valid: true},
			}); err != nil {
				return fmt.Errorf("insert patch entry: %w", err)
			}
			fmt.Printf("added patch entry %q to override for %q in profile %q\n",
				entryKey, relpath, p.Name)
		} else {
			// existing entry — update value in place
			if err := qtx.UpdateOverridePatchEntryValue(ctx, dbq.UpdateOverridePatchEntryValueParams{
				EntryValue: sql.NullString{String: entryValue, Valid: true},
				ID:         existingEntry.ID,
			}); err != nil {
				return fmt.Errorf("update patch entry: %w", err)
			}
			fmt.Printf("updated patch entry %q in override for %q in profile %q\n",
				entryKey, relpath, p.Name)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		return nil

	},
}

func init() {
	profilesOverridesPatchCmd.AddCommand(profilesOverridesPatchSetCmd)

	profilesOverridesPatchSetCmd.Flags().StringVarP(&profilesOverridesPatchSetGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesPatchSetCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesOverridesPatchSetCmd.Flags().StringVarP(&profilesOverridesPatchSetProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesPatchSetCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	profilesOverridesPatchSetCmd.Flags().StringVar(&profilesOverridesPatchSetSection, "section", "",
		"Section name (ini patches only)")
	profilesOverridesPatchSetCmd.Flags().StringVar(&profilesOverridesPatchSetType, "type", "",
		"Patch type: ini, yaml, or json (inferred from file extension if not specified)")
}

// resolvePatchType determines the override_type from the --type flag or
// file extension. Returns an error if neither can determine the type.
func resolvePatchType(relpath, flagType string) (string, error) {
	if flagType != "" {
		switch strings.ToLower(flagType) {
		case "ini":
			return "ini_patch", nil
		case "yaml":
			return "yaml_patch", nil
		case "json":
			return "json_patch", nil
		default:
			return "", fmt.Errorf("unknown patch type %q: valid types are ini, yaml, json", flagType)
		}
	}

	ext := strings.ToLower(filepath.Ext(relpath))
	switch ext {
	case ".ini":
		return "ini_patch", nil
	case ".yaml", ".yml":
		return "yaml_patch", nil
	case ".json":
		return "json_patch", nil
	default:
		return "", fmt.Errorf(
			"cannot infer patch type from extension %q; use --type ini, --type yaml, or --type json",
			ext,
		)
	}
}
