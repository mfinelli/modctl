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

func ResolveGameInstallArg(ctx context.Context, q *dbq.Queries, arg string) (dbq.GameInstall, error) {
	// Fast path: numeric ID
	// TODO: i'm not sure if I actually want this or not...
	if id, ok := ParseInt64(arg); ok {
		gi, err := q.GetGameInstallByID(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return dbq.GameInstall{}, fmt.Errorf("no game install with id %d", id)
			}
			return dbq.GameInstall{}, fmt.Errorf("get game install by id: %w", err)
		}
		return gi, nil
	}

	// Selector path
	storeID, storeGameID, instanceID, err := ParseSelector(arg)
	if err != nil {
		return dbq.GameInstall{}, err
	}

	// If user provided an explicit instance, lookup is unambiguous.
	gi, err := q.GetGameInstallBySelector(ctx, dbq.GetGameInstallBySelectorParams{
		StoreID:     storeID,
		StoreGameID: storeGameID,
		InstanceID:  instanceID,
	})
	if err == nil {
		return gi, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return dbq.GameInstall{}, fmt.Errorf("get game install: %w", err)
	}

	// If instance was explicitly provided and we didn't find it -> not found.
	// ParseSelector defaults to "default" when omitted, so we need to distinguish.
	// We'll treat the input containing '#' as "explicit instance".
	if strings.Contains(arg, "#") {
		return dbq.GameInstall{}, fmt.Errorf("no game install found for %s",
			FullSelector(storeID, storeGameID, instanceID))
	}

	// Instance omitted: maybe ambiguous, list candidates
	rows, lerr := q.ListGameInstallsByStoreGameID(ctx, dbq.ListGameInstallsByStoreGameIDParams{
		StoreID:     storeID,
		StoreGameID: storeGameID,
	})
	if lerr != nil {
		return dbq.GameInstall{}, fmt.Errorf("list candidates: %w", lerr)
	}
	if len(rows) == 0 {
		return dbq.GameInstall{}, fmt.Errorf("no game installs found for %s:%s", storeID, storeGameID)
	}
	if len(rows) == 1 {
		// Only one install exists: treat it as the selected one
		only := rows[0]
		gi2, gerr := q.GetGameInstallByID(ctx, only.ID)
		if gerr != nil {
			return dbq.GameInstall{}, fmt.Errorf("get game install by id: %w", gerr)
		}
		return gi2, nil
	}

	// Ambiguous: show choices and require instance
	var b strings.Builder
	fmt.Fprintf(&b, "Multiple installs found for %s:%s. Choose one:\n\n", storeID, storeGameID)
	for _, r := range rows {
		sel := FullSelector(r.StoreID, r.StoreGameID, r.InstanceID)
		present := "present"
		if r.IsPresent == 0 {
			present = "missing"
		}
		lastSeen := ""
		if r.LastSeenAt.Valid {
			lastSeen = r.LastSeenAt.String
		}
		fmt.Fprintf(&b, "  %s  (%s)  %s  %s\n", sel, r.DisplayName, present, lastSeen)
	}
	return dbq.GameInstall{}, errors.New(b.String())
}
