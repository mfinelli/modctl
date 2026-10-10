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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBufferSizeFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int64
		want int
	}{
		{"empty file gets the minimum", 0, minBufferSize},
		{"tiny file gets the minimum", 10, minBufferSize},
		{"just under the minimum", minBufferSize - 1, minBufferSize},
		{"exactly the minimum", minBufferSize, minBufferSize},
		{"between the limits uses the file size", 300 * 1024, 300 * 1024},
		{"exactly the maximum", maxBufferSize, maxBufferSize},
		{"just over the maximum", maxBufferSize + 1, maxBufferSize},
		{"huge file gets the maximum", 10 << 30, maxBufferSize},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, bufferSizeFor(tc.size))
		})
	}
}
