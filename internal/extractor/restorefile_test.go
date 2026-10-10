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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/planner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// backedUp stores content as the backup of what was at relpath before: the
// blob in the store and in the database, and the row that says what it is a
// backup of. It returns the sha256 of the blob.
func (f fileOpFixture) backedUp(t *testing.T, relpath, content string) string {
	t.Helper()

	ctx := context.Background()
	sum := sha256.Sum256([]byte(content))
	sha := hex.EncodeToString(sum[:])

	path, err := f.store.PathFor(blobstore.KindBackup, sha)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	require.NoError(t, f.q.InsertBlob(ctx, dbq.InsertBlobParams{
		Sha256:    sha,
		Kind:      "backup",
		SizeBytes: int64(len(content)),
	}))
	require.NoError(t, f.q.UpsertBackup(ctx, dbq.UpsertBackupParams{
		GameInstallID:    f.gameID,
		TargetID:         f.targetID,
		Relpath:          relpath,
		BackupBlobSha256: sha,
		SizeBytes:        int64(len(content)),
	}))

	return sha
}

func (f fileOpFixture) hasBackup(t *testing.T, relpath string) bool {
	t.Helper()

	_, err := f.q.GetBackupForPath(context.Background(), dbq.GetBackupForPathParams{
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

func (f fileOpFixture) restore(ctx context.Context, relpath, backupSha string, operationID int64) (RestoreFileResult, error) {
	return Extractor{BlobStore: f.store}.RestoreFile(ctx, f.db, f.q,
		planner.PlanOp{Kind: planner.PlanOpRestoreBackup, DestPath: relpath, BackupSha256: backupSha},
		f.root, f.gameID, f.targetID, operationID)
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestRestoreFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("puts the backup back, forgets the file and the backup, and records the change", func(t *testing.T) {
		t.Parallel()

		f := newFileOpFixture(t)
		f.install(t, "bin/game.dll", "modded", helloSha)
		sha := f.backedUp(t, "bin/game.dll", "the original")

		res, err := f.restore(ctx, "bin/game.dll", sha, f.operationID)
		require.NoError(t, err)
		assert.Equal(t, "bin/game.dll", res.DestPath)

		dest := filepath.Join(f.root, "bin", "game.dll")
		assert.Equal(t, "the original", readFile(t, dest))
		info, err := os.Stat(dest)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())

		assert.False(t, f.isInstalled(t, "bin/game.dll"), "no longer ours")
		assert.False(t, f.hasBackup(t, "bin/game.dll"), "the backup has been used")

		changes := f.changes(t, f.operationID)
		require.Len(t, changes, 1)
		assert.Equal(t, "restore_backup", changes[0].Action)
		assert.Equal(t, "bin/game.dll", changes[0].Relpath)
		assert.Equal(t, sql.NullString{String: sha, Valid: true}, changes[0].BackupBlobSha256)
		// what is on disk now: the content of the backup, and its size
		assert.Equal(t, sql.NullString{String: sha, Valid: true}, changes[0].NewContentSha256)
		assert.Equal(t, sql.NullInt64{Int64: int64(len("the original")), Valid: true}, changes[0].NewSizeBytes)
		// and what it replaced
		assert.Equal(t, sql.NullString{String: shaHex("modded"), Valid: true}, changes[0].OldContentSha256)
		assert.Equal(t, sql.NullInt64{Int64: int64(len("modded")), Valid: true}, changes[0].OldSizeBytes)
		assert.False(t, changes[0].ModFileVersionID.Valid)
	})

	t.Run("what was replaced is what was on disk, even if it had been changed since it was installed", func(t *testing.T) {
		t.Parallel()

		f := newFileOpFixture(t)
		// installed as "modded", and changed by hand after that
		f.record(t, "bin/game.dll", 6, shaHex("modded"))
		abs := filepath.Join(f.root, "bin", "game.dll")
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte("changed by hand"), 0o644))
		sha := f.backedUp(t, "bin/game.dll", "the original")

		_, err := f.restore(ctx, "bin/game.dll", sha, f.operationID)
		require.NoError(t, err)

		changes := f.changes(t, f.operationID)
		require.Len(t, changes, 1)
		assert.Equal(t, sql.NullString{String: shaHex("changed by hand"), Valid: true}, changes[0].OldContentSha256)
		assert.Equal(t, sql.NullInt64{Int64: int64(len("changed by hand")), Valid: true}, changes[0].OldSizeBytes)
		assert.Equal(t, "the original", readFile(t, abs))
	})

	t.Run("a file that is gone is restored, with nothing to say of what it replaced", func(t *testing.T) {
		t.Parallel()

		f := newFileOpFixture(t)
		f.record(t, "bin/game.dll", 6, helloSha)
		sha := f.backedUp(t, "bin/game.dll", "the original")

		_, err := f.restore(ctx, "bin/game.dll", sha, f.operationID)
		require.NoError(t, err)

		assert.Equal(t, "the original", readFile(t, filepath.Join(f.root, "bin", "game.dll")))
		changes := f.changes(t, f.operationID)
		require.Len(t, changes, 1)
		assert.False(t, changes[0].OldContentSha256.Valid)
		assert.False(t, changes[0].OldSizeBytes.Valid)
		assert.True(t, changes[0].NewContentSha256.Valid)
	})

	t.Run("a file that can't be read is still restored", func(t *testing.T) {
		t.Parallel()

		if os.Geteuid() == 0 {
			t.Skip("root can read a file whatever its permissions")
		}

		f := newFileOpFixture(t)
		f.install(t, "bin/game.dll", "modded", helloSha)
		abs := filepath.Join(f.root, "bin", "game.dll")
		require.NoError(t, os.Chmod(abs, 0o000))
		sha := f.backedUp(t, "bin/game.dll", "the original")

		_, err := f.restore(ctx, "bin/game.dll", sha, f.operationID)
		require.NoError(t, err)

		assert.Equal(t, "the original", readFile(t, abs))
		changes := f.changes(t, f.operationID)
		require.Len(t, changes, 1)
		assert.False(t, changes[0].OldContentSha256.Valid)
		assert.False(t, changes[0].OldSizeBytes.Valid)
	})

	t.Run("the blob stays in the store", func(t *testing.T) {
		t.Parallel()

		f := newFileOpFixture(t)
		f.install(t, "game.dll", "modded", helloSha)
		sha := f.backedUp(t, "game.dll", "the original")

		_, err := f.restore(ctx, "game.dll", sha, f.operationID)
		require.NoError(t, err)

		// removing it is up to gc
		path, err := f.store.PathFor(blobstore.KindBackup, sha)
		require.NoError(t, err)
		assert.FileExists(t, path)
		_, err = f.q.GetBlob(ctx, sha)
		assert.NoError(t, err)
	})

	t.Run("directories that are gone are made again", func(t *testing.T) {
		t.Parallel()

		f := newFileOpFixture(t)
		f.record(t, "data/sub/game.dll", 6, helloSha)
		sha := f.backedUp(t, "data/sub/game.dll", "the original")

		_, err := f.restore(ctx, "data/sub/game.dll", sha, f.operationID)
		require.NoError(t, err)

		assert.Equal(t, "the original", readFile(t, filepath.Join(f.root, "data", "sub", "game.dll")))
	})

	t.Run("other backups and installed files are not touched", func(t *testing.T) {
		t.Parallel()

		f := newFileOpFixture(t)
		f.install(t, "a.dll", "modded a", helloSha)
		f.install(t, "b.dll", "modded b", helloSha)
		shaA := f.backedUp(t, "a.dll", "original a")
		f.backedUp(t, "b.dll", "original b")

		_, err := f.restore(ctx, "a.dll", shaA, f.operationID)
		require.NoError(t, err)

		assert.Equal(t, "modded b", readFile(t, filepath.Join(f.root, "b.dll")))
		assert.True(t, f.isInstalled(t, "b.dll"))
		assert.True(t, f.hasBackup(t, "b.dll"))
	})

	t.Run("a backup that is missing from the store leaves everything alone", func(t *testing.T) {
		t.Parallel()

		f := newFileOpFixture(t)
		f.install(t, "bin/game.dll", "modded", helloSha)
		sha := f.backedUp(t, "bin/game.dll", "the original")
		path, err := f.store.PathFor(blobstore.KindBackup, sha)
		require.NoError(t, err)
		require.NoError(t, os.Remove(path))

		_, err = f.restore(ctx, "bin/game.dll", sha, f.operationID)
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "backup blob")

		assert.Equal(t, "modded", readFile(t, filepath.Join(f.root, "bin", "game.dll")))
		assert.True(t, f.isInstalled(t, "bin/game.dll"))
		assert.True(t, f.hasBackup(t, "bin/game.dll"))
		assert.Empty(t, f.changes(t, f.operationID))
	})

	t.Run("a hash that is not a hash is an error", func(t *testing.T) {
		t.Parallel()

		f := newFileOpFixture(t)
		f.install(t, "bin/game.dll", "modded", helloSha)

		_, err := f.restore(ctx, "bin/game.dll", "abc", f.operationID)
		require.Error(t, err)

		assert.Equal(t, "modded", readFile(t, filepath.Join(f.root, "bin", "game.dll")))
		assert.True(t, f.isInstalled(t, "bin/game.dll"))
	})

	t.Run("canceled context leaves everything alone", func(t *testing.T) {
		t.Parallel()

		f := newFileOpFixture(t)
		f.install(t, "bin/game.dll", "modded", helloSha)
		sha := f.backedUp(t, "bin/game.dll", "the original")

		canceled, cancel := context.WithCancel(ctx)
		cancel()

		_, err := f.restore(canceled, "bin/game.dll", sha, f.operationID)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)

		assert.Equal(t, "modded", readFile(t, filepath.Join(f.root, "bin", "game.dll")))
		assert.True(t, f.isInstalled(t, "bin/game.dll"))
		assert.True(t, f.hasBackup(t, "bin/game.dll"))
		assert.Empty(t, f.changes(t, f.operationID))
	})

	t.Run("a failure recording the change rolls back what the database did", func(t *testing.T) {
		t.Parallel()

		f := newFileOpFixture(t)
		f.install(t, "bin/game.dll", "modded", helloSha)
		sha := f.backedUp(t, "bin/game.dll", "the original")

		// an operation that doesn't exist makes the insert violate a foreign
		// key, after the rows have already been deleted
		_, err := f.restore(ctx, "bin/game.dll", sha, f.operationID+1000)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insert operation change")

		// both rows are back, since it is all one transaction (the file
		// itself was already restored, which the next apply sees as drift)
		assert.True(t, f.isInstalled(t, "bin/game.dll"))
		assert.True(t, f.hasBackup(t, "bin/game.dll"))
		assert.Empty(t, f.changes(t, f.operationID))
	})
}
