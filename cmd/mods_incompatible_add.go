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
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-sqlite3"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	modsIncompatibleAddGame   string
	modsIncompatibleAddReason string
)

var modsIncompatibleAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Flag two mods as incompatible with each other",
	Long: `Flag two mods as incompatible with each other.

Incompatibility flags are surfaced as warnings in 'profiles status' when
both mods are enabled in the same profile. They do not prevent applying a
profile - they are informational only.

The reason is freeform and entirely up to you - it might be a note about
known crashes, conflicting game mechanics, or anything else.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract this
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		idA, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid mod-page-id-a %q: must be a numeric ID", args[0])
		}
		idB, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid mod-page-id-b %q: must be a numeric ID", args[1])
		}
		if idA == idB {
			return fmt.Errorf("mod-page-id-a and mod-page-id-b must be different")
		}

		err = internal.EnsureDBExists()
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

		// Resolve game install id: --game overrides active selection
		if modsIncompatibleAddGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			modsIncompatibleAddGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, modsIncompatibleAddGame)
		if err != nil {
			return err
		}

		// Verify both mod pages exist and belong to the current game install
		pageA, err := q.GetModPage(ctx, idA)
		if err != nil {
			return fmt.Errorf("mod page %d not found", idA)
		}
		pageB, err := q.GetModPage(ctx, idB)
		if err != nil {
			return fmt.Errorf("mod page %d not found", idB)
		}
		if pageA.GameInstallID != gi.ID || pageB.GameInstallID != gi.ID {
			return fmt.Errorf("both mod pages must belong to the current game install")
		}

		nullReason := sql.NullString{}
		if modsIncompatibleAddReason != "" {
			nullReason = sql.NullString{String: modsIncompatibleAddReason, Valid: true}
		}

		if err := q.AddModIncompatibility(ctx, dbq.AddModIncompatibilityParams{
			ModPageIDA: idA,
			ModPageIDB: idB,
			Reason:     nullReason,
		}); err != nil {
			var se sqlite3.Error
			if errors.As(err, &se) {
				if se.Code == sqlite3.ErrConstraint && se.ExtendedCode == sqlite3.ErrConstraintUnique {
					return fmt.Errorf("mods %q (id: %d) and %q (id: %d) are already flagged as incompatible",
						pageA.Name, pageA.ID, pageB.Name, pageB.ID)
				}
				if se.Code == sqlite3.ErrConstraint && se.ExtendedCode == sqlite3.ErrConstraintTrigger {
					// Fired by trg_mod_incompatibilities_same_game_ins — shouldn't be
					// reachable in normal use since we verify game_install_id above,
					// but handle it gracefully in case of a race or direct DB access.
					return fmt.Errorf("mod pages %d and %d do not belong to the same game install",
						idA, idB)
				}
			}
			return fmt.Errorf("flagging incompatibility: %w", err)
		}

		fmt.Printf("Flagged as incompatible:\n")
		fmt.Printf("  %s (id: %d)\n", pageA.Name, pageA.ID)
		fmt.Printf("  %s (id: %d)\n", pageB.Name, pageB.ID)
		if modsIncompatibleAddReason != "" {
			fmt.Println(subtleStyle.Render(fmt.Sprintf("  reason: %s", modsIncompatibleAddReason)))
		}

		return nil
	},
}

func init() {
	modsIncompatibleCmd.AddCommand(modsIncompatibleAddCmd)

	modsIncompatibleAddCmd.Flags().StringVarP(&modsIncompatibleAddGame, "game", "g", "",
		"Override the currently active game")
	modsIncompatibleAddCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	modsIncompatibleAddCmd.Flags().StringVarP(&modsIncompatibleAddReason, "reason", "r", "",
		"Notes on mod incompatibility")
}
