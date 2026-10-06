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
	"fmt"
	"strings"

	"github.com/mfinelli/modctl/dbq"
)

// DefaultTargetName is the target used when a command isn't told which one
// to use.
const DefaultTargetName = "game_dir"

// TargetEnabled reports whether the target takes part in planning.
func TargetEnabled(t dbq.Target) bool {
	return t.Enabled != 0
}

// EnabledTargets returns the enabled subset of targets, preserving order.
func EnabledTargets(targets []dbq.Target) []dbq.Target {
	var out []dbq.Target
	for _, t := range targets {
		if TargetEnabled(t) {
			out = append(out, t)
		}
	}
	return out
}

// EnsureTargetEnabled returns an error if the target is disabled.
func EnsureTargetEnabled(t dbq.Target) error {
	if TargetEnabled(t) {
		return nil
	}
	return fmt.Errorf("target %q is disabled; run `modctl games targets enable %s` to use it", t.Name, t.Name)
}

// ResolveTarget picks the install target for a command from the targets of a
// game install. A requested name must exist and be enabled. With no name the
// default target (game_dir) is used if it is enabled; if it isn't, the only
// enabled target is used, and it is an error to have to guess between several.
func ResolveTarget(targets []dbq.Target, requested string) (dbq.Target, error) {
	if requested != "" {
		for _, t := range targets {
			if t.Name == requested {
				if err := EnsureTargetEnabled(t); err != nil {
					return dbq.Target{}, err
				}
				return t, nil
			}
		}
		return dbq.Target{}, fmt.Errorf("target %q not found; run `modctl games targets list` to see available targets", requested)
	}

	enabled := EnabledTargets(targets)
	for _, t := range enabled {
		if t.Name == DefaultTargetName {
			return t, nil
		}
	}

	switch len(enabled) {
	case 0:
		return dbq.Target{}, fmt.Errorf("no enabled targets; run `modctl games targets enable <name>`")
	case 1:
		return enabled[0], nil
	default:
		names := make([]string, len(enabled))
		for i, t := range enabled {
			names[i] = t.Name
		}
		return dbq.Target{}, fmt.Errorf(
			"the default target %q is not enabled and several targets are (%s); pass --target",
			DefaultTargetName, strings.Join(names, ", "),
		)
	}
}

// TargetUsage describes what still depends on a target.
type TargetUsage struct {
	InstalledFiles int64 // files recorded as installed through the target
	ProfileItems   int64 // profile items deploying to the target
	Overrides      int64 // overrides defined for the target
	EnabledTargets int64 // enabled targets for the game install, counting this one
}

// CheckTargetCanBeDisabled returns an error explaining why an enabled target
// can't be disabled: it still has installed files, profile items or
// overrides pointing at it, or it is the last enabled target.
func CheckTargetCanBeDisabled(t dbq.Target, u TargetUsage) error {
	if u.InstalledFiles > 0 {
		return fmt.Errorf(
			"target %q has %d installed file(s); unapply the profile before disabling this target",
			t.Name, u.InstalledFiles,
		)
	}

	if u.ProfileItems > 0 || u.Overrides > 0 {
		var parts []string
		if u.ProfileItems > 0 {
			parts = append(parts, fmt.Sprintf("%d profile item(s)", u.ProfileItems))
		}
		if u.Overrides > 0 {
			parts = append(parts, fmt.Sprintf("%d override(s)", u.Overrides))
		}
		return fmt.Errorf(
			"target %q is still used by %s; remove them before disabling this target",
			t.Name, strings.Join(parts, " and "),
		)
	}

	if u.EnabledTargets <= 1 {
		return fmt.Errorf("target %q is the only enabled target and cannot be disabled", t.Name)
	}

	return nil
}
