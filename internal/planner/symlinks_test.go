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
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var modSeq atomic.Int64

// addMod puts a mod in the profile of the fixture (a mod page, a file and a
// version that has been inventoried, with the given paths in it), and returns
// the id of the version.
func (f planFixture) addMod(t *testing.T, relpaths ...string) (versionID int64) {
	t.Helper()

	n := modSeq.Add(1)
	archiveSha := fmt.Sprintf("%064x", n)
	exec := func(query string, args ...any) int64 {
		t.Helper()
		res, err := f.db.Exec(query, args...)
		require.NoError(t, err, query)
		id, err := res.LastInsertId()
		require.NoError(t, err)
		return id
	}

	exec(`INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, archiveSha)
	page := exec(`INSERT INTO mod_pages (game_install_id, name, source_kind) VALUES (?, ?, 'local')`,
		f.gameID, fmt.Sprintf("Mod %d", n))
	file := exec(`INSERT INTO mod_files (mod_page_id, label) VALUES (?, 'Main')`, page)
	versionID = exec(`INSERT INTO mod_file_versions (mod_file_id, archive_sha256, inventory_scanned_at) VALUES (?, ?, '2026-01-01T00:00:00.000Z')`,
		file, archiveSha)
	for i, p := range relpaths {
		exec(`INSERT INTO archive_inventory_entries (archive_sha256, raw_path, entry_type, size_bytes, position) VALUES (?, ?, 'file', 5, ?)`,
			archiveSha, p, i)
	}
	exec(`INSERT INTO profile_items (profile_id, mod_file_version_id, target_id, priority, enabled) VALUES (?, ?, ?, 1, 1)`,
		f.profileID, versionID, f.target.ID)

	return versionID
}

func (f planFixture) buildApply(t *testing.T, skipRecheck bool) Plan {
	t.Helper()

	plan, err := BuildApplyPlan(context.Background(), f.q, f.gameID, f.profileID, f.target, skipRecheck)
	require.NoError(t, err)
	return plan
}

// opFor is the op in the plan that is for destPath.
func opFor(t *testing.T, plan Plan, destPath string) PlanOp {
	t.Helper()

	for _, op := range plan.Ops {
		if op.DestPath == destPath {
			return op
		}
	}
	require.Failf(t, "no op", "there is no op for %q in %+v", destPath, plan.Ops)
	return PlanOp{}
}

// symlinkWarnings are the warnings of a plan that are about symlinks.
func symlinkWarnings(plan Plan) []string {
	var out []string
	for _, w := range plan.Warnings {
		if len(w) >= 8 && w[:8] == "symlink:" {
			out = append(out, w)
		}
	}
	return out
}

func TestBuildApplyPlanSymlinks(t *testing.T) {
	t.Parallel()

	t.Run("a regular file that is replaced does not get a warning", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.onDisk(t, "bin/mod.dll")
		f.addMod(t, "bin/mod.dll")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "bin/mod.dll")
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.True(t, op.NeedsBackup)
		assert.Empty(t, symlinkWarnings(plan))
	})

	t.Run("a file that is not there yet does not get one", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.addMod(t, "bin/mod.dll")

		plan := f.buildApply(t, false)

		assert.Equal(t, PlanOpWrite, opFor(t, plan, "bin/mod.dll").Kind)
		assert.Empty(t, symlinkWarnings(plan))
	})

	t.Run("a symlink to a file is replaced, and the warning says what happens to it", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		elsewhere := filepath.Join(t.TempDir(), "shared.cfg")
		require.NoError(t, os.WriteFile(elsewhere, []byte("shared"), 0o644))
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(f.root, "settings.cfg")))
		f.addMod(t, "settings.cfg")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "settings.cfg")
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.True(t, op.NeedsBackup, "what it points to is backed up")
		warnings := symlinkWarnings(plan)
		require.Len(t, warnings, 1)
		assert.Equal(t, symlinkWarning("settings.cfg", elsewhere, true, true), warnings[0])
		assert.Contains(t, warnings[0], elsewhere)
	})

	t.Run("a relative symlink is given as it is written", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		require.NoError(t, os.WriteFile(filepath.Join(f.root, "real.cfg"), []byte("x"), 0o644))
		require.NoError(t, os.Symlink("real.cfg", filepath.Join(f.root, "settings.cfg")))
		f.addMod(t, "settings.cfg")

		warnings := symlinkWarnings(f.buildApply(t, false))

		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], `points to "real.cfg"`)
	})

	t.Run("a symlink that leads nowhere is replaced without a backup, and the warning says so", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		gone := filepath.Join(t.TempDir(), "gone.cfg")
		require.NoError(t, os.Symlink(gone, filepath.Join(f.root, "settings.cfg")))
		f.addMod(t, "settings.cfg")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "settings.cfg")
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.False(t, op.NeedsBackup, "there is nothing to back up")
		warnings := symlinkWarnings(plan)
		require.Len(t, warnings, 1)
		assert.Equal(t, symlinkWarning("settings.cfg", gone, false, false), warnings[0])
		assert.Contains(t, warnings[0], "doesn't exist")
	})

	t.Run("a symlink that is replaced without a backup says that it will not come back", func(t *testing.T) {
		t.Parallel()

		// a file that is installed and then replaced by a symlink to
		// something else: replaced again without looking at it
		f := newPlanFixture(t)
		elsewhere := filepath.Join(t.TempDir(), "shared.cfg")
		require.NoError(t, os.WriteFile(elsewhere, []byte("shared"), 0o644))
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(f.root, "settings.cfg")))
		version := f.addMod(t, "settings.cfg")
		require.NoError(t, f.q.UpsertInstalledFile(context.Background(), dbq.UpsertInstalledFileParams{
			GameInstallID:         f.gameID,
			TargetID:              f.target.ID,
			Relpath:               "settings.cfg",
			ContentSha256:         helloSha,
			SizeBytes:             5,
			OwnerModFileVersionID: sql.NullInt64{Int64: version, Valid: true},
		}))

		plan := f.buildApply(t, true)

		op := opFor(t, plan, "settings.cfg")
		assert.Equal(t, PlanOpOverwrite, op.Kind)
		assert.False(t, op.NeedsBackup)
		warnings := symlinkWarnings(plan)
		require.Len(t, warnings, 1)
		assert.Equal(t, symlinkWarning("settings.cfg", elsewhere, true, false), warnings[0])
	})

	t.Run("a symlink to a directory is mentioned too", func(t *testing.T) {
		t.Parallel()

		// (apply itself can't back up a directory, with or without a
		// symlink to it: that is a different matter)

		f := newPlanFixture(t)
		require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(f.root, "settings.cfg")))
		f.addMod(t, "settings.cfg")

		assert.Len(t, symlinkWarnings(f.buildApply(t, false)), 1)
	})

	t.Run("files below a directory that is a symlink are not symlinks", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		realDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(realDir, "mod.dll"), []byte("x"), 0o644))
		require.NoError(t, os.Symlink(realDir, filepath.Join(f.root, "Data")))
		f.addMod(t, "Data/mod.dll")

		plan := f.buildApply(t, false)

		assert.Equal(t, PlanOpWrite, opFor(t, plan, "Data/mod.dll").Kind)
		assert.Empty(t, symlinkWarnings(plan))
	})

	t.Run("a symlink that nothing is written to is not mentioned", func(t *testing.T) {
		t.Parallel()

		// installed, and the symlink leads to a file that has the same
		// content: nothing changes, so the symlink stays as it is
		f := newPlanFixture(t)
		elsewhere := filepath.Join(t.TempDir(), "copy.dll")
		require.NoError(t, os.WriteFile(elsewhere, []byte("hello"), 0o644))
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(f.root, "mod.dll")))
		version := f.addMod(t, "mod.dll")
		require.NoError(t, f.q.UpsertInstalledFile(context.Background(), dbq.UpsertInstalledFileParams{
			GameInstallID:         f.gameID,
			TargetID:              f.target.ID,
			Relpath:               "mod.dll",
			ContentSha256:         helloSha,
			SizeBytes:             5,
			OwnerModFileVersionID: sql.NullInt64{Int64: version, Valid: true},
		}))

		plan := f.buildApply(t, false)

		assert.Equal(t, PlanOpNoop, opFor(t, plan, "mod.dll").Kind)
		assert.Empty(t, symlinkWarnings(plan))
	})

	t.Run("each symlink gets its own warning", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		for _, name := range []string{"a.cfg", "b.cfg"} {
			target := filepath.Join(t.TempDir(), name)
			require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
			require.NoError(t, os.Symlink(target, filepath.Join(f.root, name)))
		}
		f.onDisk(t, "c.cfg")
		f.addMod(t, "a.cfg", "b.cfg", "c.cfg")

		warnings := symlinkWarnings(f.buildApply(t, false))

		require.Len(t, warnings, 2)
		assert.Contains(t, warnings[0], `"a.cfg"`)
		assert.Contains(t, warnings[1], `"b.cfg"`)
	})
}

func TestSymlinkWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		pointsAtSomething bool
		backedUp          bool
		wantContains      []string
		wantNotContains   []string
	}{
		{
			name:              "points at something that is backed up",
			pointsAtSomething: true,
			backedUp:          true,
			wantContains:      []string{`"bin/x.cfg"`, `"/shared/x.cfg"`, "replaced by a regular file", "left alone", "put back a regular file", "not the symlink"},
			wantNotContains:   []string{"doesn't exist"},
		},
		{
			name:              "points at something that is not backed up",
			pointsAtSomething: true,
			backedUp:          false,
			wantContains:      []string{`"bin/x.cfg"`, `"/shared/x.cfg"`, "replaced by a regular file", "left alone", "won't bring the symlink back"},
			wantNotContains:   []string{"doesn't exist", "put back"},
		},
		{
			name:              "points at nothing",
			pointsAtSomething: false,
			backedUp:          false,
			wantContains:      []string{`"bin/x.cfg"`, `"/shared/x.cfg"`, "doesn't exist", "replaced by a regular file", "won't bring the symlink back"},
			wantNotContains:   []string{"left alone", "put back"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := symlinkWarning("bin/x.cfg", "/shared/x.cfg", tc.pointsAtSomething, tc.backedUp)

			assert.True(t, len(got) > 8 && got[:8] == "symlink:", "starts with what it is about: %s", got)
			for _, s := range tc.wantContains {
				assert.Contains(t, got, s)
			}
			for _, s := range tc.wantNotContains {
				assert.NotContains(t, got, s)
			}
		})
	}

	t.Run("a backup that can't be made is not told about", func(t *testing.T) {
		t.Parallel()

		// there is nothing to back up when it leads nowhere, whatever
		// backedUp says
		a := symlinkWarning("x", "y", false, true)
		b := symlinkWarning("x", "y", false, false)

		assert.Equal(t, a, b)
	})
}
