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
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/charmbracelet/lipgloss"
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
the command warns and requires --force to proceed.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

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
				onDiskHash, hashErr := hashFile(absPath)
				if hashErr == nil && onDiskHash != installedFile.ContentSha256 {
					if !gamesBackupsRestoreForce {
						return fmt.Errorf(
							"file %q has been modified since modctl installed it (drift detected); pass --force to restore anyway",
							relpath,
						)
					}
					fmt.Println(warnStyle.Render(fmt.Sprintf(
						"  warning: %q has been modified since modctl installed it, restoring backup anyway",
						relpath,
					)))
				}
			}
		}

		// Warn if profile is currently applied
		appliedState, err := q.GetGameInstallAppliedState(ctx, gi.ID)
		if err == nil && appliedState.AppliedProfileID.Valid {
			fmt.Println(warnStyle.Render(
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

		fmt.Printf("Restored backup for %q\n", relpath)
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
