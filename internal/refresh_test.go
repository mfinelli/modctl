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

func TestClassifyInstalls(t *testing.T) {
	t.Parallel()

	known := func(game, instance, name string, present bool) knownInstall {
		return knownInstall{game, instance, name, present}
	}
	found := func(game, instance, name string) foundInstall {
		return foundInstall{game, instance, name}
	}

	cases := []struct {
		name  string
		known []knownInstall
		found []foundInstall
		want  []RefreshChange
	}{
		{
			name: "nothing known, nothing found",
		},
		{
			name:  "a first sighting is new",
			found: []foundInstall{found("1", "default", "Alpha")},
			want:  []RefreshChange{{"Alpha", RefreshNew}},
		},
		{
			name:  "a known install that is present is updated",
			known: []knownInstall{known("1", "default", "Alpha", true)},
			found: []foundInstall{found("1", "default", "Alpha")},
			want:  []RefreshChange{{"Alpha", RefreshUpdated}},
		},
		{
			name:  "a known install that was missing has returned",
			known: []knownInstall{known("1", "default", "Alpha", false)},
			found: []foundInstall{found("1", "default", "Alpha")},
			want:  []RefreshChange{{"Alpha", RefreshReturned}},
		},
		{
			name:  "a present install that is not found is missing",
			known: []knownInstall{known("1", "default", "Alpha", true)},
			want:  []RefreshChange{{"Alpha", RefreshMissing}},
		},
		{
			name:  "an install that was already missing is not reported again",
			known: []knownInstall{known("1", "default", "Alpha", false)},
			want:  nil,
		},
		{
			name: "another instance of the same game is a different install",
			known: []knownInstall{
				known("1", "default", "Alpha", true),
			},
			found: []foundInstall{
				found("1", "default", "Alpha"),
				found("1", "library-2", "Alpha"),
			},
			want: []RefreshChange{{"Alpha", RefreshUpdated}, {"Alpha", RefreshNew}},
		},
		{
			name: "missing installs come first and by name, then installs in the order found",
			known: []knownInstall{
				known("3", "default", "Zulu", true),
				known("1", "default", "Alpha", false),
				known("2", "default", "Mike", true),
				known("4", "default", "Bravo", true),
			},
			found: []foundInstall{
				found("5", "default", "Echo"),
				found("1", "default", "Alpha"),
				found("4", "default", "Bravo"),
			},
			want: []RefreshChange{
				{"Mike", RefreshMissing},
				{"Zulu", RefreshMissing},
				{"Echo", RefreshNew},
				{"Alpha", RefreshReturned},
				{"Bravo", RefreshUpdated},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := classifyInstalls(tc.known, tc.found)
			if len(tc.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRefreshResultAddChanges(t *testing.T) {
	t.Parallel()

	var r RefreshResult
	r.addChanges([]RefreshChange{
		{"Gone", RefreshMissing},
		{"Fresh", RefreshNew},
		{"Back", RefreshReturned},
		{"Same", RefreshUpdated},
		{"Fresher", RefreshNew},
	})

	assert.Equal(t, []string{"Fresh", "Fresher"}, r.New)
	assert.Equal(t, []string{"Back"}, r.Returned)
	assert.Equal(t, []string{"Same"}, r.Updated)
	assert.Equal(t, []string{"Gone"}, r.Missing)
	assert.Len(t, r.Changes, 5, "the order of Changes is kept for display")
	assert.Equal(t, "Gone", r.Changes[0].Name)
}
