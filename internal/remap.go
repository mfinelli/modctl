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

// ResolveProfileItemByVersion looks up a profile item by mod file version ID
// within a profile, returning a clean error if the version is not in the profile.
func ResolveProfileItemByVersion(ctx context.Context, profile *dbq.Profile, q *dbq.Queries, versionID int64) (int64, error) {
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

// EnsureRemapConfig returns the existing remap_config_id for a profile item,
// or creates a new one and links it to the profile item within the provided
// transaction if none exists yet.
func EnsureRemapConfig(ctx context.Context, qtx *dbq.Queries, itemID int64) (int64, error) {
	configID, err := qtx.GetProfileItemRemapConfigID(ctx, itemID)
	if err != nil {
		return 0, fmt.Errorf("get remap config id: %w", err)
	}
	if configID.Valid {
		return configID.Int64, nil
	}
	// No config yet - create one and link it
	newID, err := qtx.CreateRemapConfig(ctx)
	if err != nil {
		return 0, fmt.Errorf("create remap config: %w", err)
	}
	if err := qtx.SetProfileItemRemapConfig(ctx, dbq.SetProfileItemRemapConfigParams{
		RemapConfigID: sql.NullInt64{Int64: newID, Valid: true},
		ID:            itemID,
	}); err != nil {
		return 0, fmt.Errorf("link remap config to profile item: %w", err)
	}
	return newID, nil
}

// AddRemapRule adds a single remap rule to the config for a profile item,
// creating the config if it does not exist yet. The position is auto-assigned
// as MAX(position)+1 unless overridePos is >= 0.
func AddRemapRule(ctx context.Context, db *sql.DB, q *dbq.Queries, itemID int64, ruleType string, intVal sql.NullInt64, textVal sql.NullString, overridePos int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := q.WithTx(tx)

	configID, err := EnsureRemapConfig(ctx, qtx, itemID)
	if err != nil {
		return err
	}

	pos := overridePos
	if pos < 0 {
		maxPos, err := qtx.GetMaxRemapRulePosition(ctx, configID)
		if err != nil {
			return fmt.Errorf("get max remap rule position: %w", err)
		}
		pos = maxPos + 1
	}

	result, err := qtx.CreateRemapRule(ctx, dbq.CreateRemapRuleParams{
		RemapConfigID: configID,
		Position:      pos,
		RuleType:      ruleType,
		IntValue:      intVal,
		TextValue:     textVal,
	})
	if err != nil {
		return fmt.Errorf("create remap rule: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	fmt.Printf("Added %s rule at position %d\n", ruleType, result.Position)
	return nil
}

// ClearRemapConfig deletes all remap rules for a profile item's config and
// removes the config itself, leaving remap_config_id NULL on the profile item.
// No-op if the profile item has no remap config.
func ClearRemapConfig(ctx context.Context, db *sql.DB, q *dbq.Queries, itemID int64, profileName string, versionID int64) error {
	configID, err := q.GetProfileItemRemapConfigID(ctx, itemID)
	if err != nil {
		return fmt.Errorf("get remap config id: %w", err)
	}
	if !configID.Valid {
		fmt.Printf("Version %d in profile %q has no remap rules\n", versionID, profileName)
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := q.WithTx(tx)

	// Unlink config from profile item first to avoid FK issues
	if err := qtx.SetProfileItemRemapConfig(ctx, dbq.SetProfileItemRemapConfigParams{
		RemapConfigID: sql.NullInt64{Valid: false},
		ID:            itemID,
	}); err != nil {
		return fmt.Errorf("unlink remap config: %w", err)
	}

	// Deleting the config cascades to remap_rules via ON DELETE CASCADE
	if err := qtx.DeleteRemapConfig(ctx, configID.Int64); err != nil {
		return fmt.Errorf("delete remap config: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	fmt.Printf("Cleared remap rules for version %d in profile %q\n", versionID, profileName)
	return nil
}

// CopyRemapConfig copies remap rules from one profile item to another within
// the same profile. If the destination already has a config its rules are
// replaced. If the source has no config this is a no-op.
func CopyRemapConfig(ctx context.Context, db *sql.DB, q *dbq.Queries, srcItemID, dstItemID int64, srcVersionID, dstVersionID int64, profileName string) error {
	srcRules, err := q.ListRemapRulesForProfileItem(ctx, srcItemID)
	if err != nil {
		return fmt.Errorf("list source remap rules: %w", err)
	}
	if len(srcRules) == 0 {
		fmt.Printf("Version %d in profile %q has no remap rules to copy\n", srcVersionID, profileName)
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := q.WithTx(tx)

	// Clear existing config on dst if present
	dstConfigID, err := qtx.GetProfileItemRemapConfigID(ctx, dstItemID)
	if err != nil {
		return fmt.Errorf("get dst remap config id: %w", err)
	}
	if dstConfigID.Valid {
		if err := qtx.SetProfileItemRemapConfig(ctx, dbq.SetProfileItemRemapConfigParams{
			RemapConfigID: sql.NullInt64{Valid: false},
			ID:            dstItemID,
		}); err != nil {
			return fmt.Errorf("unlink dst remap config: %w", err)
		}
		if err := qtx.DeleteRemapConfig(ctx, dstConfigID.Int64); err != nil {
			return fmt.Errorf("delete dst remap config: %w", err)
		}
	}

	// Create a fresh config for dst
	newConfigID, err := qtx.CreateRemapConfig(ctx)
	if err != nil {
		return fmt.Errorf("create dst remap config: %w", err)
	}
	if err := qtx.SetProfileItemRemapConfig(ctx, dbq.SetProfileItemRemapConfigParams{
		RemapConfigID: sql.NullInt64{Int64: newConfigID, Valid: true},
		ID:            dstItemID,
	}); err != nil {
		return fmt.Errorf("link dst remap config: %w", err)
	}

	// Copy rules preserving positions
	for _, rule := range srcRules {
		if _, err := qtx.CreateRemapRule(ctx, dbq.CreateRemapRuleParams{
			RemapConfigID: newConfigID,
			Position:      rule.Position,
			RuleType:      rule.RuleType,
			IntValue:      rule.IntValue,
			TextValue:     rule.TextValue,
		}); err != nil {
			return fmt.Errorf("copy remap rule at position %d: %w", rule.Position, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	fmt.Printf("Copied %d remap rule(s) from version %d to version %d in profile %q\n",
		len(srcRules), srcVersionID, dstVersionID, profileName)
	return nil
}
