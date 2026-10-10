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

package updatechain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func imported(ids ...int64) map[int64]struct{} {
	set := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

func TestChainHead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		updates []Update
		from    int64
		want    int64
	}{
		{"no updates at all", nil, 1, 1},
		{"a file that nothing replaced", []Update{{10, 11}}, 5, 5},
		{"one update", []Update{{1, 2}}, 1, 2},
		{"several updates in a row", []Update{{1, 2}, {2, 3}, {3, 4}}, 1, 4},
		{"from the middle of the chain", []Update{{1, 2}, {2, 3}, {3, 4}}, 2, 4},
		{"from the head", []Update{{1, 2}, {2, 3}}, 3, 3},
		{"links given out of order", []Update{{3, 4}, {1, 2}, {2, 3}}, 1, 4},
		{"two separate chains", []Update{{1, 2}, {20, 21}, {21, 22}}, 20, 22},
		{"the other chain is not mixed in", []Update{{1, 2}, {20, 21}, {21, 22}}, 1, 2},
		{"the last link wins for a file replaced twice", []Update{{1, 2}, {1, 3}}, 1, 3},
		{"a file that replaces itself", []Update{{1, 1}}, 1, 1},
		{"a loop ends where it closes", []Update{{1, 2}, {2, 1}}, 1, 1},
		{"a loop entered from outside ends where it closes", []Update{{9, 1}, {1, 2}, {2, 1}}, 9, 1},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, New(tc.updates).Head(tc.from))
		})
	}

	t.Run("the zero chain has no updates", func(t *testing.T) {
		t.Parallel()

		var c Chain
		assert.Equal(t, int64(7), c.Head(7))
		assert.False(t, c.HasSuccessor(7))
		assert.Equal(t, Status{State: UpToDate, Head: 7}, c.Status(7, nil))
	})
}

func TestChainHasSuccessor(t *testing.T) {
	t.Parallel()

	c := New([]Update{{1, 2}, {2, 3}})

	assert.True(t, c.HasSuccessor(1))
	assert.True(t, c.HasSuccessor(2))
	assert.False(t, c.HasSuccessor(3), "the head has none")
	assert.False(t, c.HasSuccessor(99), "nor does a file the chain doesn't know")
}

func TestChainStatus(t *testing.T) {
	t.Parallel()

	chain := New([]Update{{1, 2}, {2, 3}})

	tests := []struct {
		name     string
		file     int64
		imported map[int64]struct{}
		want     Status
	}{
		{"the head is up to date", 3, imported(3), Status{UpToDate, 3}},
		{"the head is up to date whatever else is imported", 3, imported(1, 2, 3), Status{UpToDate, 3}},
		{"a file the chain doesn't know is up to date", 99, imported(99), Status{UpToDate, 99}},
		{"replaced by a newer file that is not imported", 1, imported(1), Status{UpdateAvailable, 3}},
		{"replaced by a newer file that is imported", 1, imported(1, 3), Status{Superseded, 3}},
		{"only a file in between is imported", 1, imported(1, 2), Status{UpdateAvailable, 3}},
		{"the one before the head, head not imported", 2, imported(1, 2), Status{UpdateAvailable, 3}},
		{"the one before the head, head imported", 2, imported(2, 3), Status{Superseded, 3}},
		{"nothing at all is imported", 1, nil, Status{UpdateAvailable, 3}},
		{"the file itself does not have to be imported", 1, imported(3), Status{Superseded, 3}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, chain.Status(tc.file, tc.imported))
		})
	}

	t.Run("two chains are told apart", func(t *testing.T) {
		t.Parallel()

		// a main file and an optional file, each replaced on its own
		c := New([]Update{{1, 2}, {10, 11}})
		have := imported(1, 2, 10)

		assert.Equal(t, Status{Superseded, 2}, c.Status(1, have), "the main file has its update")
		assert.Equal(t, Status{UpdateAvailable, 11}, c.Status(10, have), "the optional file doesn't")
	})
}
