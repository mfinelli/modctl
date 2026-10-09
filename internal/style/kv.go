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

import "strings"

// KV lays out label/value lines so that the values line up:
//
//	ID:        3
//	Store:     steam
//
// Each line is Indent spaces, the label padded to Width, a space and then the
// value. Use one KV per group of lines and write all of them through it, so
// that they stay aligned. Width must be at least the length of the longest
// label: lipgloss wraps a label that is wider.
type KV struct {
	Indent int
	Width  int

	// Dim renders the whole line dimmed, for items that are inactive.
	Dim bool
}

// Line returns the line for a label and value, including the newline.
func (kv KV) Line(label, value string) string {
	line := strings.Repeat(" ", kv.Indent) +
		Label.Width(kv.Width).Render(label) + " " + value
	if kv.Dim {
		// style the line itself and add the newline afterwards: rendering a
		// string that ends in a newline makes lipgloss pad the empty last
		// line with spaces and leaves the output without a newline
		line = Inactive.Render(line)
	}
	return line + "\n"
}

// Write adds the line for a label and value to b.
func (kv KV) Write(b *strings.Builder, label, value string) {
	b.WriteString(kv.Line(label, value))
}

// Print prints the line for a label and value.
func (kv KV) Print(label, value string) {
	Print(kv.Line(label, value))
}
