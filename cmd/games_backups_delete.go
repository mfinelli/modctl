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
	"path/filepath"
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	gamesBackupsDeleteGame   string
	gamesBackupsDeleteTarget string
)

var gamesBackupsDeleteCmd = &cobra.Command{
	Use:   "delete <path>",
	Short: "Delete a backup entry",
	Long: `Delete the backup entry for a path.

The path is relative to the target root (default: game_dir). Use --target
to delete a backup from a different install target.

Warning: deleting a backup means modctl cannot restore the original file at
this path on unapply. The file will be deleted instead. The backup blob is
not immediately removed from disk; run 'modctl gc' to reclaim space.`,
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

		if gamesBackupsDeleteGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			gamesBackupsDeleteGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, gamesBackupsDeleteGame)
		if err != nil {
			return err
		}

		targetName := gamesBackupsDeleteTarget
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

		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		fmt.Println(warnStyle.Render(fmt.Sprintf(
			"  warning: deleting this backup means modctl cannot restore the original file at %q on unapply",
			relpath,
		)))

		rows, err := q.DeleteBackupByPath(ctx, dbq.DeleteBackupByPathParams{
			GameInstallID: gi.ID,
			TargetID:      target.ID,
			Relpath:       relpath,
		})
		if err != nil {
			return fmt.Errorf("delete backup: %w", err)
		}
		if rows == 0 {
			return fmt.Errorf("no backup found for %q in target %q", relpath, targetName)
		}

		fmt.Printf("Deleted backup for %q (run 'modctl gc' to reclaim disk space)\n", relpath)
		return nil
	},
}

func init() {
	gamesBackupsCmd.AddCommand(gamesBackupsDeleteCmd)

	gamesBackupsDeleteCmd.Flags().StringVarP(&gamesBackupsDeleteGame, "game", "g", "",
		"Override the currently active game")
	gamesBackupsDeleteCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	gamesBackupsDeleteCmd.Flags().StringVarP(&gamesBackupsDeleteTarget, "target", "t", "",
		"Install target (default: game_dir)")
	gamesBackupsDeleteCmd.RegisterFlagCompletionFunc("target",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.TargetNames(cmd, toComplete)
		})
}
