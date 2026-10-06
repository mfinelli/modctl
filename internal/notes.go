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

import (
	"fmt"
	"io"
	"strings"
)

// NotesTextFromArg returns the notes text given as a command argument. If the
// argument is "-" the text is read from stdin instead. Trailing newlines are
// trimmed so that piped input doesn't store them; everything else, including
// interior newlines and surrounding spaces, is kept exactly as given.
func NotesTextFromArg(arg string, stdin io.Reader) (string, error) {
	if arg != "-" {
		return arg, nil
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read from stdin: %w", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// FormatNotesLines renders freeform notes for display under a "notes:" label.
// A single line stays on the label's line; multi-line notes start on the next
// line, indented, so they read as one block.
func FormatNotesLines(notes string) string {
	if !strings.Contains(notes, "\n") {
		return "notes: " + notes
	}
	return "notes:\n  " + strings.ReplaceAll(notes, "\n", "\n  ")
}
