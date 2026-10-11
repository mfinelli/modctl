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

package restore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/database"
)

// The targets of a bundle say where files are written to, and what a bundle says
// is not trusted: the path of a target is the one of the machine that the bundle
// is from, and could be anything. So the targets are imported disabled, with
// "unverified" in their metadata, and finding one of them when discovering what
// is installed here (a refresh) confirms it, with the path of this machine (see
// UpsertDiscoveredTarget in queries.sql). One that is not found that way, which
// is any that a user made by hand, has to be enabled by the user.
//
// A target that was disabled in the bundle stays disabled, and does not get the
// mark: finding it is not a reason to enable what was turned off.

// targetOriginCustom is the origin of a target that was made by the user, and not
// by discovery.
const targetOriginCustom = "user_override"

// DisabledTarget is a target that was imported disabled.
type DisabledTarget struct {
	Game     string
	Name     string
	RootPath string

	// Custom is whether it is a target that the user made: nothing on this
	// machine is going to confirm it.
	Custom bool
}

// unverifiedTarget is what an imported target that is enabled is to be in the
// database: disabled, with the mark in its metadata (which keeps what else it
// has). A target that is disabled is left alone (changed is false).
func unverifiedTarget(enabled int64, metadata sql.NullString) (marked sql.NullString, changed bool) {
	if enabled == 0 {
		return metadata, false
	}

	obj := map[string]any{}
	if metadata.Valid {
		var parsed any
		if err := json.Unmarshal([]byte(metadata.String), &parsed); err == nil {
			if m, ok := parsed.(map[string]any); ok {
				obj = m
			}
		}
	}
	obj["unverified"] = true

	out, err := json.Marshal(obj)
	if err != nil {
		// a map of what was just parsed, and a bool, can't fail to be written
		panic(fmt.Sprintf("marshal target metadata: %v", err))
	}

	return sql.NullString{String: string(out), Valid: true}, true
}

// disableImportedTargets is for a full restore that has replaced the database
// with the one of the bundle: every target that is enabled is disabled and
// marked, which is what the import of a game does with its targets as it
// inserts them.
func disableImportedTargets(ctx context.Context, db *sql.DB) ([]DisabledTarget, error) {
	var disabled []DisabledTarget

	err := database.WithTx(ctx, db, func(qtx *dbq.Queries) error {
		targets, err := qtx.ListEnabledTargetsWithGame(ctx)
		if err != nil {
			return fmt.Errorf("list targets: %w", err)
		}

		for _, t := range targets {
			marked, _ := unverifiedTarget(1, t.Metadata)
			if err := qtx.DisableTargetWithMetadata(ctx, dbq.DisableTargetWithMetadataParams{
				Metadata: marked,
				ID:       t.ID,
			}); err != nil {
				return fmt.Errorf("disable target %q of %q: %w", t.Name, t.Game, err)
			}
			disabled = append(disabled, DisabledTarget{
				Game:     t.Game,
				Name:     t.Name,
				RootPath: t.RootPath,
				Custom:   t.Origin == targetOriginCustom,
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return disabled, nil
}
