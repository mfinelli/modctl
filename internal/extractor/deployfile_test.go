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
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/planner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var deploySeq atomic.Int64

// deployFixture is what deploying a file needs besides the file operation
// fixture: a mod version that is the winner of the path, a profile, a staging
// directory with the file that is deployed in it and a blob store that has
// room for overrides.
type deployFixture struct {
	fileOpFixture
	ex        Extractor
	profileID int64
	versionID int64
	staging   string
}

func newDeployFixture(t *testing.T) deployFixture {
	t.Helper()

	f := newFileOpFixture(t)
	f.store.OverridesDir = t.TempDir()
	f.store.TmpDir = t.TempDir()

	profile, err := f.q.GetActiveProfileForGame(context.Background(), f.gameID)
	require.NoError(t, err)

	n := deploySeq.Add(1)
	archiveSha := fmt.Sprintf("%064x", n)
	exec := func(query string, args ...any) int64 {
		res, err := f.db.Exec(query, args...)
		require.NoError(t, err, query)
		id, err := res.LastInsertId()
		require.NoError(t, err)
		return id
	}
	exec(`INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, archiveSha)
	page := exec(`INSERT INTO mod_pages (game_install_id, name, source_kind) VALUES (?, 'Mod', 'local')`, f.gameID)
	file := exec(`INSERT INTO mod_files (mod_page_id, label) VALUES (?, 'Main')`, page)
	version := exec(`INSERT INTO mod_file_versions (mod_file_id, archive_sha256, inventory_scanned_at) VALUES (?, ?, '2026-01-01T00:00:00.000Z')`, file, archiveSha)

	staging := t.TempDir()
	return deployFixture{
		fileOpFixture: f,
		ex:            Extractor{BlobStore: f.store, StagingDir: staging},
		profileID:     profile.ID,
		versionID:     version,
		staging:       staging,
	}
}

// fileOp is the op that deploys the file "staged.dll" of the staging directory
// (with the content that is given) to destPath.
func (f deployFixture) fileOp(t *testing.T, destPath, content string, kind planner.PlanOpKind) planner.PlanOp {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(f.staging, "staged.dll"), []byte(content), 0o644))

	return planner.PlanOp{
		Kind:     kind,
		DestPath: destPath,
		File: &planner.PlanFile{
			DestPath: destPath,
			Conflicts: []planner.Conflict{{
				ModFileVersionID: f.versionID,
				Won:              true,
				Entry: planner.RemappedEntry{
					ArchiveSha256: fmt.Sprintf("%064x", 0),
					SourcePath:    "staged.dll",
					DestPath:      destPath,
				},
			}},
		},
	}
}

type recordedChange struct {
	Action  string
	OldSha  sql.NullString
	OldSize sql.NullInt64
	NewSha  sql.NullString
	Backup  sql.NullString
}

func (f deployFixture) change(t *testing.T, relpath string) recordedChange {
	t.Helper()

	var c recordedChange
	require.NoError(t, f.db.QueryRow(
		`SELECT action, old_content_sha256, old_size_bytes, new_content_sha256, backup_blob_sha256
		 FROM operation_changes WHERE operation_id = ? AND relpath = ?`, f.operationID, relpath).
		Scan(&c.Action, &c.OldSha, &c.OldSize, &c.NewSha, &c.Backup))
	return c
}

func (f deployFixture) deploy(t *testing.T, op planner.PlanOp) DeployFileResult {
	t.Helper()

	res, err := f.ex.DeployFile(context.Background(), f.db, f.q, op, f.staging, f.root, f.gameID, f.targetID, f.profileID, f.operationID)
	require.NoError(t, err)
	return res
}

func nullSha(s string) sql.NullString { return sql.NullString{String: shaHex(s), Valid: true} }
func nullSize(s string) sql.NullInt64 { return sql.NullInt64{Int64: int64(len(s)), Valid: true} }

func TestDeployFileRecordsWhatItReplaces(t *testing.T) {
	t.Parallel()

	t.Run("an overwrite takes what the plan hashed", func(t *testing.T) {
		t.Parallel()
		f := newDeployFixture(t)
		f.install(t, "bin/a.dll", "what was there", shaHex("what was installed"))
		op := f.fileOp(t, "bin/a.dll", "new content", planner.PlanOpOverwrite)
		op.OnDiskSha256, op.OnDiskSize = nullSha("what was there"), nullSize("what was there")

		res := f.deploy(t, op)

		c := f.change(t, "bin/a.dll")
		assert.Equal(t, "overwrite", c.Action)
		assert.Equal(t, nullSha("what was there"), c.OldSha, "the content that was on disk, and not what was recorded")
		assert.Equal(t, nullSize("what was there"), c.OldSize)
		assert.Equal(t, nullSha("new content"), c.NewSha)
		assert.False(t, c.Backup.Valid)
		assert.False(t, res.WasBackedUp)
		assert.Equal(t, "new content", readFile(t, filepath.Join(f.root, "bin", "a.dll")))
	})

	t.Run("a backup is what was there, with its size", func(t *testing.T) {
		t.Parallel()
		f := newDeployFixture(t)
		abs := filepath.Join(f.root, "bin", "a.dll")
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte("the game's own"), 0o644))
		op := f.fileOp(t, "bin/a.dll", "new content", planner.PlanOpWrite)
		op.NeedsBackup = true

		res := f.deploy(t, op)

		c := f.change(t, "bin/a.dll")
		assert.Equal(t, "write", c.Action)
		assert.Equal(t, nullSha("the game's own"), c.OldSha)
		assert.Equal(t, nullSize("the game's own"), c.OldSize, "which was never recorded for a backup")
		assert.Equal(t, nullSha("the game's own"), c.Backup)
		assert.True(t, res.WasBackedUp)
		assert.Equal(t, int64(len("the game's own")), res.BackupSizeBytes)
	})

	t.Run("a backup wins over what the plan saw, since it is the file at the time it was copied", func(t *testing.T) {
		t.Parallel()
		f := newDeployFixture(t)
		f.install(t, "bin/a.dll", "changed since the plan", shaHex("what was installed"))
		op := f.fileOp(t, "bin/a.dll", "new content", planner.PlanOpOverwrite)
		op.NeedsBackup = true
		op.OnDiskSha256, op.OnDiskSize = nullSha("what the plan saw"), nullSize("what the plan saw")

		f.deploy(t, op)

		c := f.change(t, "bin/a.dll")
		assert.Equal(t, nullSha("changed since the plan"), c.OldSha)
		assert.Equal(t, nullSize("changed since the plan"), c.OldSize)
	})

	t.Run("a file that nothing looked at has no old content", func(t *testing.T) {
		t.Parallel()
		f := newDeployFixture(t)
		f.install(t, "bin/a.dll", "what was there", shaHex("what was installed"))
		op := f.fileOp(t, "bin/a.dll", "new content", planner.PlanOpOverwrite) // as with --no-recheck

		f.deploy(t, op)

		c := f.change(t, "bin/a.dll")
		assert.Equal(t, "overwrite", c.Action)
		assert.False(t, c.OldSha.Valid)
		assert.False(t, c.OldSize.Valid)
	})

	t.Run("a file that is new has no old content", func(t *testing.T) {
		t.Parallel()
		f := newDeployFixture(t)

		f.deploy(t, f.fileOp(t, "bin/new.dll", "new content", planner.PlanOpWrite))

		c := f.change(t, "bin/new.dll")
		assert.Equal(t, "write", c.Action)
		assert.False(t, c.OldSha.Valid)
		assert.False(t, c.OldSize.Valid)
	})
}

func TestDeployOverrideFileRecordsWhatItReplaces(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// the override of a whole file, whose blob is in the store
	setup := func(t *testing.T, content string) (deployFixture, planner.PlanOp) {
		t.Helper()

		f := newDeployFixture(t)
		src := filepath.Join(t.TempDir(), "override.cfg")
		require.NoError(t, os.WriteFile(src, []byte(content), 0o644))
		ingested, err := f.store.IngestFile(ctx, blobstore.KindOverride, src)
		require.NoError(t, err)
		res, err := f.db.Exec(`INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'override', ?) ON CONFLICT DO NOTHING`, ingested.SHA256Hex, ingested.SizeBytes)
		require.NoError(t, err)
		_ = res
		overrideID, err := f.db.Exec(`INSERT INTO overrides (profile_id, target_id, relpath, blob_sha256, override_type) VALUES (?, ?, 'user.cfg', ?, 'full_file')`,
			f.profileID, f.targetID, ingested.SHA256Hex)
		require.NoError(t, err)
		id, err := overrideID.LastInsertId()
		require.NoError(t, err)

		return f, planner.PlanOp{
			Kind:               planner.PlanOpOverwrite,
			DestPath:           "user.cfg",
			OverrideID:         sql.NullInt64{Int64: id, Valid: true},
			OverrideType:       "full_file",
			OverrideBlobSha256: sql.NullString{String: ingested.SHA256Hex, Valid: true},
		}
	}

	deploy := func(t *testing.T, f deployFixture, op planner.PlanOp) DeployOverrideFileResult {
		t.Helper()

		res, err := f.ex.DeployOverrideFile(ctx, f.db, f.q, op, f.staging, f.root, f.gameID, f.targetID, f.profileID, f.operationID, nil)
		require.NoError(t, err)
		return res
	}

	t.Run("an overwrite takes what the plan hashed", func(t *testing.T) {
		t.Parallel()
		f, op := setup(t, "my settings")
		f.install(t, "user.cfg", "what was there", shaHex("what was installed"))
		op.OnDiskSha256, op.OnDiskSize = nullSha("what was there"), nullSize("what was there")

		deploy(t, f, op)

		c := f.change(t, "user.cfg")
		assert.Equal(t, "overwrite", c.Action)
		assert.Equal(t, nullSha("what was there"), c.OldSha)
		assert.Equal(t, nullSize("what was there"), c.OldSize)
		assert.Equal(t, nullSha("my settings"), c.NewSha)
		assert.Equal(t, "my settings", readFile(t, filepath.Join(f.root, "user.cfg")))
	})

	t.Run("a backup is what was there, with its size", func(t *testing.T) {
		t.Parallel()
		f, op := setup(t, "my settings")
		require.NoError(t, os.WriteFile(filepath.Join(f.root, "user.cfg"), []byte("the game's own"), 0o644))
		op.Kind = planner.PlanOpWrite
		op.NeedsBackup = true

		res := deploy(t, f, op)

		c := f.change(t, "user.cfg")
		assert.Equal(t, nullSha("the game's own"), c.OldSha)
		assert.Equal(t, nullSize("the game's own"), c.OldSize)
		assert.True(t, res.WasBackedUp)
		assert.Equal(t, int64(len("the game's own")), res.BackupSizeBytes)
	})

	t.Run("a file that nothing looked at has no old content", func(t *testing.T) {
		t.Parallel()
		f, op := setup(t, "my settings")
		f.install(t, "user.cfg", "what was there", shaHex("what was installed"))

		deploy(t, f, op)

		c := f.change(t, "user.cfg")
		assert.False(t, c.OldSha.Valid)
		assert.False(t, c.OldSize.Valid)
	})
}
