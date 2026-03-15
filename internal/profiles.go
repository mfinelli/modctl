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

func SetProfileItemEnabled(ctx context.Context, profile *dbq.Profile, q *dbq.Queries, versionID int64, enabled bool) error {
	// Find the profile item row for this version.
	item, err := q.GetProfileItemByVersion(ctx, dbq.GetProfileItemByVersionParams{
		ProfileID:        profile.ID,
		ModFileVersionID: versionID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("version %d is not in profile %q", versionID, profile.Name)
		}
		return fmt.Errorf("lookup profile item: %w", err)
	}

	want := int64(0)
	if enabled {
		want = 1
	}

	if item.Enabled == want {
		// Idempotent.
		if enabled {
			fmt.Printf("Version %d is already enabled in profile %q\n", versionID, profile.Name)
		} else {
			fmt.Printf("Version %d is already disabled in profile %q\n", versionID, profile.Name)
		}
		return nil
	}

	if err := q.SetProfileItemEnabled(ctx, dbq.SetProfileItemEnabledParams{
		Enabled: want,
		ID:      item.ID,
	}); err != nil {
		return fmt.Errorf("update enabled: %w", err)
	}

	if enabled {
		fmt.Printf("Enabled version %d in profile %q\n", versionID, profile.Name)
	} else {
		fmt.Printf("Disabled version %d in profile %q\n", versionID, profile.Name)
	}

	return nil
}
