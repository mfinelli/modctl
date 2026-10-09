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

func TestKVLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		kv    KV
		label string
		value string
		want  string
	}{
		{"indented and padded", KV{Indent: 2, Width: 12}, "ID:", "3", "  ID:          3\n"},
		{"deeper indent", KV{Indent: 6, Width: 12}, "path:", "/g", "      path:        /g\n"},
		{"wider label column", KV{Indent: 2, Width: 16}, "size:", "1.0 KB", "  size:            1.0 KB\n"},
		{"no indent", KV{Width: 8}, "a:", "b", "a:       b\n"},
		{"empty label keeps the column", KV{Indent: 2, Width: 8}, "", "continued", "           continued\n"},
		{"label exactly as wide as the column", KV{Indent: 0, Width: 4}, "abcd", "v", "abcd v\n"},
		{"empty value", KV{Indent: 2, Width: 6}, "x:", "", "  x:     \n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.kv.Line(tc.label, tc.value))
		})
	}
}

func TestKVLinesAlign(t *testing.T) {
	t.Parallel()

	kv := KV{Indent: 2, Width: 10}
	var b strings.Builder
	kv.Write(&b, "a:", "one")
	kv.Write(&b, "longer:", "two")
	kv.Write(&b, "x:", "three")

	// the values all start in the same column
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	assert.Len(t, lines, 3)
	for i, v := range []string{"one", "two", "three"} {
		assert.Equal(t, 2+10+1, strings.Index(lines[i], v), "line %d", i)
	}
}

func TestKVDim(t *testing.T) {
	t.Parallel()

	dim := KV{Indent: 6, Width: 12, Dim: true}

	// whatever the terminal renders (colors or not), the dimmed line is the
	// line styled as a whole, followed by a plain newline and nothing else
	want := Inactive.Render("      "+Label.Width(12).Render("path:")+" /g") + "\n"
	assert.Equal(t, want, dim.Line("path:", "/g"))
	assert.True(t, strings.HasSuffix(dim.Line("path:", "/g"), "\n"))
	assert.False(t, strings.HasSuffix(strings.TrimSuffix(dim.Line("path:", "/g"), "\n"), " "),
		"no trailing padding after the newline")
}
