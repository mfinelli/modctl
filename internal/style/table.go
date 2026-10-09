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

package style

import "github.com/charmbracelet/lipgloss/table"

// Table renders rows under a header as a table. Cells are rendered as they are
// given, so pad them (for example with spaces on either side) if you want them
// to have some room.
func Table(headers []string, rows [][]string) string {
	return table.New().
		Headers(headers...).
		Rows(rows...).
		String()
}
