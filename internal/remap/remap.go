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

// Package remap implements the remap rule engine for modctl.
// Rules are applied sequentially in position order to transform or filter
// archive entry paths before they are handed to the planner.
package remap

import (
	"path"
	"strings"

	"github.com/mfinelli/modctl/dbq"
)

// Result is the outcome of applying a remap rule set to a single path
type Result struct {
	// Path is the transformed destination path. Empty if Skip is true
	Path string
	// Skip is true if the entry should be excluded from the install
	Skip bool
}

// Apply applies a slice of RemapRules (sorted by position ascending) to the
// given raw archive path. It returns the transformed path or a skip signal.
// Rules are applied sequentially; each rule sees the path as transformed by
// all previous rules.
func Apply(rules []dbq.RemapRule, rawPath string) (Result, error) {
	current := rawPath

	for _, rule := range rules {
		switch rule.RuleType {
		case "strip_components":
			n := int(rule.IntValue.Int64)
			current = stripComponents(current, n)
			// if stripping leaves us with an empty path, skip the entry
			if current == "" {
				return Result{Skip: true}, nil
			}

		case "select_subdir":
			subdir := rule.TextValue.String
			// normalise both sides to avoid slash inconsistencies
			subdirClean := path.Clean(subdir)
			currentClean := path.Clean(current)
			prefix := subdirClean + "/"
			if currentClean == subdirClean {
				// the entry IS the subdir itself (a directory entry); skip it
				return Result{Skip: true}, nil
			}
			if !strings.HasPrefix(currentClean, prefix) {
				return Result{Skip: true}, nil
			}
			// strip the subdir prefix so the entry installs relative to it
			current = currentClean[len(prefix):]
			if current == "" {
				return Result{Skip: true}, nil
			}

		case "dest_prefix":
			prefix := path.Clean(rule.TextValue.String)
			current = prefix + "/" + current

		case "include_glob":
			pattern := rule.TextValue.String
			matched, err := path.Match(pattern, current)
			if err != nil {
				return Result{}, &InvalidGlobError{Pattern: pattern, Err: err}
			}
			if !matched {
				return Result{Skip: true}, nil
			}

		case "exclude_glob":
			pattern := rule.TextValue.String
			matched, err := path.Match(pattern, current)
			if err != nil {
				return Result{}, &InvalidGlobError{Pattern: pattern, Err: err}
			}
			if matched {
				return Result{Skip: true}, nil
			}
		}
	}

	return Result{Path: current}, nil
}

// stripComponents removes the first n path segments from p.
// Returns an empty string if n >= the number of segments.
func stripComponents(p string, n int) string {
	if n <= 0 {
		return p
	}
	// clean first to normalise any double slashes, dot segments, etc.
	p = path.Clean(p)
	parts := strings.Split(p, "/")
	if n >= len(parts) {
		return ""
	}
	return strings.Join(parts[n:], "/")
}

// InvalidGlobError is returned when a glob pattern in a remap rule is malformed.
type InvalidGlobError struct {
	Pattern string
	Err     error
}

func (e *InvalidGlobError) Error() string {
	return "invalid glob pattern " + e.Pattern + ": " + e.Err.Error()
}

func (e *InvalidGlobError) Unwrap() error {
	return e.Err
}
