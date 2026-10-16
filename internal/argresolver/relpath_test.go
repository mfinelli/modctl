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

package argresolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelpath(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name string
		arg  string
		want string
	}{
		{"a file", "plugin.esp", "plugin.esp"},
		{"a file in a directory", "Data/textures/rock.dds", "Data/textures/rock.dds"},
		{"a leading dot segment", "./Data/plugin.esp", "Data/plugin.esp"},
		{"a doubled slash", "Data//plugin.esp", "Data/plugin.esp"},
		{"a trailing slash", "Data/", "Data"},
		{"a traversal that stays inside", "Data/../plugin.esp", "plugin.esp"},
		{"a name that only starts with two dots", "..hidden/plugin.esp", "..hidden/plugin.esp"},
	}

	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := Relpath(tc.arg)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	invalid := []struct {
		name   string
		arg    string
		reason string
	}{
		{"an absolute path", "/etc/passwd", "absolute path not allowed"},
		{"an absolute path that goes up", "/../etc/passwd", "absolute path not allowed"},
		{"one level up", "../escape.cfg", "path traversal not allowed"},
		{"up by way of a directory", "Data/../../escape.cfg", "path traversal not allowed"},
		{"only up", "..", "path traversal not allowed"},
		{"up, hidden by a dot segment", "./../escape.cfg", "path traversal not allowed"},
		{"the root itself", ".", "empty or dot path not allowed"},
		{"nothing", "", "empty or dot path not allowed"},
		{"a directory and back out of it", "Data/..", "empty or dot path not allowed"},
	}

	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := Relpath(tc.arg)

			require.Error(t, err)
			assert.Empty(t, got)
			assert.Contains(t, err.Error(), tc.reason)
			assert.Contains(t, err.Error(), `invalid path "`+tc.arg+`"`, "says what was typed, not what it was turned into")
		})
	}
}
