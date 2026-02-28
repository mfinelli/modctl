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

// ProfileNames completes profile names for the current game install.
// If the command has a --game flag set, it is used; otherwise the active game
// is used.
//
// N.B. that at the moment we're using Flags() on all of the profile commands
//
//	individually (instead of PersistentFlags() on the profiles root
//	command) which means if we ever switch to PersistentFlags we need to
//	update here to use cmd.InheritedFlags() instead of cmd.Flags().
//
// Returns candidates in "name\t(active)" format.
func ProfileNames(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := context.Background()

	db, err := internal.SetupDBReadOnly()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer db.Close()

	// Resolve game install id for completion scope.
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
	rows, err := q.ListProfilesForCompletion(ctx, gameID)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	needle := strings.ToLower(toComplete)
	out := make([]string, 0, len(rows))
	for _, p := range rows {
		if strings.HasPrefix(strings.ToLower(p.Name), needle) {
			hint := ""
			if p.IsActive != 0 {
				hint = "active"
			}
			if hint != "" {
				out = append(out, fmt.Sprintf("%s\t%s", p.Name, hint))
			} else {
				out = append(out, p.Name)
			}
		}
	}

	return out, cobra.ShellCompDirectiveNoFileComp
}
