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

package completion

import "strings"

// A Completer completes what a command is given, from the database and the
// active selection that it is given: what it knows is what it is made with,
// and nothing it can find by itself, so one is made for the place that a test
// has set up as easily as for the one that the user has.
//
// A completion is not allowed to fail loudly, or to fall back to the files of
// the directory it is in: if the database is not there, or the active game is
// not known, there is nothing to complete.
type Completer struct {
	// DatabasePath is the file of the database, which is only read.
	DatabasePath string

	// StateDir is the directory that the active selection is kept in.
	StateDir string
}

func likePrefixPattern(s string) string {
	// Escape LIKE wildcards so user input is treated literally.
	// Then append % for prefix match.
	repl := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return repl.Replace(s) + `%`
}
