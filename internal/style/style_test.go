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
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestPalette(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		style lipgloss.Style
		color string
	}{
		{"Green", Green, "2"},
		{"BrightGreen", BrightGreen, "10"},
		{"Red", Red, "1"},
		{"BrightRed", BrightRed, "9"},
		{"Yellow", Yellow, "3"},
		{"BrightYellow", BrightYellow, "11"},
		{"Cyan", Cyan, "6"},
		{"BrightCyan", BrightCyan, "14"},
		{"BrightBlue", BrightBlue, "12"},
		{"White", White, "7"},
		{"Gray", Gray, "8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, lipgloss.Color(tc.color), tc.style.GetForeground())
			assert.False(t, tc.style.GetBold(), "palette colors are not bold")
		})
	}
}

// TestRoles pins what each role looks like. Restyling a role is meant to be a
// one-line change in roles.go, and this is the list to update when it is.
func TestRoles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		style lipgloss.Style
		color string
		bold  bool
	}{
		// messages: plain colors
		{"Success", Success, "2", false},
		{"Failure", Failure, "1", true},
		{"Warning", Warning, "3", false},
		{"Info", Info, "6", false},

		// changes, state: bright colors
		{"Added", Added, "10", false},
		{"Removed", Removed, "9", false},
		{"Changed", Changed, "11", false},
		{"Restored", Restored, "14", false},
		{"Unchanged", Unchanged, "8", false},
		{"Hunk", Hunk, "6", false},
		{"Active", Active, "10", false},
		{"Inactive", Inactive, "8", false},
		{"Good", Good, "10", false},
		{"Bad", Bad, "9", false},
		{"Pending", Pending, "11", false},

		// text
		{"Subtle", Subtle, "245", false},
		{"Dim", Dim, "8", false},
		{"Label", Label, "7", false},
		{"Header", Header, "63", true},

		// tags
		{"ActiveTag", ActiveTag, "10", true},
		{"PrimaryTag", PrimaryTag, "6", false},
		{"UpdateAvailable", UpdateAvailable, "11", true},
		{"DryRun", DryRun, "12", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, lipgloss.Color(tc.color), tc.style.GetForeground())
			assert.Equal(t, tc.bold, tc.style.GetBold())
		})
	}

	t.Run("text with no color", func(t *testing.T) {
		t.Parallel()
		assert.True(t, Bold.GetBold())
		assert.True(t, Title.GetBold())
		assert.True(t, Section.GetBold())
		assert.Equal(t, 1, Section.GetMarginTop())
	})

	t.Run("Label takes a width", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 16, Label.Width(16).GetWidth())
		assert.Zero(t, Label.GetWidth(), "setting a width on a copy must not change the role")
	})

	t.Run("Card", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, lipgloss.RoundedBorder(), Card.GetBorderStyle())
		assert.Equal(t, 1, Card.GetPaddingLeft())
		assert.Equal(t, 1, Card.GetPaddingRight())
		assert.Zero(t, Card.GetPaddingTop())
	})

	t.Run("Banner", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, lipgloss.NormalBorder(), Banner.GetBorderStyle())
		assert.Equal(t, lipgloss.Color("11"), Banner.GetForeground())
		assert.Equal(t, lipgloss.Color("11"), Banner.GetBorderTopForeground())
	})

	t.Run("ContextBadge", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, lipgloss.Color("0"), ContextBadge.GetForeground())
		assert.Equal(t, lipgloss.Color("10"), ContextBadge.GetBackground())
		assert.True(t, ContextBadge.GetBold())
	})
}

func TestDots(t *testing.T) {
	t.Parallel()

	// the colors may or may not be rendered depending on the terminal the
	// tests run in, but the glyph is always there
	assert.True(t, strings.Contains(ActiveDot(), "●"))
	assert.True(t, strings.Contains(InactiveDot(), "○"))
}
