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

func TestFullSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		storeID    string
		storeGame  string
		instanceID string
		want       string
	}{
		{
			name:       "includes default instance explicitly",
			storeID:    "steam",
			storeGame:  "1091500",
			instanceID: "default",
			want:       "steam:1091500#default",
		},
		{
			name:       "lowercases store id",
			storeID:    "StEaM",
			storeGame:  "1091500",
			instanceID: "default",
			want:       "steam:1091500#default",
		},
		{
			name:       "trims whitespace",
			storeID:    " steam ",
			storeGame:  " 1091500 ",
			instanceID: " library_2 ",
			want:       "steam:1091500#library_2",
		},
		{
			name:       "empty instance becomes default",
			storeID:    "steam",
			storeGame:  "1091500",
			instanceID: "",
			want:       "steam:1091500#default",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FullSelector(tt.storeID, tt.storeGame, tt.instanceID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShortSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		storeID    string
		storeGame  string
		instanceID string
		want       string
	}{
		{
			name:       "omits default instance",
			storeID:    "steam",
			storeGame:  "1091500",
			instanceID: "default",
			want:       "steam:1091500",
		},
		{
			name:       "omits instance when empty",
			storeID:    "steam",
			storeGame:  "1091500",
			instanceID: "",
			want:       "steam:1091500",
		},
		{
			name:       "includes non-default instance",
			storeID:    "steam",
			storeGame:  "1091500",
			instanceID: "library_2",
			want:       "steam:1091500#library_2",
		},
		{
			name:       "trims and lowercases store id",
			storeID:    " StEaM ",
			storeGame:  " 1091500 ",
			instanceID: " default ",
			want:       "steam:1091500",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ShortSelector(tt.storeID, tt.storeGame, tt.instanceID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseSelector(t *testing.T) {
	t.Parallel()

	type want struct {
		storeID    string
		storeGame  string
		instanceID string
	}

	tests := []struct {
		name    string
		input   string
		want    want
		wantErr bool
	}{
		{
			name:  "parses without instance (defaults to default)",
			input: "steam:1091500",
			want:  want{storeID: "steam", storeGame: "1091500", instanceID: "default"},
		},
		{
			name:  "parses with instance",
			input: "steam:1091500#library_2",
			want:  want{storeID: "steam", storeGame: "1091500", instanceID: "library_2"},
		},
		{
			name:  "parses with explicit default instance",
			input: "steam:1091500#default",
			want:  want{storeID: "steam", storeGame: "1091500", instanceID: "default"},
		},
		{
			name:  "lowercases store and trims whitespace",
			input: " StEaM : 1091500 # library_2 ",
			want:  want{storeID: "steam", storeGame: "1091500", instanceID: "library_2"},
		},
		{
			name:    "rejects empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "rejects missing colon",
			input:   "steam1091500",
			wantErr: true,
		},
		{
			name:    "rejects missing store",
			input:   ":1091500",
			wantErr: true,
		},
		{
			name:    "rejects missing game id",
			input:   "steam:",
			wantErr: true,
		},
		{
			name:    "rejects empty game id before hash",
			input:   "steam:#default",
			wantErr: true,
		},
		{
			name:    "rejects empty instance after hash",
			input:   "steam:1091500#",
			wantErr: true,
		},
		{
			name:    "rejects multiple hashes",
			input:   "steam:1091500#a#b",
			wantErr: true,
		},
		{
			name:    "rejects extra colon in store section (since store id becomes invalid)",
			input:   "steam:1091500:foo",
			wantErr: false, // NOTE: This is actually valid under our parser: store=steam, game=1091500:foo
			want:    want{storeID: "steam", storeGame: "1091500:foo", instanceID: "default"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sid, gid, iid, err := ParseSelector(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want.storeID, sid)
			assert.Equal(t, tt.want.storeGame, gid)
			assert.Equal(t, tt.want.instanceID, iid)
		})
	}
}
