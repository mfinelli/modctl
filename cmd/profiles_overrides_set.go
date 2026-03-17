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
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	profilesOverridesSetGame    string
	profilesOverridesSetProfile string
	profilesOverridesSetNotes   string
	profilesOverridesSetForce   bool
)

var profilesOverridesSetCmd = &cobra.Command{
	Use:   "set <path> <file>",
	Short: "Set a full-file override for a path in a profile",
	Long: `Set a full-file override for a path in the active profile.

The file is imported from disk and stored in the override blob store.
The path should be relative to the game directory.

If an override already exists for this path it will be replaced.
The source anchor is captured automatically from the current conflict
winner for this path in the active profile.`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		relpath := filepath.Clean(args[0])
		srcFile := args[1]

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

		if profilesOverridesSetGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesSetGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesSetGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesSetProfile)
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

		// check for existing override unless --force
		if !profilesOverridesSetForce {
			_, err := q.GetOverride(ctx, dbq.GetOverrideParams{
				ProfileID: p.ID,
				TargetID:  target.ID,
				Relpath:   relpath,
			})
			if err == nil {
				return fmt.Errorf(
					"override already exists for %q in profile %q — pass --force to replace it",
					relpath, p.Name,
				)
			}
		}

		bs := blobstore.Store{
			ArchivesDir:  viper.GetString("archives_dir"),
			BackupsDir:   viper.GetString("backups_dir"),
			OverridesDir: viper.GetString("overrides_dir"),
			TmpDir:       viper.GetString("tmp_dir"),
		}

		// ingest the file into the override blob store
		ingestResult, err := bs.IngestFile(ctx, blobstore.KindOverride, srcFile)
		if err != nil {
			return fmt.Errorf("ingest override blob: %w", err)
		}

		originalName := filepath.Base(srcFile)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		if err := blobstore.EnsureBlobRecorded(
			ctx,
			qtx,
			ingestResult.SHA256Hex,
			string(blobstore.KindOverride),
			ingestResult.SizeBytes,
			&originalName,
		); err != nil {
			return fmt.Errorf("record override blob: %w", err)
		}

		// capture source anchor from current conflict winner for this path
		var srcArchiveSha256 sql.NullString
		var srcRawPath sql.NullString
		var srcContentSha256 sql.NullString

		anchor, err := qtx.GetCurrentWinnerForPath(ctx, dbq.GetCurrentWinnerForPathParams{
			ProfileID: p.ID,
			RawPath:   sql.NullString{String: relpath, Valid: true},
		})
		if err == nil {
			srcArchiveSha256 = sql.NullString{String: anchor.ArchiveSha256, Valid: true}
			srcRawPath = anchor.RawPath
			srcContentSha256 = anchor.ContentSha256
		}
		// if no winner found anchor stays NULL (net-new override)

		notes := sql.NullString{}
		if profilesOverridesSetNotes != "" {
			notes = sql.NullString{String: profilesOverridesSetNotes, Valid: true}
		}

		if _, err = qtx.UpsertOverride(ctx, dbq.UpsertOverrideParams{
			ProfileID:           p.ID,
			TargetID:            target.ID,
			Relpath:             relpath,
			BlobSha256:          sql.NullString{String: ingestResult.SHA256Hex, Valid: true},
			OverrideType:        "full_file",
			SourceArchiveSha256: srcArchiveSha256,
			SourceRawPath:       srcRawPath,
			SourceContentSha256: srcContentSha256,
			Notes:               notes,
		}); err != nil {
			return fmt.Errorf("upsert override: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		fmt.Printf("override set for %q in profile %q\n", relpath, p.Name)
		if !srcArchiveSha256.Valid {
			fmt.Println("  note: no mod in this profile provides this path — override will write a net-new file")
		}

		return nil
	},
}

func init() {
	profilesOverridesCmd.AddCommand(profilesOverridesSetCmd)

	profilesOverridesSetCmd.Flags().StringVarP(&profilesOverridesSetGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesSetCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesOverridesSetCmd.Flags().StringVarP(&profilesOverridesSetProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesSetCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
	profilesOverridesSetCmd.Flags().StringVar(&profilesOverridesSetNotes, "notes", "",
		"Optional notes for this override")
	profilesOverridesSetCmd.Flags().BoolVar(&profilesOverridesSetForce, "force", false,
		"Replace existing override if one already exists for this path")
}
