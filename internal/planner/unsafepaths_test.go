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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var unsafePaths = []struct {
	name string
	path string
}{
	{"one level up", "../escape.cfg"},
	{"further up, by way of a directory", "a/../../escape.cfg"},
	{"up to the root and down", "../../../../etc/escape.cfg"},
	{"an absolute path", "/abs/path.cfg"},
	{"the directory itself", "."},
}

func TestBuildApplyPlanRejectsUnsafeOverridePaths(t *testing.T) {
	t.Parallel()

	for _, tc := range unsafePaths {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newPlanFixture(t)
			f.fullFileOverride(t, tc.path)

			plan := f.buildApply(t, false)

			assert.Empty(t, plan.Ops, "nothing is written to it")
			assert.Empty(t, plan.Files, "and it is not a file of the plan")
			rejected := warningsContaining(plan, "rejected override")
			require.Len(t, rejected, 1)
			assert.Contains(t, rejected[0], fmt.Sprintf("%q", tc.path))
		})
	}

	t.Run("the reason is in the warning", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.fullFileOverride(t, "../escape.cfg")
		f.fullFileOverride(t, "/abs/path.cfg")

		plan := f.buildApply(t, false)

		rejected := warningsContaining(plan, "rejected override")
		require.Len(t, rejected, 2)
		assert.Contains(t, rejected[0]+rejected[1], "path traversal not allowed")
		assert.Contains(t, rejected[0]+rejected[1], "absolute path not allowed")
	})

	t.Run("the overrides that are fine are still written", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.fullFileOverride(t, "../escape.cfg")
		f.fullFileOverride(t, "settings/user.cfg")
		f.fullFileOverride(t, "Data/../Data/other.cfg") // goes down and comes back, which stays inside

		plan := f.buildApply(t, false)

		assert.ElementsMatch(t, []string{"settings/user.cfg", "Data/../Data/other.cfg"}, destPaths(plan.Ops))
		assert.Len(t, warningsContaining(plan, "rejected override"), 1)
	})

	t.Run("an override that is rejected does not take the file of a mod away", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{paths: []string{"a.dll"}})
		f.fullFileOverride(t, "../a.dll")

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"a.dll"}, destPaths(plan.Ops))
		op := opFor(t, plan, "a.dll")
		assert.False(t, op.OverrideID.Valid, "it is the mod's file")
	})

	t.Run("a patch that is rejected does not ask for its archive", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.patchOverride(t, "../settings.ini", "ini_patch", f.archive(t), "settings.ini")

		plan := f.buildApply(t, false)

		assert.Empty(t, plan.Ops)
		assert.Empty(t, plan.PatchBaseArchives)
		assert.Len(t, warningsContaining(plan, "rejected override"), 1)
	})
}

func TestPlansLeaveUnsafeInstalledPathsAlone(t *testing.T) {
	t.Parallel()

	builders := []struct {
		name  string
		build func(t *testing.T, f planFixture) Plan
	}{
		{"apply", func(t *testing.T, f planFixture) Plan { return f.buildApply(t, false) }},
		{"unapply", func(t *testing.T, f planFixture) Plan {
			plan, err := BuildUnapplyPlan(context.Background(), f.q, f.gameID, f.target)
			require.NoError(t, err)
			return plan
		}},
	}

	for _, b := range builders {
		b := b
		for _, tc := range unsafePaths {
			tc := tc
			t.Run(b.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				f := newPlanFixture(t)
				// there is a file there, outside of the target, which is what a
				// removal would take away
				outside := filepath.Join(f.root, "..", "escape.cfg")
				require.NoError(t, os.WriteFile(outside, []byte("not ours"), 0o644))
				f.installed(t, tc.path)

				plan := b.build(t, f)

				assert.Empty(t, plan.Ops)
				rejected := warningsContaining(plan, "rejected installed file")
				require.Len(t, rejected, 1)
				assert.Contains(t, rejected[0], fmt.Sprintf("%q", tc.path))
				assert.FileExists(t, outside)
			})
		}

		t.Run(b.name+"/a backup is not put back there either", func(t *testing.T) {
			t.Parallel()

			f := newPlanFixture(t)
			f.installed(t, "../escape.cfg")
			f.backedUp(t, "../escape.cfg")

			plan := b.build(t, f)

			assert.Empty(t, plan.Ops)
			assert.Len(t, warningsContaining(plan, "rejected installed file"), 1)
		})

		t.Run(b.name+"/the files that are fine are still removed", func(t *testing.T) {
			t.Parallel()

			f := newPlanFixture(t)
			f.onDisk(t, "ok.txt")
			f.installed(t, "ok.txt")
			f.installed(t, "../escape.cfg")

			plan := b.build(t, f)

			assert.Equal(t, []string{"ok.txt"}, destPaths(plan.Ops))
			assert.Equal(t, PlanOpRemove, opFor(t, plan, "ok.txt").Kind)
			assert.Len(t, warningsContaining(plan, "rejected installed file"), 1)
		})
	}
}
