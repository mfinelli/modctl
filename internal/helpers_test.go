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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantVal int64
		wantOK  bool
	}{
		{
			name:    "valid positive number",
			input:   "42",
			wantVal: 42,
			wantOK:  true,
		},
		{
			name:    "valid negative number",
			input:   "-7",
			wantVal: -7,
			wantOK:  true,
		},
		{
			name:    "zero",
			input:   "0",
			wantVal: 0,
			wantOK:  true,
		},
		{
			name:    "trims whitespace",
			input:   "  123  ",
			wantVal: 123,
			wantOK:  true,
		},
		{
			name:   "empty string",
			input:  "",
			wantOK: false,
		},
		{
			name:   "whitespace only",
			input:  "   ",
			wantOK: false,
		},
		{
			name:   "non-numeric",
			input:  "abc",
			wantOK: false,
		},
		{
			name:   "mixed numeric and text",
			input:  "123abc",
			wantOK: false,
		},
		{
			name:   "float value",
			input:  "3.14",
			wantOK: false,
		},
		{
			name:   "overflow",
			input:  "9223372036854775808", // int64 max + 1
			wantOK: false,
		},
		{
			name:   "hex not allowed",
			input:  "0x10",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotVal, gotOK := ParseInt64(tt.input)

			assert.Equal(t, tt.wantOK, gotOK)

			if tt.wantOK {
				assert.Equal(t, tt.wantVal, gotVal)
			}
		})
	}
}
