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

	"github.com/mfinelli/modctl/dbq"
)

func ResolveProfileArg(ctx context.Context, q *dbq.Queries, gi *dbq.GameInstall, arg string) (dbq.Profile, error) {
	if arg != "" {
		p, err := q.GetProfileByName(ctx, dbq.GetProfileByNameParams{
			GameInstallID: gi.ID,
			Name:          arg,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return dbq.Profile{}, fmt.Errorf("profile %q not found for this game", arg)
			}
			return dbq.Profile{}, fmt.Errorf("lookup profile: %w", err)
		}
		return p, nil
	} else {
		p, err := q.GetActiveProfileForGame(ctx, gi.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return dbq.Profile{}, fmt.Errorf("no active profile set; run `modctl profiles set-active <name>` or pass --profile")
			}
			return dbq.Profile{}, fmt.Errorf("get active profile: %w", err)
		}
		return p, nil
	}
}
