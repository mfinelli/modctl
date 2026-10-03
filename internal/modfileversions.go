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
			ID:                 rows[0].ID,
			ModPageName:        rows[0].ModPageName,
			FileLabel:          rows[0].FileLabel,
			VersionString:      rows[0].VersionString,
			InventoryScannedAt: rows[0].InventoryScannedAt,
			ArchiveSha256:      rows[0].ArchiveSha256,
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

// ResolveEnabledProfileVersionArg resolves a mod file version argument for a
// command that acts on the enabled version of a mod in a profile (e.g.,
// disable). It behaves like ResolveModFileVersionArg except that when the
// argument is a mod page name matching several versions, the candidates are
// narrowed to the versions that are enabled in the given profile:
//   - exactly one enabled version: that version is returned
//   - several enabled versions: an error listing only those is returned
//   - none enabled: resolution falls back to ResolveModFileVersionArg so the
//     usual not-in-profile/already-disabled/ambiguity handling applies
func ResolveEnabledProfileVersionArg(ctx context.Context, q *dbq.Queries, gi dbq.GameInstall, profile *dbq.Profile, arg string) (int64, error) {
	if _, ok := ParseInt64(arg); !ok {
		rows, err := q.ListProfileVersionsByModPageName(ctx, dbq.ListProfileVersionsByModPageNameParams{
			ProfileID:     profile.ID,
			GameInstallID: gi.ID,
			Name:          arg,
		})
		if err != nil {
			return 0, fmt.Errorf("list profile versions by name: %w", err)
		}

		var enabled []dbq.ListProfileVersionsByModPageNameRow
		for _, r := range rows {
			if r.Enabled != 0 {
				enabled = append(enabled, r)
			}
		}

		switch len(enabled) {
		case 0:
			// fall through to the generic resolver
		case 1:
			return enabled[0].ID, nil
		default:
			var b strings.Builder
			fmt.Fprintf(&b, "Multiple enabled versions of %q in profile %q. Specify a numeric ID:\n\n", arg, profile.Name)
			for _, r := range enabled {
				version := "(no version)"
				if r.VersionString.Valid && r.VersionString.String != "" {
					version = r.VersionString.String
				}
				fmt.Fprintf(&b, "  %-6d  %s › %s (%s)\n", r.ID, r.ModPageName, r.FileLabel, version)
			}
			return 0, errors.New(strings.TrimRight(b.String(), "\n"))
		}
	}

	mfv, err := ResolveModFileVersionArg(ctx, q, gi, arg)
	if err != nil {
		return 0, err
	}
	return mfv.ID, nil
}
