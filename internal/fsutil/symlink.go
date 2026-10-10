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

// SymlinkTarget reports whether path itself is a symlink, and where it points
// to as it is written in the link (which can be a relative path, and doesn't
// have to be there).
//
// Only the last element of path is looked at: a file that is in a directory
// that is a symlink is not itself one. A path that isn't there is not a
// symlink; failing to find out for any other reason is an error, as it is for
// Exists.
func SymlinkTarget(path string) (target string, isLink bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}

	if info.Mode()&fs.ModeSymlink == 0 {
		return "", false, nil
	}

	target, err = os.Readlink(path)
	if err != nil {
		return "", false, err
	}

	return target, true, nil
}
