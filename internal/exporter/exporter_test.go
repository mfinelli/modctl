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

package exporter

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
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newBlob writes a blob with the given content into the store and records it in
// the database, returning what verifyBlobs needs to check it.
func newBlob(t *testing.T, db *sql.DB, bs blobstore.Store, content string) blobToVerify {
	t.Helper()

	sum := sha256.Sum256([]byte(content))
	sha := hex.EncodeToString(sum[:])

	path, err := bs.PathFor(blobstore.KindArchive, sha)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	_, err = db.Exec(`INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', ?)`, sha, len(content))
	require.NoError(t, err)

	return blobToVerify{sha, blobstore.KindArchive}
}

func newStore(t *testing.T) blobstore.Store {
	t.Helper()
	dir := t.TempDir()
	return blobstore.Store{
		ArchivesDir:  filepath.Join(dir, "archives"),
		BackupsDir:   filepath.Join(dir, "backups"),
		OverridesDir: filepath.Join(dir, "overrides"),
		TmpDir:       filepath.Join(dir, "tmp"),
	}
}

// record collects the progress updates it is given.
type record struct{ got []Progress }

func (r *record) fn(p Progress) { r.got = append(r.got, p) }

func verifiedAt(t *testing.T, db *sql.DB, sha string) bool {
	t.Helper()
	var v sql.NullString
	require.NoError(t, db.QueryRow(`SELECT verified_at FROM blobs WHERE sha256 = ?`, sha).Scan(&v))
	return v.Valid
}

func TestVerifyBlobs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("reports each blob and then that it finished", func(t *testing.T) {
		t.Parallel()

		db := testbuilder.SetupDB(t)
		bs := newStore(t)
		blobs := []blobToVerify{
			newBlob(t, db, bs, "one"),
			newBlob(t, db, bs, "two"),
			newBlob(t, db, bs, "three"),
		}

		var rec record
		require.NoError(t, verifyBlobs(ctx, dbq.New(db), bs, blobs, rec.fn))

		assert.Equal(t, []Progress{
			{VerifyStarted, 0, 3},
			{VerifyBlob, 1, 3},
			{VerifyBlob, 2, 3},
			{VerifyBlob, 3, 3},
			{VerifyFinished, 3, 3},
		}, rec.got)

		for _, b := range blobs {
			assert.True(t, verifiedAt(t, db, b.sha256), "verified_at is set once a blob checks out")
		}
	})

	t.Run("nothing to verify is silent", func(t *testing.T) {
		t.Parallel()

		var rec record
		require.NoError(t, verifyBlobs(ctx, nil, newStore(t), nil, rec.fn))
		assert.Empty(t, rec.got)
	})

	t.Run("progress is optional", func(t *testing.T) {
		t.Parallel()

		db := testbuilder.SetupDB(t)
		bs := newStore(t)
		blobs := []blobToVerify{newBlob(t, db, bs, "one")}

		assert.NoError(t, verifyBlobs(ctx, dbq.New(db), bs, blobs, nil))
	})

	t.Run("a missing blob fails with its full hash", func(t *testing.T) {
		t.Parallel()

		db := testbuilder.SetupDB(t)
		bs := newStore(t)
		first := newBlob(t, db, bs, "one")
		missing := newBlob(t, db, bs, "two")
		path, err := bs.PathFor(blobstore.KindArchive, missing.sha256)
		require.NoError(t, err)
		require.NoError(t, os.Remove(path))

		var rec record
		err = verifyBlobs(ctx, dbq.New(db), bs, []blobToVerify{first, missing}, rec.fn)

		require.Error(t, err)
		assert.Contains(t, err.Error(), missing.sha256, "errors carry the whole hash")
		assert.Contains(t, err.Error(), "missing from disk")
		assert.NotContains(t, err.Error(), "...")

		assert.Equal(t, []Progress{
			{VerifyStarted, 0, 2},
			{VerifyBlob, 1, 2},
			{VerifyBlob, 2, 2},
			{VerifyFailed, 2, 2},
		}, rec.got)
		assert.True(t, verifiedAt(t, db, first.sha256))
		assert.False(t, verifiedAt(t, db, missing.sha256))
	})

	t.Run("a blob that does not match its hash fails", func(t *testing.T) {
		t.Parallel()

		db := testbuilder.SetupDB(t)
		bs := newStore(t)
		corrupt := newBlob(t, db, bs, "original")
		path, err := bs.PathFor(blobstore.KindArchive, corrupt.sha256)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, []byte("tampered"), 0o644))

		var rec record
		err = verifyBlobs(ctx, dbq.New(db), bs, []blobToVerify{corrupt}, rec.fn)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "integrity check failed")
		assert.Contains(t, err.Error(), corrupt.sha256)
		require.NotEmpty(t, rec.got)
		assert.Equal(t, VerifyFailed, rec.got[len(rec.got)-1].Kind)
		assert.False(t, verifiedAt(t, db, corrupt.sha256))
	})

	t.Run("cancelling stops it and says so", func(t *testing.T) {
		t.Parallel()

		db := testbuilder.SetupDB(t)
		bs := newStore(t)
		blobs := []blobToVerify{newBlob(t, db, bs, "one")}

		cancelled, cancel := context.WithCancel(ctx)
		cancel()

		var rec record
		err := verifyBlobs(cancelled, dbq.New(db), bs, blobs, rec.fn)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, []Progress{
			{VerifyStarted, 0, 1},
			{VerifyFailed, 0, 1},
		}, rec.got)
	})

	t.Run("a failure is never followed by a finish", func(t *testing.T) {
		t.Parallel()

		db := testbuilder.SetupDB(t)
		bs := newStore(t)
		bad := newBlob(t, db, bs, "one")
		path, err := bs.PathFor(blobstore.KindArchive, bad.sha256)
		require.NoError(t, err)
		require.NoError(t, os.Remove(path))

		var rec record
		require.Error(t, verifyBlobs(ctx, dbq.New(db), bs, []blobToVerify{bad}, rec.fn))
		for _, p := range rec.got {
			assert.NotEqual(t, VerifyFinished, p.Kind)
		}
	})
}
