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
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/spf13/cobra"
)

var modsNotesSetGame string

var modsNotesSetCmd = &cobra.Command{
	Use:   "set <mod> <text|->",
	Short: "Set the notes on a mod page",
	Long: `Set freeform notes on a mod page, replacing any existing notes. The text is
stored exactly as given and is shown by 'mods info'.

Pass - as the text to read it from stdin:

  modctl mods notes set "Appearance Menu Mod" "https://example.com/mods/ami"
  cat notes.txt | modctl mods notes set "Appearance Menu Mod" -`,
	Args: cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completer().ModPageIDs(cmd, toComplete)
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		db, err := openDB(ctx)
		if err != nil {
			return err
		}
		defer db.Close()

		q := dbq.New(db)

		gi, err := gameInstall(ctx, q, modsNotesSetGame)
		if err != nil {
			return err
		}

		mp, err := argresolver.ModPage(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		text, err := internal.NotesTextFromArg(args[1], os.Stdin)
		if err != nil {
			return err
		}

		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("notes text cannot be empty; use 'mods notes clear' to remove notes")
		}

		return q.SetModPageNotes(ctx, dbq.SetModPageNotesParams{
			Notes:         sql.NullString{String: text, Valid: true},
			ID:            mp.ID,
			GameInstallID: gi.ID,
		})
	},
}

func init() {
	modsNotesCmd.AddCommand(modsNotesSetCmd)

	modsNotesSetCmd.Flags().StringVarP(&modsNotesSetGame, "game", "g", "",
		"Override the currently active game")
	modsNotesSetCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().GameInstallSelectors(cmd, toComplete)
		})
}
