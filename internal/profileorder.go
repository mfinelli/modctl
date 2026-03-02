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
	"fmt"

	"github.com/mfinelli/modctl/dbq"
)

// RewriteProfilePriorities rewrites priorities for the given ordered items.
//
// It is safe under UNIQUE(profile_id, priority) because it first shifts all
// existing priorities out of the way by an offset, then assigns final values.
//
// start is the first priority value to assign (for compact: 1).
// step is the increment between consecutive priorities (for compact: 1; for --multiple N: N).
func RewriteProfilePriorities(
	ctx context.Context,
	qtx *dbq.Queries,
	profileID int64,
	items []dbq.ListProfileItemsForOrderRow,
	start, step int64,
) error {
	if step <= 0 {
		return fmt.Errorf("step must be > 0")
	}
	if len(items) == 0 {
		return nil
	}

	// Move all priorities out of the way so we never collide when writing
	// new ones
	// Offset choice: max+1 ensures that "priority+offset" won't overlap
	// any newly assigned priorities starting at `start`
	maxPrio, err := qtx.GetMaxPriorityForProfile(ctx, profileID)
	if err != nil {
		return fmt.Errorf("get max priority: %w", err)
	}
	offset := (maxPrio + 1)
	if offset < 1 {
		offset = 1
	}

	if err := qtx.BumpPrioritiesForProfile(ctx, dbq.BumpPrioritiesForProfileParams{
		Offset:    offset, // if your sqlc param name is different, adjust
		ProfileID: profileID,
	}); err != nil {
		return fmt.Errorf("bump priorities: %w", err)
	}

	// Assign final compacted priorities
	next := start
	for _, it := range items {
		if err := qtx.SetProfileItemPriorityByID(ctx, dbq.SetProfileItemPriorityByIDParams{
			Priority: next,
			ID:       it.ID,
		}); err != nil {
			return fmt.Errorf("set priority (id=%d): %w", it.ID, err)
		}
		next += step
	}

	return nil
}
