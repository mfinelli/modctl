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
	"github.com/spf13/cobra"
)

func likePrefixPattern(s string) string {
	// Escape LIKE wildcards so user input is treated literally.
	// Then append % for prefix match.
	repl := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return repl.Replace(s) + `%`
}

// GameInstallSelectors completes "games set-active <selector>".
// It returns *full selectors* (always includes #instance) with a description.
func GameInstallSelectors(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := context.Background()

	db, err := internal.SetupDBReadOnly()
	if err != nil {
		// No DB (not initialized) or error: don't fall back to file completion.
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer db.Close()

	pat := likePrefixPattern(strings.TrimSpace(toComplete))

	q := dbq.New(db)
	rows, err := q.CompleteGameInstallsByPrefix(ctx, pat)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		sel := internal.FullSelector(r.StoreID, r.StoreGameID, r.InstanceID)

		desc := r.DisplayName
		if r.IsPresent == 0 {
			desc = desc + " (missing)"
		}

		out = append(out, fmt.Sprintf("%s\t%s", sel, desc))
	}

	return out, cobra.ShellCompDirectiveNoFileComp
}
