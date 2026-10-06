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
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestNotesTextFromArg(t *testing.T) {
	t.Parallel()

	t.Run("literal argument", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			arg  string
			want string
		}{
			{"plain text", "see https://example.com/mod", "see https://example.com/mod"},
			{"kept exactly as given", "  spaced  \n\tand tabbed ", "  spaced  \n\tand tabbed "},
			{"trailing newline is kept for literals", "text\n", "text\n"},
			{"dash inside text is not stdin", "-not stdin", "-not stdin"},
			{"empty", "", ""},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				// stdin must not be touched for a literal argument
				got, err := NotesTextFromArg(tc.arg, errReader{})
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			})
		}
	})

	t.Run("dash reads stdin", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name  string
			stdin string
			want  string
		}{
			{"single line", "https://example.com\n", "https://example.com"},
			{"no trailing newline", "https://example.com", "https://example.com"},
			{"several trailing newlines", "text\n\n\n", "text"},
			{"multi-line keeps interior newlines", "line one\n\nline three\n", "line one\n\nline three"},
			{"leading and trailing spaces kept", "  indented \n", "  indented "},
			{"empty stdin", "", ""},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got, err := NotesTextFromArg("-", strings.NewReader(tc.stdin))
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			})
		}
	})

	t.Run("stdin read error", func(t *testing.T) {
		t.Parallel()
		_, err := NotesTextFromArg("-", errReader{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read from stdin")
	})
}

func TestFormatNotesLines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		notes string
		want  string
	}{
		{"single line stays on the label", "https://example.com", "notes: https://example.com"},
		{"multi-line is indented below the label", "first\nsecond", "notes:\n  first\n  second"},
		{"blank interior line is preserved", "a\n\nb", "notes:\n  a\n  \n  b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, FormatNotesLines(tc.notes))
		})
	}
}
