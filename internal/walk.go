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

// WalkUpdateChain follows the file_updates chain from startFileID and returns
// the terminal (latest) file_id, or startFileID if no updates exist, using a
// prebuilt map of old_file_id -> new_file_id.
func WalkUpdateChain(startFileID int64, next map[int64]int64) int64 {
	current := startFileID
	seen := make(map[int64]struct{}) // guard against cycles

	for {
		if _, visited := seen[current]; visited {
			break
		}
		seen[current] = struct{}{}
		if n, ok := next[current]; ok {
			current = n
		} else {
			break
		}
	}

	return current
}
