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

package archivescanner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCarryForwardHashes(t *testing.T) {
	t.Parallel()

	const hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	cases := []struct {
		name    string
		old     []cachedHash
		entries []Entry
		want    map[int64]string
	}{
		{
			name: "matching position, type and size is kept",
			old:  []cachedHash{{Position: 1, Type: "file", Size: 100, Sha256: hashA}},
			entries: []Entry{
				{Position: 0, Type: EntryTypeDir},
				{Position: 1, Type: EntryTypeFile, SizeBytes: 100, RawPath: "a.esp"},
			},
			want: map[int64]string{1: hashA},
		},
		{
			name: "path change does not matter",
			old:  []cachedHash{{Position: 0, Type: "file", Size: 8, Sha256: hashA}},
			entries: []Entry{
				{Position: 0, Type: EntryTypeFile, SizeBytes: 8, RawPath: " Best in Party Skills.pak"},
			},
			want: map[int64]string{0: hashA},
		},
		{
			name: "size mismatch is dropped",
			old:  []cachedHash{{Position: 0, Type: "file", Size: 8, Sha256: hashA}},
			entries: []Entry{
				{Position: 0, Type: EntryTypeFile, SizeBytes: 9},
			},
			want: map[int64]string{},
		},
		{
			name: "type mismatch is dropped",
			old:  []cachedHash{{Position: 0, Type: "file", Size: 0, Sha256: hashA}},
			entries: []Entry{
				{Position: 0, Type: EntryTypeSymlink, SizeBytes: 0},
			},
			want: map[int64]string{},
		},
		{
			name: "position no longer present is dropped",
			old:  []cachedHash{{Position: 5, Type: "file", Size: 8, Sha256: hashA}},
			entries: []Entry{
				{Position: 0, Type: EntryTypeFile, SizeBytes: 8},
			},
			want: map[int64]string{},
		},
		{
			name: "each position is matched independently",
			old: []cachedHash{
				{Position: 0, Type: "file", Size: 1, Sha256: hashA},
				{Position: 1, Type: "file", Size: 2, Sha256: hashB},
			},
			entries: []Entry{
				{Position: 0, Type: EntryTypeFile, SizeBytes: 1},
				{Position: 1, Type: EntryTypeFile, SizeBytes: 99},
			},
			want: map[int64]string{0: hashA},
		},
		{
			name:    "nothing cached",
			old:     nil,
			entries: []Entry{{Position: 0, Type: EntryTypeFile, SizeBytes: 1}},
			want:    map[int64]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, carryForwardHashes(tc.old, tc.entries))
		})
	}
}
