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
	"path/filepath"
	"strings"
)

// isUnderDir reports whether the given path resides within the directory dir.
//
// Both path and dir are first converted to absolute paths to avoid surprises
// with relative paths. The function then computes the relative path from dir
// to path using filepath.Rel.
//
// If the resulting relative path starts with "..", the path lies outside dir.
// Otherwise, it is considered inside dir (including the case where path == dir).
//
// This avoids unsafe string-prefix checks such as:
//
//	strings.HasPrefix(path, dir)
//
// which can produce false positives (e.g. "/foo/bar-baz" vs "/foo/bar")
// and does not correctly handle ".." traversal.
//
// The function does not resolve symlinks. If symlink-aware containment checks
// are required, both paths should be resolved via filepath.EvalSymlinks first.
func IsUnderDir(path, dir string) (bool, error) {
	ap, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}

	ad, err := filepath.Abs(dir)
	if err != nil {
		return false, err
	}

	// Compute relative path from dir -> path.
	rel, err := filepath.Rel(ad, ap)
	if err != nil {
		return false, err
	}

	if rel == "." {
		// path and dir are the same directory.
		return true, nil
	}

	// If rel begins with "..", then path escapes dir.
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return false, nil
	}

	// Defensive: if Rel somehow returned an absolute path (shouldn't happen),
	// treat it as outside.
	if filepath.IsAbs(rel) {
		return false, nil
	}

	return true, nil
}
