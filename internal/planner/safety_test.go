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

package planner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDestPath(t *testing.T) {
	t.Parallel()

	t.Run("valid paths", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			path string
		}{
			{
				name: "simple filename",
				path: "plugin.esp",
			},
			{
				name: "nested path",
				path: "Data/textures/rock.dds",
			},
			{
				name: "deeply nested",
				path: "Data/meshes/architecture/whiterun/wrbuildings01.nif",
			},
			{
				name: "path with dots in filename",
				path: "Data/textures/rock.01.dds",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				err := validateDestPath(tc.path)
				require.NoError(t, err)
			})
		}
	})

	t.Run("absolute paths", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			path string
		}{
			{
				name: "unix absolute",
				path: "/etc/passwd",
			},
			{
				name: "absolute with subdir",
				path: "/home/user/game/Data/plugin.esp",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				err := validateDestPath(tc.path)
				require.Error(t, err)
				assert.ErrorContains(t, err, "absolute path")
			})
		}
	})

	t.Run("path traversal", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			path string
		}{
			{
				name: "simple traversal",
				path: "../etc/passwd",
			},
			{
				name: "traversal after subdir",
				path: "Data/../../etc/passwd",
			},
			{
				name: "traversal only",
				path: "..",
			},
			{
				name: "multiple traversals",
				path: "../../etc/passwd",
			},
			{
				name: "traversal with valid prefix",
				path: "Data/../../../etc/passwd",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				err := validateDestPath(tc.path)
				require.Error(t, err)
				assert.ErrorContains(t, err, "traversal")
			})
		}
	})

	t.Run("empty or dot paths", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			path string
		}{
			{
				name: "dot only",
				path: ".",
			},
			{
				name: "empty string",
				path: "",
			},
			{
				name: "current dir slash",
				path: "./",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				err := validateDestPath(tc.path)
				require.Error(t, err)
			})
		}
	})
}
