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

// ResolveModFileVersionArg resolves a mod file version argument for the given
// game install. Accepts:
//   - a numeric ID (fast path, unambiguous)
//   - a mod page name (case-insensitive; errors if ambiguous, listing all
//     candidates with their numeric IDs)
func ResolveModFileVersionArg(ctx context.Context, q *dbq.Queries, gi dbq.GameInstall, arg string) (dbq.GetModFileVersionByIDRow, error) {
	// Fast path: numeric ID
	if id, ok := ParseInt64(arg); ok {
		row, err := q.GetModFileVersionByID(ctx, dbq.GetModFileVersionByIDParams{
			ID:            id,
			GameInstallID: gi.ID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return dbq.GetModFileVersionByIDRow{}, fmt.Errorf("no mod file version with id %d for game %q", id, gi.DisplayName)
			}
			return dbq.GetModFileVersionByIDRow{}, fmt.Errorf("get mod file version: %w", err)
		}
		return row, nil
	}

	// Name path: look up by mod page name
	rows, err := q.GetModFileVersionsByName(ctx, dbq.GetModFileVersionsByNameParams{
		GameInstallID: gi.ID,
		Name:          arg,
	})
	if err != nil {
		return dbq.GetModFileVersionByIDRow{}, fmt.Errorf("get mod file versions by name: %w", err)
	}

	switch len(rows) {
	case 0:
		return dbq.GetModFileVersionByIDRow{}, fmt.Errorf("no mod file versions found for %q in game %q", arg, gi.DisplayName)
	case 1:
		return dbq.GetModFileVersionByIDRow{
			ID:            rows[0].ID,
			ModPageName:   rows[0].ModPageName,
			FileLabel:     rows[0].FileLabel,
			VersionString: rows[0].VersionString,
		}, nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "Multiple mod file versions found for %q. Specify a numeric ID:\n\n", arg)
		for _, r := range rows {
			version := "(no version)"
			if r.VersionString.Valid && r.VersionString.String != "" {
				version = r.VersionString.String
			}
			fmt.Fprintf(&b, "  %-6d  %s › %s (%s)\n", r.ID, r.ModPageName, r.FileLabel, version)
		}
		return dbq.GetModFileVersionByIDRow{}, errors.New(strings.TrimRight(b.String(), "\n"))
	}
}
