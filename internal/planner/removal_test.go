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
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var backupSha = strings.Repeat("a", 64)

// planFixture is a game install with a target on disk, to plan removals for.
type planFixture struct {
	db        *sql.DB
	q         *dbq.Queries
	gameID    int64
	profileID int64
	target    dbq.Target
	root      string
}

func newPlanFixture(t *testing.T) planFixture {
	t.Helper()

	ctx := context.Background()
	db := testbuilder.SetupDB(t)
	q := dbq.New(db)
	root := t.TempDir()

	gi := testbuilder.NewGame(t, db).WithInstallRoot(root).Build()

	target, err := q.UpsertDiscoveredTarget(ctx, dbq.UpsertDiscoveredTargetParams{
		GameInstallID: gi.ID,
		Name:          "game_dir",
		RootPath:      root,
	})
	require.NoError(t, err)

	// the profile that every game install starts with, which has no mods in it
	profile, err := q.GetActiveProfileForGame(ctx, gi.ID)
	require.NoError(t, err)

	return planFixture{db: db, q: q, gameID: gi.ID, profileID: profile.ID, target: target, root: root}
}

// onDisk puts a file under the target.
func (f planFixture) onDisk(t *testing.T, relpath string) {
	t.Helper()

	abs := filepath.Join(f.root, relpath)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("hello"), 0o644))
}

// installed records a file as installed, whether or not it is on disk.
func (f planFixture) installed(t *testing.T, relpath string) {
	t.Helper()

	require.NoError(t, f.q.UpsertInstalledFile(context.Background(), dbq.UpsertInstalledFileParams{
		GameInstallID: f.gameID,
		TargetID:      f.target.ID,
		Relpath:       relpath,
		ContentSha256: helloSha,
		SizeBytes:     5,
	}))
}

// backedUp records that there is a backup of what was at relpath before.
func (f planFixture) backedUp(t *testing.T, relpath string) {
	t.Helper()

	ctx := context.Background()
	require.NoError(t, f.q.InsertBlob(ctx, dbq.InsertBlobParams{
		Sha256:    backupSha,
		Kind:      "backup",
		SizeBytes: 5,
	}))
	require.NoError(t, f.q.UpsertBackup(ctx, dbq.UpsertBackupParams{
		GameInstallID:    f.gameID,
		TargetID:         f.target.ID,
		Relpath:          relpath,
		BackupBlobSha256: backupSha,
		SizeBytes:        5,
	}))
}

// opsByPath indexes the ops of a plan by the path they are for.
func opsByPath(t *testing.T, plan Plan) map[string]PlanOp {
	t.Helper()

	ops := make(map[string]PlanOp, len(plan.Ops))
	for _, op := range plan.Ops {
		_, dup := ops[op.DestPath]
		require.False(t, dup, "more than one op for %q", op.DestPath)
		ops[op.DestPath] = op
	}
	return ops
}

