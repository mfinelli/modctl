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

// StoreIDs completes (enabled) store IDs
// Returns candidates in "id\tDisplay Name" format.
func StoreIDs(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := context.Background()

	db, err := internal.SetupDBReadOnly()
	if err != nil {
		// No DB (not initialized) or error: don't fall back to file completion.
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer db.Close()

	q := dbq.New(db)
	rows, err := q.ListEnabledStoresForCompletion(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	needle := strings.ToLower(toComplete)
	out := make([]string, 0, len(rows))
	for _, s := range rows {
		// do the prefix check in code... we have so few rows that
		// it doesn't matter
		if strings.HasPrefix(strings.ToLower(s.ID), needle) {
			out = append(out, fmt.Sprintf("%s\t%s", s.ID, s.DisplayName))
		}
	}

	return out, cobra.ShellCompDirectiveNoFileComp
}
