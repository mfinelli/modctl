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

	"github.com/stretchr/testify/assert"
)

func TestTable(t *testing.T) {
	t.Parallel()

	out := Table(
		[]string{" ID ", " Name "},
		[][]string{{" 1 ", " first "}, {" 2 ", " second "}},
	)

	for _, want := range []string{"ID", "Name", "first", "second"} {
		assert.Contains(t, out, want)
	}
	// a header row, a separator, then one line per row, all inside a border
	assert.Equal(t, 6, len(strings.Split(out, "\n")))

	// the border is rounded, not square
	assert.Contains(t, out, "╭")
	assert.Contains(t, out, "╯")
	assert.NotContains(t, out, "┌")
}

func TestTableNoRows(t *testing.T) {
	t.Parallel()

	out := Table([]string{" ID "}, [][]string{})
	assert.Contains(t, out, "ID")
}
