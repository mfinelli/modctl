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
	"strconv"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
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

	q := dbq.New(db)

	gi, err := resolveGameInstallForCompletion(cmd, ctx, q)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	rows, err := q.CompleteModFileVersionsByGameInstall(ctx, gi.ID)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		desc := r.FileLabel
		if r.VersionString.Valid && r.VersionString.String != "" {
			desc += " (" + r.VersionString.String + ")"
		}
		out = append(out, fmt.Sprintf("%s\t%s", r.ModPageName, desc))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// ModFileVersionIDsForPage completes mod file version IDs filtered to a
// specific mod page. modPageArg may be a numeric ID or a name; if it is
// non-empty and cannot be resolved unambiguously, no completions are returned.
// Returns candidates as numeric IDs with a "File Label (version)" description.
func ModFileVersionIDsForPage(cmd *cobra.Command, modPageArg string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := context.Background()
	db, err := internal.SetupDBReadOnly()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer db.Close()

	q := dbq.New(db)

	gi, err := resolveGameInstallForCompletion(cmd, ctx, q)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// If a mod page arg was provided, resolve it and filter by page.
	// If it can't be resolved unambiguously, return nothing.
	if modPageArg != "" {
		var pageID int64
		if id, ok := internal.ParseInt64(modPageArg); ok {
			pageID = id
		} else {
			rows, err := q.GetModPagesByName(ctx, dbq.GetModPagesByNameParams{
				GameInstallID: gi.ID,
				Name:          modPageArg,
			})
			if err != nil || len(rows) != 1 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			pageID = rows[0].ID
		}

		rows, err := q.CompleteModFileVersionsByPageAndGameInstall(ctx,
			dbq.CompleteModFileVersionsByPageAndGameInstallParams{
				GameInstallID: gi.ID,
				ID:            pageID,
				Prefix:        "%",
			})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		out := make([]string, 0, len(rows))
		for _, r := range rows {
			desc := r.FileLabel
			if r.VersionString.Valid && r.VersionString.String != "" {
				desc += " (" + r.VersionString.String + ")"
			}
			out = append(out, fmt.Sprintf("%d\t%s", r.ID, desc))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}

	// No mod page arg: fall back to unfiltered completion
	rows, err := q.CompleteModFileVersionsByGameInstall(ctx, gi.ID)
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

// resolveGameInstallForCompletion resolves the game install for a completion
// function, preferring the --game flag if set, otherwise falling back to the
// active selection.
func resolveGameInstallForCompletion(cmd *cobra.Command, ctx context.Context, q *dbq.Queries) (dbq.GameInstall, error) {
	gameArg := ""
	if f := cmd.Flags().Lookup("game"); f != nil && f.Changed {
		gameArg = f.Value.String()
	} else {
		active, err := state.LoadActive()
		if err != nil || active.ActiveGameInstallID <= 0 {
			return dbq.GameInstall{}, fmt.Errorf("no active game")
		}
		gameArg = strconv.FormatInt(active.ActiveGameInstallID, 10)
	}
	return argresolver.ResolveGameInstallArg(ctx, q, gameArg)
}
