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

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Messages: a line that reports how something went. These use the plain
// (non-bright) colors.
var (
	Success = Green
	Failure = Red.Bold(true)
	Warning = Yellow
	Info    = Cyan
)

// Changes: markers and lines in plans, diffs and previews. These use the
// bright colors.
var (
	Added     = BrightGreen
	Removed   = BrightRed
	Changed   = BrightYellow
	Restored  = BrightCyan
	Unchanged = Gray
	Hunk      = Cyan // diff hunk headers
)

// State: the state of an item in a list or on a status screen. These use the
// bright colors, like the change markers.
var (
	Active   = BrightGreen
	Inactive = Gray
	Good     = BrightGreen
	Bad      = BrightRed
	Pending  = BrightYellow
)

// Text
var (
	Bold    = lipgloss.NewStyle().Bold(true)
	Title   = Bold
	Subtle  = lipgloss.NewStyle().Foreground(subtleColor(lipgloss.Writer.Profile)) // secondary text
	Dim     = Gray                                                                 // de-emphasized text
	Label   = White                                                                // field labels; use Label.Width(n) in aligned lists
	Header  = Bold.Foreground(lipgloss.Color("63"))
	Section = Bold.MarginTop(1)
)

// Tags and badges
var (
	ActiveTag       = BrightGreen.Bold(true) // "(active)"
	PrimaryTag      = Cyan                   // "(primary)"
	UpdateAvailable = BrightYellow.Bold(true)
	DryRun          = BrightBlue
	ContextBadge    = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("10")).
			Padding(0, 1).
			Bold(true)
)

// Containers
var (
	Card = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)
	Banner = lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("11")).
		Padding(0, 1)
)

// subtleColor is the color of secondary text for a terminal with the given
// color profile: gray 245 wherever that can be shown. A terminal limited to 16
// colors would be given white for it by the conversion lipgloss does, which
// makes secondary text as prominent as the text around it, so there it is asked
// for by name instead: bright black, which is the nearest gray it has.
func subtleColor(p colorprofile.Profile) color.Color {
	if p == colorprofile.ANSI {
		return lipgloss.BrightBlack
	}
	return lipgloss.Color("245")
}

// ActiveDot and InactiveDot are the dots that mark an active or inactive item.
// They are functions so that they render when called, not when the program
// starts.
func ActiveDot() string   { return Active.Render("●") }
func InactiveDot() string { return Inactive.Render("○") }
