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

package completion

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLikePrefixPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain string",
			input:    "steam",
			expected: "steam%",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "%",
		},
		{
			name:     "percent sign escaped",
			input:    "100%",
			expected: `100\%%`,
		},
		{
			name:     "underscore escaped",
			input:    "my_game",
			expected: `my\_game%`,
		},
		{
			name:     "backslash escaped",
			input:    `back\slash`,
			expected: `back\\slash%`,
		},
		{
			name:     "multiple special characters",
			input:    `50%_off\sale`,
			expected: `50\%\_off\\sale%`,
		},
		{
			name:     "selector prefix",
			input:    "steam:109",
			expected: "steam:109%",
		},
		{
			name:     "non-ascii game name",
			input:    "モンスターハンター", // monster hunter
			expected: "モンスターハンター%",
		},
		{
			name:     "game name with colon",
			input:    "My Game: the sequel",
			expected: "My Game: the sequel%",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, likePrefixPattern(tc.input))
		})
	}
}
