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

package fsutil

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsUnderDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		dir  string
		want bool
	}{
		{"same directory", "/foo/bar", "/foo/bar", true},
		{"direct child", "/foo/bar/baz", "/foo/bar", true},
		{"nested child", "/foo/bar/a/b/c", "/foo/bar", true},
		{"trailing separator on dir", "/foo/bar/baz", "/foo/bar/", true},
		{"parent of dir", "/foo", "/foo/bar", false},
		{"sibling", "/foo/baz", "/foo/bar", false},
		{"sibling sharing a name prefix", "/foo/bar-baz", "/foo/bar", false},
		{"child whose name starts with dots", "/foo/bar/..baz", "/foo/bar", true},
		{"traversal out of dir", "/foo/bar/../baz", "/foo/bar", false},
		{"traversal that stays inside dir", "/foo/bar/a/../b", "/foo/bar", true},
		{"unrelated tree", "/other/place", "/foo/bar", false},
		{"relative path inside relative dir", "a/b", "a", true},
		{"relative path outside relative dir", "b", "a", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := IsUnderDir(filepath.FromSlash(tc.path), filepath.FromSlash(tc.dir))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("relative path is resolved against the working directory", func(t *testing.T) {
		t.Parallel()

		abs, err := filepath.Abs("child")
		require.NoError(t, err)

		got, err := IsUnderDir(abs, ".")
		require.NoError(t, err)
		assert.True(t, got)
	})
}
