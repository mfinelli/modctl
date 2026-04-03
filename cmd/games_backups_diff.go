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
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	gamesBackupsDiffGame   string
	gamesBackupsDiffTarget string
	gamesBackupsDiffForce  bool
)

var gamesBackupsDiffCmd = &cobra.Command{
	Use:   "diff <path>",
	Short: "Show a diff between a backup and the current on-disk file",
	Long: `Show a unified diff between the backed-up content and the current on-disk file.

The path is relative to the target root (default: game_dir). Use --target
to diff a backup from a different install target.

The diff shows what has changed since the backup was taken:
  - lines removed from the backup are shown in red
  - lines added to the on-disk file are shown in green

If the on-disk file is missing, the backup content is shown in full as a
deletion. Binary files are detected automatically and refused unless --force
is passed.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		boldStyle := lipgloss.NewStyle().Bold(true)
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		removeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		hunkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

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

		if gamesBackupsDiffGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			gamesBackupsDiffGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, gamesBackupsDiffGame)
		if err != nil {
			return err
		}

		targetName := gamesBackupsDiffTarget
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

		backupData, err := os.ReadFile(blobPath)
		if err != nil {
			return fmt.Errorf("read backup blob: %w", err)
		}

		if internal.IsBinaryContent(backupData) && !gamesBackupsDiffForce {
			return fmt.Errorf(
				"backup for %q appears to be a binary file; pass --force to diff anyway",
				relpath,
			)
		}

		absPath := filepath.Join(target.RootPath, relpath)
		var onDiskData []byte
		var missingFromDisk bool

		if _, exists := diskStat(absPath); exists {
			onDiskData, err = os.ReadFile(absPath)
			if err != nil {
				return fmt.Errorf("read on-disk file: %w", err)
			}
			if internal.IsBinaryContent(onDiskData) && !gamesBackupsDiffForce {
				return fmt.Errorf(
					"on-disk file %q appears to be binary; pass --force to diff anyway",
					relpath,
				)
			}
		} else {
			missingFromDisk = true
			fmt.Println(warnStyle.Render(fmt.Sprintf(
				"  warning: %q is not present on disk; showing backup content as full deletion",
				relpath,
			)))
			fmt.Println()
		}

		fmt.Println(boldStyle.Render(fmt.Sprintf("Backup diff for %q:", relpath)))
		fmt.Println()

		diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(string(backupData)),
			B:        difflib.SplitLines(string(onDiskData)),
			FromFile: relpath + " (backup)",
			ToFile:   relpath + " (on disk)",
			Context:  3,
		})
		if err != nil {
			return fmt.Errorf("generate diff: %w", err)
		}

		if diff == "" && !missingFromDisk {
			fmt.Println(subtleStyle.Render("  no differences (on-disk file matches backup)"))
			return nil
		}

		for _, line := range strings.Split(diff, "\n") {
			switch {
			case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
				fmt.Println(boldStyle.Render(line))
			case strings.HasPrefix(line, "@@"):
				fmt.Println(hunkStyle.Render(line))
			case strings.HasPrefix(line, "+"):
				fmt.Println(addStyle.Render(line))
			case strings.HasPrefix(line, "-"):
				fmt.Println(removeStyle.Render(line))
			default:
				fmt.Println(subtleStyle.Render(line))
			}
		}

		return nil
	},
}

func init() {
	gamesBackupsCmd.AddCommand(gamesBackupsDiffCmd)

	gamesBackupsDiffCmd.Flags().StringVarP(&gamesBackupsDiffGame, "game", "g", "",
		"Override the currently active game")
	gamesBackupsDiffCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	gamesBackupsDiffCmd.Flags().StringVarP(&gamesBackupsDiffTarget, "target", "t", "",
		"Install target (default: game_dir)")
	gamesBackupsDiffCmd.RegisterFlagCompletionFunc("target",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.TargetNames(cmd, toComplete)
		})

	gamesBackupsDiffCmd.Flags().BoolVar(&gamesBackupsDiffForce, "force", false,
		"Diff binary files without refusing")
}
