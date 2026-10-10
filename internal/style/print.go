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
	"io"

	"charm.land/lipgloss/v2"
)

// Everything modctl shows to the user goes through these functions instead of
// the ones in fmt.
//
// A style renders to a string that always contains the full ANSI escape
// sequences for its colors. It is the writer that decides what the output can
// show: when it is not a terminal (a pipe, a file) or the user has set
// NO_COLOR, the sequences are removed, and when the terminal supports fewer
// colors they are converted to the closest ones it does support. Printing a
// styled string with fmt would skip that and put the raw sequences into a pipe.
//
// Print, Printf and Println write to standard output, and the Fprint functions
// to any writer, such as os.Stderr, which is checked for color support on its
// own. Text with no styling in it passes through unchanged.

// Print prints to standard output, like fmt.Print.
func Print(a ...any) (int, error) { return lipgloss.Print(a...) }

// Printf prints formatted text to standard output, like fmt.Printf.
func Printf(format string, a ...any) (int, error) { return lipgloss.Printf(format, a...) }

// Println prints to standard output followed by a newline, like fmt.Println.
func Println(a ...any) (int, error) { return lipgloss.Println(a...) }

// Fprint prints to w, like fmt.Fprint.
func Fprint(w io.Writer, a ...any) (int, error) { return lipgloss.Fprint(w, a...) }

// Fprintf prints formatted text to w, like fmt.Fprintf.
func Fprintf(w io.Writer, format string, a ...any) (int, error) {
	return lipgloss.Fprintf(w, format, a...)
}

// Fprintln prints to w followed by a newline, like fmt.Fprintln.
func Fprintln(w io.Writer, a ...any) (int, error) { return lipgloss.Fprintln(w, a...) }
