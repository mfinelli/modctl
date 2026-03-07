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

package internal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mfinelli/modctl/dbq"
)

// ResolveModPageArg resolves a mod page argument for the given game install.
// Accepts:
//   - a numeric ID (fast path, unambiguous)
//   - a mod page name (case-insensitive; errors if ambiguous, listing all
//     candidates with their numeric IDs)
func ResolveModPageArg(ctx context.Context, q *dbq.Queries, gi dbq.GameInstall, arg string) (dbq.GetModPageByIDForGameRow, error) {
	if id, ok := ParseInt64(arg); ok {
		row, err := q.GetModPageByIDForGame(ctx, dbq.GetModPageByIDForGameParams{
			ID:            id,
			GameInstallID: gi.ID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return dbq.GetModPageByIDForGameRow{}, fmt.Errorf("no mod page with id %d for game %q", id, gi.DisplayName)
			}
			return dbq.GetModPageByIDForGameRow{}, fmt.Errorf("get mod page: %w", err)
		}
		return row, nil
	}

	rows, err := q.GetModPagesByName(ctx, dbq.GetModPagesByNameParams{
		GameInstallID: gi.ID,
		Name:          arg,
	})
	if err != nil {
		return dbq.GetModPageByIDForGameRow{}, fmt.Errorf("get mod pages by name: %w", err)
	}

	switch len(rows) {
	case 0:
		return dbq.GetModPageByIDForGameRow{}, fmt.Errorf("no mod page found for %q in game %q", arg, gi.DisplayName)
	case 1:
		return dbq.GetModPageByIDForGameRow{
			ID:         rows[0].ID,
			Name:       rows[0].Name,
			SourceKind: rows[0].SourceKind,
		}, nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "Multiple mod pages found for %q. Specify a numeric ID:\n\n", arg)
		for _, r := range rows {
			fmt.Fprintf(&b, "  %-6d  %s (%s)\n", r.ID, r.Name, r.SourceKind)
		}
		return dbq.GetModPageByIDForGameRow{}, errors.New(strings.TrimRight(b.String(), "\n"))
	}
}
