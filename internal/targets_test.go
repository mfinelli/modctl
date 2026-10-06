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

	"github.com/mfinelli/modctl/dbq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tgt(name string, enabled bool) dbq.Target {
	var e int64
	if enabled {
		e = 1
	}
	return dbq.Target{Name: name, Enabled: e}
}

func TestEnabledTargets(t *testing.T) {
	t.Parallel()

	targets := []dbq.Target{tgt("a", true), tgt("b", false), tgt("c", true)}
	got := EnabledTargets(targets)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Name)
	assert.Equal(t, "c", got[1].Name)

	assert.Empty(t, EnabledTargets(nil))
	assert.Empty(t, EnabledTargets([]dbq.Target{tgt("a", false)}))
}

func TestEnsureTargetEnabled(t *testing.T) {
	t.Parallel()

	assert.NoError(t, EnsureTargetEnabled(tgt("game_dir", true)))

	err := EnsureTargetEnabled(tgt("proton_prefix", false))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"proton_prefix" is disabled`)
	assert.Contains(t, err.Error(), "games targets enable proton_prefix")
}

func TestResolveTarget(t *testing.T) {
	t.Parallel()

	t.Run("explicit name", func(t *testing.T) {
		t.Parallel()

		both := []dbq.Target{tgt("game_dir", true), tgt("proton_prefix", true)}
		oneOff := []dbq.Target{tgt("game_dir", false), tgt("proton_prefix", true)}

		cases := []struct {
			name      string
			targets   []dbq.Target
			requested string
			want      string
			wantErr   string
		}{
			{"enabled target", both, "proton_prefix", "proton_prefix", ""},
			{"default name asked for explicitly", both, "game_dir", "game_dir", ""},
			{"disabled target is refused", oneOff, "game_dir", "", "is disabled"},
			{"unknown target", both, "nope", "", `"nope" not found`},
			{"no targets at all", nil, "game_dir", "", "not found"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got, err := ResolveTarget(tc.targets, tc.requested)
				if tc.wantErr != "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tc.wantErr)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, tc.want, got.Name)
			})
		}
	})

	t.Run("default", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name    string
			targets []dbq.Target
			want    string
			wantErr string
		}{
			{
				name:    "game_dir wins when enabled, even with others",
				targets: []dbq.Target{tgt("game_dir", true), tgt("proton_prefix", true)},
				want:    "game_dir",
			},
			{
				name:    "game_dir only",
				targets: []dbq.Target{tgt("game_dir", true)},
				want:    "game_dir",
			},
			{
				name:    "game_dir disabled falls back to the only enabled target",
				targets: []dbq.Target{tgt("game_dir", false), tgt("proton_prefix", true)},
				want:    "proton_prefix",
			},
			{
				name:    "game_dir disabled with several others enabled is ambiguous",
				targets: []dbq.Target{tgt("game_dir", false), tgt("proton_prefix", true), tgt("docs", true)},
				wantErr: "pass --target",
			},
			{
				name:    "ambiguity error names the candidates",
				targets: []dbq.Target{tgt("game_dir", false), tgt("a", true), tgt("b", true)},
				wantErr: "a, b",
			},
			{
				name:    "nothing enabled",
				targets: []dbq.Target{tgt("game_dir", false), tgt("proton_prefix", false)},
				wantErr: "no enabled targets",
			},
			{
				name:    "no targets",
				targets: nil,
				wantErr: "no enabled targets",
			},
			{
				name:    "no game_dir at all but one target",
				targets: []dbq.Target{tgt("proton_prefix", true)},
				want:    "proton_prefix",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got, err := ResolveTarget(tc.targets, "")
				if tc.wantErr != "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tc.wantErr)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, tc.want, got.Name)
			})
		}
	})
}

func TestCheckTargetCanBeDisabled(t *testing.T) {
	t.Parallel()

	target := tgt("proton_prefix", true)

	cases := []struct {
		name    string
		usage   TargetUsage
		wantErr string
	}{
		{"unused target with another enabled", TargetUsage{EnabledTargets: 2}, ""},
		{"installed files block", TargetUsage{InstalledFiles: 3, EnabledTargets: 2}, "3 installed file(s)"},
		{"profile items block", TargetUsage{ProfileItems: 2, EnabledTargets: 2}, "2 profile item(s)"},
		{"overrides block", TargetUsage{Overrides: 1, EnabledTargets: 2}, "1 override(s)"},
		{"items and overrides are both reported", TargetUsage{ProfileItems: 2, Overrides: 1, EnabledTargets: 2}, "2 profile item(s) and 1 override(s)"},
		{"installed files are reported before anything else", TargetUsage{InstalledFiles: 1, ProfileItems: 5, EnabledTargets: 1}, "installed file(s)"},
		{"last enabled target", TargetUsage{EnabledTargets: 1}, "only enabled target"},
		{"no enabled targets counted", TargetUsage{}, "only enabled target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := CheckTargetCanBeDisabled(target, tc.usage)
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
