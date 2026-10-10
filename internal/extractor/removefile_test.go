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

package extractor

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/planner"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// removeFixture is a game install with a target on disk, a running operation,
// and helpers to put files on disk and in installed_files.
type removeFixture struct {
	db          *sql.DB
	q           *dbq.Queries
	gameID      int64
	targetID    int64
	operationID int64
	root        string
}

func newRemoveFixture(t *testing.T) removeFixture {
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

	op, err := q.CreateOperation(ctx, dbq.CreateOperationParams{
		GameInstallID: gi.ID,
		OpType:        "apply",
	})
	require.NoError(t, err)

	return removeFixture{db: db, q: q, gameID: gi.ID, targetID: target.ID, operationID: op.ID, root: root}
}

// install writes a file under the target and records it as installed.
func (f removeFixture) install(t *testing.T, relpath, content, sha string) {
	t.Helper()

	abs := filepath.Join(f.root, relpath)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))

	f.record(t, relpath, int64(len(content)), sha)
}

// record adds an installed_files row without putting anything on disk.
func (f removeFixture) record(t *testing.T, relpath string, size int64, sha string) {
	t.Helper()

	require.NoError(t, f.q.UpsertInstalledFile(context.Background(), dbq.UpsertInstalledFileParams{
		GameInstallID:   f.gameID,
		TargetID:        f.targetID,
		Relpath:         relpath,
		ContentSha256:   sha,
		SizeBytes:       size,
		LastOperationID: sql.NullInt64{Int64: f.operationID, Valid: true},
	}))
}

func (f removeFixture) isInstalled(t *testing.T, relpath string) bool {
	t.Helper()

	_, err := f.q.GetInstalledFileByPath(context.Background(), dbq.GetInstalledFileByPathParams{
		GameInstallID: f.gameID,
		TargetID:      f.targetID,
		Relpath:       relpath,
	})
	if err == sql.ErrNoRows {
		return false
	}
	require.NoError(t, err)
	return true
}

func (f removeFixture) changes(t *testing.T, operationID int64) []dbq.OperationChange {
	t.Helper()

	changes, err := f.q.ListOperationChanges(context.Background(), operationID)
	require.NoError(t, err)
	return changes
}

func (f removeFixture) remove(ctx context.Context, relpath string, operationID int64) (RemoveFileResult, error) {
	return Extractor{}.RemoveFile(ctx, f.db, f.q,
		planner.PlanOp{Kind: planner.PlanOpRemove, DestPath: relpath},
		f.root, f.gameID, f.targetID, operationID)
}

func TestRemoveFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("removes the file, forgets it and records the change", func(t *testing.T) {
		t.Parallel()

		f := newRemoveFixture(t)
		f.install(t, "bin/mod.dll", "hello", helloSha)

		res, err := f.remove(ctx, "bin/mod.dll", f.operationID)
		require.NoError(t, err)
		assert.Equal(t, "bin/mod.dll", res.DestPath)

		assert.NoFileExists(t, filepath.Join(f.root, "bin", "mod.dll"))
		assert.False(t, f.isInstalled(t, "bin/mod.dll"))

		changes := f.changes(t, f.operationID)
		require.Len(t, changes, 1)
		assert.Equal(t, "remove", changes[0].Action)
		assert.Equal(t, "bin/mod.dll", changes[0].Relpath)
		assert.Equal(t, f.gameID, changes[0].GameInstallID)
		assert.Equal(t, f.targetID, changes[0].TargetID)
		// what the file was, for the audit trail
		assert.Equal(t, sql.NullString{String: helloSha, Valid: true}, changes[0].OldContentSha256)
		assert.Equal(t, sql.NullInt64{Int64: 5, Valid: true}, changes[0].OldSizeBytes)
		assert.False(t, changes[0].NewContentSha256.Valid)
		assert.False(t, changes[0].BackupBlobSha256.Valid)
	})

	t.Run("leaves the directories the file was in", func(t *testing.T) {
		t.Parallel()

		f := newRemoveFixture(t)
		f.install(t, "data/sub/mod.dll", "hello", helloSha)

		_, err := f.remove(ctx, "data/sub/mod.dll", f.operationID)
		require.NoError(t, err)

		// emptied directories are only removed by apply --prune-dirs
		assert.DirExists(t, filepath.Join(f.root, "data", "sub"))
	})

	t.Run("other installed files are not touched", func(t *testing.T) {
		t.Parallel()

		f := newRemoveFixture(t)
		f.install(t, "a.dll", "hello", helloSha)
		f.install(t, "b.dll", "hello", helloSha)

		_, err := f.remove(ctx, "a.dll", f.operationID)
		require.NoError(t, err)

		assert.FileExists(t, filepath.Join(f.root, "b.dll"))
		assert.True(t, f.isInstalled(t, "b.dll"))
		assert.Len(t, f.changes(t, f.operationID), 1)
	})

	t.Run("a file that is already gone is still forgotten", func(t *testing.T) {
		t.Parallel()

		f := newRemoveFixture(t)
		f.record(t, "bin/mod.dll", 5, helloSha)

		_, err := f.remove(ctx, "bin/mod.dll", f.operationID)
		require.NoError(t, err)

		assert.False(t, f.isInstalled(t, "bin/mod.dll"))

		changes := f.changes(t, f.operationID)
		require.Len(t, changes, 1)
		assert.Equal(t, "remove", changes[0].Action)
		assert.False(t, changes[0].OldContentSha256.Valid, "there was nothing to hash")
		assert.False(t, changes[0].OldSizeBytes.Valid)
	})

	t.Run("canceled context leaves the file and the database alone", func(t *testing.T) {
		t.Parallel()

		f := newRemoveFixture(t)
		f.install(t, "bin/mod.dll", "hello", helloSha)

		canceled, cancel := context.WithCancel(ctx)
		cancel()

		_, err := f.remove(canceled, "bin/mod.dll", f.operationID)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)

		assert.FileExists(t, filepath.Join(f.root, "bin", "mod.dll"))
		assert.True(t, f.isInstalled(t, "bin/mod.dll"))
		assert.Empty(t, f.changes(t, f.operationID))
	})

	t.Run("a failure recording the change rolls back what the database did", func(t *testing.T) {
		t.Parallel()

		f := newRemoveFixture(t)
		f.install(t, "bin/mod.dll", "hello", helloSha)

		// an operation that doesn't exist makes the insert violate a foreign
		// key, after the installed file row has already been deleted
		_, err := f.remove(ctx, "bin/mod.dll", f.operationID+1000)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insert operation change")

		// the row is back, since it is all one transaction (the file itself
		// was removed first, which the next apply sees as drift)
		assert.True(t, f.isInstalled(t, "bin/mod.dll"))
		assert.Empty(t, f.changes(t, f.operationID))
		assert.Empty(t, f.changes(t, f.operationID+1000))
	})
}
