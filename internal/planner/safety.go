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

package planner

import (
	"errors"
	"path"
	"strings"
)

// validateDestPath rejects paths that are unsafe to write to disk.
// It checks for absolute paths, path traversal, and other dangerous patterns.
func validateDestPath(p string) error {
	if path.IsAbs(p) {
		return errors.New("absolute path not allowed")
	}

	// path.Clean resolves ".." segments - if the result escapes the root
	// it will start with ".."
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("path traversal not allowed")
	}

	// Reject paths that are just "." after cleaning
	if cleaned == "." {
		return errors.New("empty or dot path not allowed")
	}

	return nil
}
