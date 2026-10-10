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
	"io/fs"
	"os"
)

// Exists reports whether there is a file (or directory, or anything else) at
// path, following symlinks: a symlink whose target is missing doesn't exist.
//
// Only a path that isn't there is reported as not existing. Failing to find
// out for any other reason (a parent directory that can't be searched, or that
// is a file, an I/O error) is returned as an error: not knowing is not the same
// as the answer being no, and treating it that way would let callers go on to,
// for instance, overwrite a file without backing it up.
func Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	return false, err
}
