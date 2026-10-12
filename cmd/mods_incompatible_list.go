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
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
)

var modsIncompatibleListGame string

var modsIncompatibleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all incompatibility flags for the current game install",
	Long: `List all incompatibility flags for the current game install.

Shows all pairs of mods that have been flagged as incompatible, regardless
of which profiles they appear in.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		db, err := openDB(ctx)
		if err != nil {
			return err
		}
		defer db.Close()

		q := dbq.New(db)

		// Resolve game install id: --game overrides active selection
		gi, err := gameInstall(ctx, q, modsIncompatibleListGame)
		if err != nil {
			return err
		}

		pairs, err := q.ListModIncompatibilities(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("listing incompatibilities: %w", err)
		}

		style.Println(renderIncompatibilityList(pairs))
		return nil
	},
}

func init() {
	modsIncompatibleCmd.AddCommand(modsIncompatibleListCmd)

	modsIncompatibleListCmd.Flags().StringVarP(&modsIncompatibleListGame, "game", "g", "",
		"Override the currently active game")
	modsIncompatibleListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().GameInstallSelectors(cmd, toComplete)
		})
}

func renderIncompatibilityList(pairs []dbq.ListModIncompatibilitiesRow) string {
	kvIndented := style.KV{Indent: 6, Width: 12}

	var b strings.Builder

	b.WriteString(style.Section.Render(fmt.Sprintf("Incompatibilities (%d)", len(pairs))) + "\n")

	if len(pairs) == 0 {
		b.WriteString(style.Subtle.Render("  (none)") + "\n")
		return strings.TrimRight(b.String(), "\n")
	}

	for _, pair := range pairs {
		b.WriteString(fmt.Sprintf("  %s\n",
			style.Warning.Render(fmt.Sprintf("⚠  %s × %s",
				pair.ModPageNameA, pair.ModPageNameB))))
		kvIndented.Write(&b, "ids:", fmt.Sprintf("%d, %d", pair.ModPageIDA, pair.ModPageIDB))
		if pair.Reason.Valid && strings.TrimSpace(pair.Reason.String) != "" {
			kvIndented.Write(&b, "reason:", pair.Reason.String)
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}
