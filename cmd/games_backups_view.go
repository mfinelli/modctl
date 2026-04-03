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
	gamesBackupsViewGame   string
	gamesBackupsViewTarget string
	gamesBackupsViewForce  bool
)

var gamesBackupsViewCmd = &cobra.Command{
	Use:   "view <path>",
	Short: "Print the content of a backed-up file",
	Long: `Print the content of a backed-up file to the terminal.

The path is relative to the target root (default: game_dir). Use --target
to view a backup from a different install target.

Binary files are detected automatically and refused unless --force is passed.`,
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

		if gamesBackupsViewGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			gamesBackupsViewGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, gamesBackupsViewGame)
		if err != nil {
			return err
		}

		targetName := gamesBackupsViewTarget
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

		data, err := os.ReadFile(blobPath)
		if err != nil {
			return fmt.Errorf("read backup blob: %w", err)
		}

		if internal.IsBinaryContent(data) && !gamesBackupsViewForce {
			return fmt.Errorf(
				"backup for %q appears to be a binary file; pass --force to print anyway",
				relpath,
			)
		}

		fmt.Print(string(data))
		return nil
	},
}

func init() {
	gamesBackupsCmd.AddCommand(gamesBackupsViewCmd)

	gamesBackupsViewCmd.Flags().StringVarP(&gamesBackupsViewGame, "game", "g", "",
		"Override the currently active game")
	gamesBackupsViewCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	gamesBackupsViewCmd.Flags().StringVarP(&gamesBackupsViewTarget, "target", "t", "",
		"Install target (default: game_dir)")
	gamesBackupsViewCmd.RegisterFlagCompletionFunc("target",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.TargetNames(cmd, toComplete)
		})
	gamesBackupsViewCmd.Flags().BoolVar(&gamesBackupsViewForce, "force", false,
		"Print binary files without refusing")
}
