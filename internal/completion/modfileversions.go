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

// ModFileVersionIDs completes mod file version IDs for the active (or
// --game-scoped) game install. Returns candidates as numeric IDs with
// a "Mod Page › File Label (version)" description.
func ModFileVersionIDs(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
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
	pat := likePrefixPattern(strings.TrimSpace(toComplete))
	rows, err := q.CompleteModFileVersionsByGameInstall(ctx, dbq.CompleteModFileVersionsByGameInstallParams{
		GameInstallID: gameID,
		Prefix:        pat,
	})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		desc := r.ModPageName + " › " + r.FileLabel
		if r.VersionString.Valid && r.VersionString.String != "" {
			desc += " (" + r.VersionString.String + ")"
		}
		out = append(out, fmt.Sprintf("%d\t%s", r.ID, desc))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
