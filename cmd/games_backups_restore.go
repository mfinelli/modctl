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
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/fsutil"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	gamesBackupsRestoreGame   string
	gamesBackupsRestoreTarget string
	gamesBackupsRestoreForce  bool
)

var gamesBackupsRestoreCmd = &cobra.Command{
	Use:   "restore <path>",
	Short: "Restore a backed-up file to disk immediately",
	Long: `Restore a backed-up file to disk immediately without running unapply.

The path is relative to the target root (default: game_dir). Use --target
to restore a backup from a different install target.

This is useful when you want to revert a single file to its pre-mod state
without unapplying everything. If the active profile is currently applied,
running apply again will overwrite this path. Consider adding a write-once
or skip-backup rule if you want to preserve this behavior permanently.

If the file currently on disk differs from what modctl last installed (drift),
the command warns and requires --force to proceed. The same goes for a file
that can't be read to check it.`,
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

		if gamesBackupsRestoreGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			gamesBackupsRestoreGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, gamesBackupsRestoreGame)
		if err != nil {
			return err
		}

		targetName := gamesBackupsRestoreTarget
		if targetName == "" {
			targetName = "game_dir"
		}

		target, err := q.GetTargetByName(ctx, dbq.GetTargetByNameParams{
			GameInstallID: gi.ID,
			Name:          targetName,
		})
		if err != nil {
			return fmt.Errorf("resolve target %q: %w", targetName, err)
		}

		backup, err := q.GetBackupForGameInstallByPath(ctx, dbq.GetBackupForGameInstallByPathParams{
			GameInstallID: gi.ID,
			TargetID:      target.ID,
			Relpath:       relpath,
		})
		if err != nil {
			return fmt.Errorf("no backup found for %q in target %q", relpath, targetName)
		}

		bs := blobstore.Store{
			ArchivesDir: viper.GetString("archives_dir"),
			BackupsDir:  viper.GetString("backups_dir"),
			TmpDir:      viper.GetString("tmp_dir"),
		}

		blobPath, err := bs.PathFor(blobstore.KindBackup, backup.BackupBlobSha256)
		if err != nil {
			return fmt.Errorf("resolve backup blob path: %w", err)
		}

		absPath := filepath.Join(target.RootPath, relpath)

		// Check for drift: if the file is on disk and tool-owned, verify
		// it matches what modctl installed before restoring over it.
		installedFile, err := q.GetInstalledFileByPath(ctx, dbq.GetInstalledFileByPathParams{
			GameInstallID: gi.ID,
			TargetID:      target.ID,
			Relpath:       relpath,
		})
		if err == nil {
			// File is tool-owned - check for drift
			if _, exists := diskStat(absPath); exists {
				warning, err := checkDrift(ctx, absPath, relpath,
					installedFile.ContentSha256, gamesBackupsRestoreForce)
				if err != nil {
					return err
				}
				if warning != "" {
					style.Println(style.Warning.Render(warning))
				}
			}
		}

		// Warn if profile is currently applied
		appliedState, err := q.GetGameInstallAppliedState(ctx, gi.ID)
		if err == nil && appliedState.AppliedProfileID.Valid {
			style.Println(style.Warning.Render(
				"  warning: the active profile is currently applied; running apply again will overwrite this path\n" +
					"  consider adding a write-once or skip-backup rule if you want to preserve this behavior permanently",
			))
		}

		// Write backup content to disk
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("create parent directories: %w", err)
		}
		if err := copyFileSimple(blobPath, absPath); err != nil {
			return fmt.Errorf("restore backup: %w", err)
		}

		style.Printf("Restored backup for %q\n", relpath)
		return nil
	},
}

func init() {
	gamesBackupsCmd.AddCommand(gamesBackupsRestoreCmd)

	gamesBackupsRestoreCmd.Flags().StringVarP(&gamesBackupsRestoreGame, "game", "g", "",
		"Override the currently active game")
	gamesBackupsRestoreCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	gamesBackupsRestoreCmd.Flags().StringVarP(&gamesBackupsRestoreTarget, "target", "t", "",
		"Install target (default: game_dir)")
	gamesBackupsRestoreCmd.RegisterFlagCompletionFunc("target",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.TargetNames(cmd, toComplete)
		})

	gamesBackupsRestoreCmd.Flags().BoolVar(&gamesBackupsRestoreForce, "force", false,
		"Restore even if the on-disk file has drifted from what modctl installed")
}

// checkDrift compares the file at absPath with the sha256 modctl recorded when
// it installed it (installedSha), before a backup is restored over it. A file
// that differs, or that can't be read to find out, is only restored over with
// force; in that case the warning to show the user is returned. relpath is the
// path relative to the target, for messages.
//
// Being told to stop is always an error, with or without force: we must not
// carry on to overwrite a file we didn't get to check.
func checkDrift(ctx context.Context, absPath, relpath, installedSha string, force bool) (warning string, err error) {
	onDiskHash, err := fsutil.HashFile(ctx, absPath)
	switch {
	case err != nil && ctx.Err() != nil:
		// report that we were told to stop, not whatever the hash ran into
		return "", fmt.Errorf("check %q for drift: %w", relpath, ctx.Err())
	case err != nil:
		if !force {
			return "", fmt.Errorf(
				"could not check %q for drift: %w; pass --force to restore anyway",
				relpath, err,
			)
		}
		return fmt.Sprintf(
			"  warning: could not check %q for drift (%v), restoring backup anyway",
			relpath, err,
		), nil
	case onDiskHash != installedSha:
		if !force {
			return "", fmt.Errorf(
				"file %q has been modified since modctl installed it (drift detected); pass --force to restore anyway",
				relpath,
			)
		}
		return fmt.Sprintf(
			"  warning: %q has been modified since modctl installed it, restoring backup anyway",
			relpath,
		), nil
	}

	return "", nil
}

// copyFileSimple copies src to dst, creating or truncating dst.
// TODO: we have a couple of other similar functions floating around we can
//
//	probably consolidate
func copyFileSimple(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return out.Sync()
}

// TODO copied from the internal/planner package, let's either export it from
//
//	there or copy it somewhere else and export it and use it in both
//	places
func diskStat(path string) (os.FileInfo, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	return info, true
}
