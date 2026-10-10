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
	"strconv"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
)

var (
	gamesBackupsListGame   string
	gamesBackupsListTarget string
)

var gamesBackupsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List backed-up files for the current game",
	Long: `List all files that modctl has backed up for the current game.

Backups are created automatically before overwriting a file that modctl did
not install. They are restored automatically on unapply.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

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

		if gamesBackupsListGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			gamesBackupsListGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, gamesBackupsListGame)
		if err != nil {
			return err
		}

		backups, err := q.ListBackupsForGameInstall(ctx, dbq.ListBackupsForGameInstallParams{
			GameInstallID: gi.ID,
			TargetName:    gamesBackupsListTarget,
		})
		if err != nil {
			return fmt.Errorf("list backups: %w", err)
		}

		if len(backups) == 0 {
			style.Println(style.Subtle.Render("  no backups found"))
			return nil
		}

		rows := [][]string{}
		for _, b := range backups {
			opInfo := ""
			if b.OperationID.Valid {
				opInfo = fmt.Sprintf("%s #%d", b.OperationType.String, b.OperationID.Int64)
			}
			rows = append(rows, []string{
				fmt.Sprintf(" %s ", b.TargetName),
				fmt.Sprintf(" %s ", b.Relpath),
				fmt.Sprintf(" %s ", style.Bytes(b.SizeBytes)),
				fmt.Sprintf(" %s ", b.CreatedAt),
				fmt.Sprintf(" %s ", opInfo),
			})
		}

		style.Println(style.Table([]string{" Target ", " Path ", " Size ", " Backed Up At ", " Operation "}, rows))
		return nil
	},
}

func init() {
	gamesBackupsCmd.AddCommand(gamesBackupsListCmd)

	gamesBackupsListCmd.Flags().StringVarP(&gamesBackupsListGame, "game", "g", "",
		"Override the currently active game")
	gamesBackupsListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	gamesBackupsListCmd.Flags().StringVarP(&gamesBackupsListTarget, "target", "t", "",
		"Filter by install target (default: all targets)")
	gamesBackupsListCmd.RegisterFlagCompletionFunc("target",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.TargetNames(cmd, toComplete)
		})
}
