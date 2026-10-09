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

// Package style holds how modctl looks in a terminal, so that no command
// formats its own output:
//
//   - the styles, in two layers: the palette (palette.go) of named ANSI colors,
//     and the roles (roles.go), styles named for what they are used for and
//     defined in terms of the palette;
//   - aligned label/value lines (kv.go) and tables (table.go);
//   - formatting of sizes, times and hashes (format.go).
//
// Commands should use a role. If a function has no role yet, add one rather
// than reaching for a palette color, so that restyling a function is a
// one-line change here instead of a search through the commands.
package style

import "github.com/charmbracelet/lipgloss"

// fg returns a style with the given ANSI color as its foreground.
func fg(color string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

// The palette. These are the 16 ANSI colors that modctl uses (the terminal's
// theme decides what they actually look like), plus the two grays.
var (
	Green        = fg("2")
	BrightGreen  = fg("10")
	Red          = fg("1")
	BrightRed    = fg("9")
	Yellow       = fg("3")
	BrightYellow = fg("11")
	Cyan         = fg("6")
	BrightCyan   = fg("14")
	BrightBlue   = fg("12")
	White        = fg("7") // light gray
	Gray         = fg("8") // "bright black"
)
