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

package extractor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// PruneDirs attempts to remove empty directories that were ancestors of the
// given relative paths, bounded by targetRoot. It is intended to be called
// after a set of file remove/restore operations to clean up directories that
// modctl emptied. Directories that are non-empty are silently skipped.
// Returns a slice of warning strings for any unexpected errors.
func PruneDirs(targetRoot string, paths []string) []string {
	// Collect unique ancestor directories, excluding targetRoot itself
	dirSet := make(map[string]struct{})
	for _, p := range paths {
		// Walk up from the file's immediate parent to (but not including)
		// the target root
		dir := filepath.Dir(filepath.Join(targetRoot, p))
		for {
			// Stop if we've reached or escaped the target root
			rel, err := filepath.Rel(targetRoot, dir)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
				break
			}
			dirSet[dir] = struct{}{}
			dir = filepath.Dir(dir)
		}
	}

	if len(dirSet) == 0 {
		return nil
	}

	// Sort deepest first so we remove leaves before parents, giving parents
	// a chance to become empty once their children are removed
	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		// More path separators = deeper. Deeper sorts first.
		return strings.Count(dirs[i], string(filepath.Separator)) >
			strings.Count(dirs[j], string(filepath.Separator))
	})

	var warnings []string
	for _, dir := range dirs {
		err := os.Remove(dir)
		if err == nil {
			continue
		}
		// Non-empty directory: expected, skip silently
		if errors.Is(err, syscall.ENOTEMPTY) {
			continue
		}
		// Already gone: fine
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		// Anything else is unexpected
		rel, relErr := filepath.Rel(targetRoot, dir)
		if relErr != nil {
			rel = dir
		}
		warnings = append(warnings, fmt.Sprintf("prune-dirs: could not remove %q: %v", rel, err))
	}

	return warnings
}
