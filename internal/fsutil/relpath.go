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

package fsutil

import (
	"errors"
	"path"
	"strings"
)

// ValidateRelPath rejects a path that is not safe to write inside a directory
// that it is meant to be relative to (the root of a target, say): an absolute
// path, one that gets out of the directory by way of "..", and one that is
// nothing but the directory itself.
// The path is judged as it is written (with slashes), without looking at the
// disk, so symlinks are not part of what it can tell.
func ValidateRelPath(p string) error {
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
