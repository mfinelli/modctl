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

package argresolver

import (
	"fmt"
	"path/filepath"

	"github.com/mfinelli/modctl/internal/fsutil"
)

// ResolveRelpathArg is the path that a command is given as an argument for a
// file in a target, such as the file of a backup or an override, made ready to
// be looked up: cleaned, and as the path that is relative to the root of the
// target that it has to be. A path that is not one (it is absolute, it gets out
// of the root by way of "..", or it is the root itself) is an error that says
// what is wrong with it, as it was typed.
func ResolveRelpathArg(arg string) (string, error) {
	relpath := filepath.Clean(arg)

	if err := fsutil.ValidateRelPath(relpath); err != nil {
		return "", fmt.Errorf("invalid path %q: %w", arg, err)
	}

	return relpath, nil
}
