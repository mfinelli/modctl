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

package completion

import (
	"context"
	"fmt"
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

// TargetNames completes target names for the current game install.
// If the command has a --game flag set, it is used; otherwise the active game
// is used.
//
// Returns candidates in "name\troot_path" format so the user can see where
// each target points without having to run a separate command.
func TargetNames(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := context.Background()
	db, err := internal.SetupDBReadOnly()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer db.Close()

	var gameID int64
	if f := cmd.Flags().Lookup("game"); f != nil && f.Changed {
		v, err := cmd.Flags().GetInt64("game")
		if err != nil || v <= 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		gameID = v
	} else {
		active, err := state.LoadActive()
		if err != nil || active.ActiveGameInstallID <= 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		gameID = active.ActiveGameInstallID
	}

	q := dbq.New(db)
	rows, err := q.ListTargetsForGameInstall(ctx, gameID)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	needle := strings.ToLower(toComplete)
	out := make([]string, 0, len(rows))
	for _, t := range rows {
		if strings.HasPrefix(strings.ToLower(t.Name), needle) {
			out = append(out, fmt.Sprintf("%s\t%s", t.Name, t.RootPath))
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
