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

package argresolver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mfinelli/modctl/dbq"
)

// ResolveProfileItemByVersion looks up a profile item by mod file version ID
// within a profile, returning a clean error if the version is not in the profile.
func ResolveProfileItemByVersion(ctx context.Context, q *dbq.Queries, profile dbq.Profile, versionID int64) (int64, error) {
	itemID, err := q.GetProfileItemIDByVersion(ctx, dbq.GetProfileItemIDByVersionParams{
		ProfileID:        profile.ID,
		ModFileVersionID: versionID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("version %d is not in profile %q", versionID, profile.Name)
		}
		return 0, fmt.Errorf("lookup profile item: %w", err)
	}
	return itemID, nil
}
