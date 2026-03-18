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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/extractor"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	profilesOverridesEditGame    string
	profilesOverridesEditProfile string
	profilesOverridesEditReset   bool
)

var profilesOverridesEditCmd = &cobra.Command{
	Use:   "edit <path>",
	Short: "Edit a file override in the active profile",
	Long: `Edit a file override for a path in the active profile.

If no override exists, the base file is extracted from the current winning
mod's archive and opened in your editor. On save, the result is stored as
a new override with the source anchor captured automatically.

If an override already exists, the current override content is opened in
your editor. On save, the override blob is updated. The source anchor from
the original creation is preserved.

Pass --reset to discard the existing override and start fresh from the
current base file, re-capturing the source anchor.

If the file is unchanged after editing, no changes are made.

Note: this command requires archive extraction which may be slow for large
archives.`,
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

		if profilesOverridesEditGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesEditGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesEditGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesEditProfile)
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

		bs := blobstore.Store{
			ArchivesDir:  viper.GetString("archives_dir"),
			BackupsDir:   viper.GetString("backups_dir"),
			OverridesDir: viper.GetString("overrides_dir"),
			TmpDir:       viper.GetString("tmp_dir"),
		}

		ex := extractor.Extractor{
			BsdtarPath: viper.GetString("bsdtar"),
			BlobStore:  bs,
			StagingDir: viper.GetString("tmp_dir"),
		}

		// check for existing override
		existing, existingErr := q.GetOverride(ctx, dbq.GetOverrideParams{
			ProfileID: p.ID,
			TargetID:  target.ID,
			Relpath:   relpath,
		})

		// determine the content to open in the editor and the source anchor
		var editContent []byte
		var srcArchiveSha256 sql.NullString
		var srcRawPath sql.NullString
		var srcContentSha256 sql.NullString
		var isNew bool

		useBase := existingErr != nil || profilesOverridesEditReset

		if useBase {
			// no existing override or --reset: extract base file from winning mod
			winner, err := q.GetCurrentWinnerForPath(ctx, dbq.GetCurrentWinnerForPathParams{
				ProfileID: p.ID,
				RawPath:   sql.NullString{String: relpath, Valid: true},
			})
			if err != nil {
				if existingErr != nil {
					// no override and no base mod: can't edit
					return fmt.Errorf(
						"no override exists for %q and no mod in this profile provides it\n"+
							"use 'profiles overrides set %s <file>' to create one from scratch",
						relpath, relpath,
					)
				}
				// --reset but no base mod - also can't reset
				return fmt.Errorf(
					"no mod in this profile provides %q; cannot reset to base file",
					relpath,
				)
			}

			// extract the archive and read the specific file
			stagingDir, err := ex.ExtractArchive(ctx, winner.ArchiveSha256)
			if err != nil {
				return fmt.Errorf("extract archive: %w", err)
			}
			defer os.RemoveAll(stagingDir)

			// raw_path from inventory is the path inside the archive
			if !winner.RawPath.Valid {
				return fmt.Errorf("inventory entry for %q has no path", relpath)
			}
			stagedFilePath := filepath.Join(stagingDir, winner.RawPath.String)
			editContent, err = os.ReadFile(stagedFilePath)
			if err != nil {
				return fmt.Errorf("read base file from staging: %w", err)
			}

			// capture source anchor
			srcArchiveSha256 = sql.NullString{String: winner.ArchiveSha256, Valid: true}
			srcRawPath = winner.RawPath
			srcContentSha256 = winner.ContentSha256
			isNew = true

		} else {
			// existing override and no --reset: read current override blob
			if !existing.BlobSha256.Valid {
				return fmt.Errorf("existing override for %q is a patch override; use 'profiles overrides patch' commands", relpath)
			}
			blobPath, err := bs.PathFor(blobstore.KindOverride, existing.BlobSha256.String)
			if err != nil {
				return fmt.Errorf("resolve override blob path: %w", err)
			}
			editContent, err = os.ReadFile(blobPath)
			if err != nil {
				return fmt.Errorf("read override blob: %w", err)
			}

			// preserve existing source anchor
			srcArchiveSha256 = existing.SourceArchiveSha256
			srcRawPath = existing.SourceRawPath
			srcContentSha256 = existing.SourceContentSha256
			isNew = false
		}

		// write content to a temp file for the editor
		tmpFile, err := os.CreateTemp("", "modctl-override-*")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		if _, err := tmpFile.Write(editContent); err != nil {
			tmpFile.Close()
			return fmt.Errorf("write temp file: %w", err)
		}
		if err := tmpFile.Close(); err != nil {
			return fmt.Errorf("close temp file: %w", err)
		}

		// launch editor
		if err := launchEditor(tmpPath); err != nil {
			return fmt.Errorf("editor: %w", err)
		}

		// read result
		result, err := os.ReadFile(tmpPath)
		if err != nil {
			return fmt.Errorf("read edited file: %w", err)
		}

		// no-op if unchanged
		if string(result) == string(editContent) {
			fmt.Println("no changes made")
			return nil
		}

		// write result to a second temp file for ingestion
		// (IngestFile takes a path, not bytes)
		ingestTmp, err := os.CreateTemp("", "modctl-override-ingest-*")
		if err != nil {
			return fmt.Errorf("create ingest temp file: %w", err)
		}
		ingestTmpPath := ingestTmp.Name()
		defer os.Remove(ingestTmpPath)

		if _, err := ingestTmp.Write(result); err != nil {
			ingestTmp.Close()
			return fmt.Errorf("write ingest temp file: %w", err)
		}
		if err := ingestTmp.Close(); err != nil {
			return fmt.Errorf("close ingest temp file: %w", err)
		}

		// ingest into override blob store
		ingestResult, err := bs.IngestFile(ctx, blobstore.KindOverride, ingestTmpPath)
		if err != nil {
			return fmt.Errorf("ingest override blob: %w", err)
		}

		originalName := filepath.Base(relpath)
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

		if _, err = qtx.UpsertOverride(ctx, dbq.UpsertOverrideParams{
			ProfileID:           p.ID,
			TargetID:            target.ID,
			Relpath:             relpath,
			BlobSha256:          sql.NullString{String: ingestResult.SHA256Hex, Valid: true},
			OverrideType:        "full_file",
			SourceArchiveSha256: srcArchiveSha256,
			SourceRawPath:       srcRawPath,
			SourceContentSha256: srcContentSha256,
			Notes:               sql.NullString{},
		}); err != nil {
			return fmt.Errorf("upsert override: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		if isNew {
			fmt.Printf("override created for %q in profile %q\n", relpath, p.Name)
		} else {
			fmt.Printf("override updated for %q in profile %q\n", relpath, p.Name)
		}

		return nil
	},
}

func init() {
	profilesOverridesCmd.AddCommand(profilesOverridesEditCmd)

	profilesOverridesEditCmd.Flags().StringVarP(&profilesOverridesEditGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesEditCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesOverridesEditCmd.Flags().StringVarP(&profilesOverridesEditProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesEditCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	profilesOverridesEditCmd.Flags().BoolVar(&profilesOverridesEditReset, "reset", false,
		"Discard existing override and start fresh from the current base file")
}

// launchEditor opens the file at path in the user's preferred editor.
// Respects $VISUAL then $EDITOR, falling back to vi.
func launchEditor(path string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
