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
	"bytes"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/assert"
)

// These tests change the process-wide writer and environment, so they don't run
// in parallel.

// captureStdout points lipgloss's standard output writer at a buffer with the
// given color profile, for the rest of the test.
func captureStdout(t *testing.T, p colorprofile.Profile) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	old := lipgloss.Writer
	lipgloss.Writer = &colorprofile.Writer{Forward: &buf, Profile: p}
	t.Cleanup(func() { lipgloss.Writer = old })
	return &buf
}

func TestPrintToStdout(t *testing.T) {
	styled := Warning.Render("careful")

	t.Run("a pipe gets plain text", func(t *testing.T) {
		buf := captureStdout(t, colorprofile.NoTTY)
		Println("a", styled, "b")
		Printf("%s and %d\n", styled, 3)
		Print(styled, "\n")

		assert.Equal(t, "a careful b\ncareful and 3\ncareful\n", buf.String())
		assert.NotContains(t, buf.String(), "\x1b")
	})

	t.Run("a true color terminal gets the colors", func(t *testing.T) {
		buf := captureStdout(t, colorprofile.TrueColor)
		Println(styled)

		assert.Contains(t, buf.String(), "\x1b[")
		assert.Contains(t, buf.String(), "careful")
	})

	t.Run("a 16 color terminal gets the colors it can show", func(t *testing.T) {
		buf := captureStdout(t, colorprofile.ANSI)
		Println(Subtle.Render("quiet"))

		assert.Contains(t, buf.String(), "\x1b[")
		assert.NotContains(t, buf.String(), "38;5;245", "256 color codes are converted")
		assert.Contains(t, buf.String(), "quiet")
	})

	t.Run("text without styling is left alone, including carriage returns", func(t *testing.T) {
		buf := captureStdout(t, colorprofile.NoTTY)
		Printf("  verifying (%d/%d)", 1, 3)
		Printf("\r  verifying (%d/%d)", 2, 3)
		Print("\n")

		assert.Equal(t, "  verifying (1/3)\r  verifying (2/3)\n", buf.String())
	})
}

func TestKVPrintGoesThroughTheWriter(t *testing.T) {
	buf := captureStdout(t, colorprofile.NoTTY)

	KV{Indent: 2, Width: 8}.Print("name:", "value")

	// the label is styled, so if this had gone straight to the terminal the
	// escape sequences would be in the output
	assert.Equal(t, "  name:    value\n", buf.String())
	assert.NotContains(t, buf.String(), "\x1b")
}

func TestFprintToAWriter(t *testing.T) {
	styled := Failure.Render("broken")

	t.Run("a writer that is not a terminal gets plain text", func(t *testing.T) {
		t.Setenv("TTY_FORCE", "0")
		t.Setenv("NO_COLOR", "")

		var buf bytes.Buffer
		Fprintln(&buf, styled)
		Fprintf(&buf, "%s!\n", styled)
		Fprint(&buf, styled, "\n")

		assert.Equal(t, "broken\nbroken!\nbroken\n", buf.String())
	})

	t.Run("a writer that is a color terminal gets the colors", func(t *testing.T) {
		t.Setenv("TTY_FORCE", "1")
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("COLORTERM", "truecolor")
		t.Setenv("NO_COLOR", "")

		var buf bytes.Buffer
		Fprintln(&buf, styled)

		assert.Contains(t, buf.String(), "\x1b[")
		assert.True(t, strings.Contains(buf.String(), "broken"))
	})

	t.Run("NO_COLOR removes the colors", func(t *testing.T) {
		t.Setenv("TTY_FORCE", "1")
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("COLORTERM", "truecolor")
		t.Setenv("NO_COLOR", "1")

		var buf bytes.Buffer
		Fprintln(&buf, Warning.Render("careful"))

		assert.NotContains(t, buf.String(), "38;", "no color sequences")
		assert.NotContains(t, buf.String(), "\x1b[33m")
		assert.Contains(t, buf.String(), "careful")
	})
}
