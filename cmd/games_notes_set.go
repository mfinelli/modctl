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
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var gamesNotesSetGame string

var gamesNotesSetCmd = &cobra.Command{
	Use:   "set <text|-]>",
	Short: "Set notes for the active game install",
	Long: `Set freeform notes on the active game install.

Pass - as the argument to read note text from stdin:

  modctl games notes set "remember to run protontricks"
  echo "remember to run protontricks" | modctl games notes set -`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		err := internal.EnsureDBExists()
		if err != nil {
			return err
		}
		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()
		err = internal.MigrateDB(ctx, db)
		if err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		q := dbq.New(db)

		if gamesNotesSetGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			gamesNotesSetGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, gamesNotesSetGame)
		if err != nil {
			return err
		}

		var text string
		if args[0] == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read from stdin: %w", err)
			}
			text = strings.TrimRight(string(data), "\n")
		} else {
			text = args[0]
		}

		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("notes text cannot be empty; use 'games notes clear' to remove notes")
		}

		return q.SetGameInstallNotes(ctx, dbq.SetGameInstallNotesParams{
			Notes: sql.NullString{String: text, Valid: true},
			ID:    gi.ID,
		})
	},
}

func init() {
	gamesNotesCmd.AddCommand(gamesNotesSetCmd)

	gamesNotesSetCmd.Flags().StringVarP(&gamesNotesSetGame, "game", "g", "",
		"Override the currently active game")
	gamesNotesSetCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