// The two ways of planning the removal of what is installed share their
// behavior, so each case is run against both: unapplying (everything goes) and
// applying a profile that has no mods in it (so nothing is wanted any more).
func TestRemovalPlans(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	builders := map[string]func(f planFixture) (Plan, error){
		"unapply": func(f planFixture) (Plan, error) {
			return BuildUnapplyPlan(ctx, f.q, f.gameID, f.target)
		},
		"apply of an empty profile": func(f planFixture) (Plan, error) {
			return BuildApplyPlan(ctx, f.q, f.gameID, f.profileID, f.target, false)
		},
	}

	for name, build := range builders {
		name, build := name, build
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("nothing installed means nothing to do", func(t *testing.T) {
				t.Parallel()

				f := newPlanFixture(t)

				plan, err := build(f)
				require.NoError(t, err)

				assert.Empty(t, plan.Ops)
				assert.Empty(t, plan.Warnings)
				assert.Equal(t, f.gameID, plan.GameInstallID)
				assert.Equal(t, f.target.ID, plan.TargetID)
				assert.Equal(t, "game_dir", plan.TargetName)
				assert.Equal(t, f.root, plan.TargetRoot)
			})

			t.Run("installed file that is on disk is removed", func(t *testing.T) {
				t.Parallel()

				f := newPlanFixture(t)
				f.onDisk(t, "bin/mod.dll")
				f.installed(t, "bin/mod.dll")

				plan, err := build(f)
				require.NoError(t, err)

				require.Len(t, plan.Ops, 1)
				assert.Equal(t, PlanOp{Kind: PlanOpRemove, DestPath: "bin/mod.dll"}, plan.Ops[0])
				assert.Empty(t, plan.Warnings)
			})

			t.Run("installed file that is on disk and has a backup is restored from it", func(t *testing.T) {
				t.Parallel()

				f := newPlanFixture(t)
				f.onDisk(t, "bin/mod.dll")
				f.installed(t, "bin/mod.dll")
				f.backedUp(t, "bin/mod.dll")

				plan, err := build(f)
				require.NoError(t, err)

				require.Len(t, plan.Ops, 1)
				assert.Equal(t, PlanOp{
					Kind:         PlanOpRestoreBackup,
					DestPath:     "bin/mod.dll",
					BackupSha256: backupSha,
				}, plan.Ops[0])
				assert.Empty(t, plan.Warnings)
			})

			t.Run("installed file that is already gone is forgotten with a warning", func(t *testing.T) {
				t.Parallel()

				f := newPlanFixture(t)
				f.installed(t, "bin/mod.dll")

				plan, err := build(f)
				require.NoError(t, err)

				require.Len(t, plan.Ops, 1)
				assert.Equal(t, PlanOp{Kind: PlanOpRemove, DestPath: "bin/mod.dll"}, plan.Ops[0])
				require.Len(t, plan.Warnings, 1)
				assert.Contains(t, plan.Warnings[0], "drift")
				assert.Contains(t, plan.Warnings[0], "bin/mod.dll")
				assert.Contains(t, plan.Warnings[0], "already missing")
			})

			t.Run("a missing file is not restored even when it has a backup", func(t *testing.T) {
				t.Parallel()

				f := newPlanFixture(t)
				f.installed(t, "bin/mod.dll")
				f.backedUp(t, "bin/mod.dll")

				plan, err := build(f)
				require.NoError(t, err)

				require.Len(t, plan.Ops, 1)
				assert.Equal(t, PlanOpRemove, plan.Ops[0].Kind)
			})

			t.Run("each file is planned on its own", func(t *testing.T) {
				t.Parallel()

				f := newPlanFixture(t)
				f.onDisk(t, "plain.dll")
				f.installed(t, "plain.dll")
				f.onDisk(t, "data/backed.dll")
				f.installed(t, "data/backed.dll")
				f.backedUp(t, "data/backed.dll")
				f.installed(t, "gone.dll")

				plan, err := build(f)
				require.NoError(t, err)

				// in order of path
				require.Len(t, plan.Ops, 3)
				assert.Equal(t, PlanOp{
					Kind:         PlanOpRestoreBackup,
					DestPath:     "data/backed.dll",
					BackupSha256: backupSha,
				}, plan.Ops[0])
				assert.Equal(t, PlanOp{Kind: PlanOpRemove, DestPath: "gone.dll"}, plan.Ops[1])
				assert.Equal(t, PlanOp{Kind: PlanOpRemove, DestPath: "plain.dll"}, plan.Ops[2])
				require.Len(t, plan.Warnings, 1)
				assert.Contains(t, plan.Warnings[0], "gone.dll")
			})

			t.Run("the files come in the same order every time: by path", func(t *testing.T) {
				t.Parallel()

				f := newPlanFixture(t)
				// recorded in a different order from the one they have to
				// come out in, so that the order they were installed in and
				// the order the database happens to return them in (and the
				// order of a map) are all different from it
				paths := []string{"z.dll", "y/b.dll", "y/a.dll", "m.dll", "c.dll", "b.dll", "a.dll", "A.dll"}
				for _, p := range paths {
					f.installed(t, p)
				}
				want := append([]string(nil), paths...)
				sort.Strings(want)

				// a map goes through its keys in a different order each time,
				// so one run proves nothing
				for i := 0; i < 20; i++ {
					plan, err := build(f)
					require.NoError(t, err)

					got := make([]string, 0, len(plan.Ops))
					for _, op := range plan.Ops {
						got = append(got, op.DestPath)
					}
					require.Equal(t, want, got, "run %d", i)
				}
			})

			t.Run("files of another target are not part of the plan", func(t *testing.T) {
				t.Parallel()

				f := newPlanFixture(t)
				f.onDisk(t, "mine.dll")
				f.installed(t, "mine.dll")

				other, err := f.q.UpsertDiscoveredTarget(ctx, dbq.UpsertDiscoveredTargetParams{
					GameInstallID: f.gameID,
					Name:          "proton_prefix",
					RootPath:      t.TempDir(),
				})
				require.NoError(t, err)
				require.NoError(t, f.q.UpsertInstalledFile(ctx, dbq.UpsertInstalledFileParams{
					GameInstallID: f.gameID,
					TargetID:      other.ID,
					Relpath:       "theirs.dll",
					ContentSha256: helloSha,
					SizeBytes:     5,
				}))

				plan, err := build(f)
				require.NoError(t, err)

				ops := opsByPath(t, plan)
				assert.Len(t, ops, 1)
				assert.Contains(t, ops, "mine.dll")
			})

			t.Run("a file that can't be looked at stops the plan, not just a warning", func(t *testing.T) {
				t.Parallel()

				f := newPlanFixture(t)
				// "data" is a file, so nothing can be below it: that is not
				// the same as the file below it being gone
				f.onDisk(t, "data")
				f.installed(t, "data/mod.dll")

				plan, err := build(f)
				require.Error(t, err)
				assert.Contains(t, err.Error(), `stat "data/mod.dll"`)
				assert.Equal(t, Plan{}, plan)
			})
		})
	}
}
