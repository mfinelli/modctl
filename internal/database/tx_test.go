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

package database_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fileDB is a database in a file of the test, with the migrations applied. It is
// not in memory because every connection to a database that is in memory is a
// different database, and a transaction has a connection of its own.
func fileDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", database.DSN(filepath.Join(t.TempDir(), "modctl.db"), false))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = testbuilder.NewProvider(t, db).Up(context.Background())
	require.NoError(t, err)

	return db
}

func blobCount(t *testing.T, db *sql.DB) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM blobs`).Scan(&n))
	return n
}

// noTransactionLeft is for after a transaction that was rolled back: its
// connection has to be given back, which is what ending it is for. One that was
// not would be held (with what it did half-done) until the context is done.
func noTransactionLeft(t *testing.T, db *sql.DB) {
	t.Helper()

	assert.Zero(t, db.Stats().InUse, "a connection is still held by a transaction")
}

func insertBlob(ctx context.Context, q *dbq.Queries, sha string) error {
	return q.InsertBlob(ctx, dbq.InsertBlobParams{Sha256: sha, Kind: "archive", SizeBytes: 1})
}

const (
	sha1 = "0000000000000000000000000000000000000000000000000000000000000001"
	sha2 = "0000000000000000000000000000000000000000000000000000000000000002"
)

func TestInTx(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("what was done is there when fn returns nil", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)

		err := database.InTx(ctx, db, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha1)
			return err
		})

		require.NoError(t, err)
		assert.Equal(t, 1, blobCount(t, db))
	})

	t.Run("what was done is not there when fn returns an error, which is returned as it is", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)
		boom := errors.New("boom")

		err := database.InTx(ctx, db, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha1)
			require.NoError(t, err)
			return boom
		})

		assert.Same(t, boom, err, "not wrapped")
		assert.Equal(t, 0, blobCount(t, db))
		noTransactionLeft(t, db)
	})

	t.Run("a panic rolls back and goes up", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)

		assert.PanicsWithValue(t, "oh no", func() {
			_ = database.InTx(ctx, db, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha1)
				require.NoError(t, err)
				panic("oh no")
			})
		})

		assert.Equal(t, 0, blobCount(t, db))
		noTransactionLeft(t, db)

		// and the database is not left in the middle of a transaction
		require.NoError(t, database.InTx(ctx, db, func(*sql.Tx) error { return nil }))
	})

	t.Run("what was done is not there until it is committed, to the others", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)

		err := database.InTx(ctx, db, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha1)
			require.NoError(t, err)
			assert.Equal(t, 0, blobCount(t, db), "through db it is not part of the transaction")
			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, 1, blobCount(t, db))
	})

	t.Run("a commit that fails is an error that says so, and nothing is there", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		err := database.InTx(ctx, db, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha1)
			require.NoError(t, err)
			cancel() // the transaction is rolled back, so there is nothing to commit
			return nil
		})

		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled, "what went wrong is what it wraps")
		assert.Contains(t, err.Error(), "commit: ")
		assert.Equal(t, 0, blobCount(t, db))
	})

	t.Run("an error of fn is not described as a commit", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)

		err := database.InTx(ctx, db, func(*sql.Tx) error { return errors.New("boom") })

		assert.EqualError(t, err, "boom")
	})

	t.Run("a transaction that can't be started is an error, and fn is not called", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)
		require.NoError(t, db.Close())

		called := false
		err := database.InTx(ctx, db, func(*sql.Tx) error {
			called = true
			return nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "begin transaction")
		assert.False(t, called)
		assert.NotContains(t, err.Error(), "commit")
	})

	t.Run("a context that is done doesn't start one", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		called := false
		err := database.InTx(ctx, db, func(*sql.Tx) error {
			called = true
			return nil
		})

		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, called)
	})

	t.Run("one transaction after another each has its own outcome", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)

		require.NoError(t, database.InTx(ctx, db, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha1)
			return err
		}))
		require.Error(t, database.InTx(ctx, db, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha2)
			require.NoError(t, err)
			return errors.New("boom")
		}))

		assert.Equal(t, 1, blobCount(t, db), "the first is there, the second is not")
	})
}

func TestWithTx(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("the queries are those of the transaction, which commits", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)

		err := database.WithTx(ctx, db, func(q *dbq.Queries) error {
			if err := insertBlob(ctx, q, sha1); err != nil {
				return err
			}
			assert.Equal(t, 0, blobCount(t, db), "not there for the others until it is committed")
			return insertBlob(ctx, q, sha2)
		})

		require.NoError(t, err)
		assert.Equal(t, 2, blobCount(t, db))
	})

	t.Run("an error rolls back all of it", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)
		boom := errors.New("boom")

		err := database.WithTx(ctx, db, func(q *dbq.Queries) error {
			require.NoError(t, insertBlob(ctx, q, sha1))
			require.NoError(t, insertBlob(ctx, q, sha2))
			return boom
		})

		assert.Same(t, boom, err)
		assert.Equal(t, 0, blobCount(t, db))
		noTransactionLeft(t, db)
	})

	t.Run("a query that fails is the error that fn gives, and rolls back what came before", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)

		err := database.WithTx(ctx, db, func(q *dbq.Queries) error {
			if err := insertBlob(ctx, q, sha1); err != nil {
				return err
			}
			return insertBlob(ctx, q, sha1) // the same blob again
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "constraint failed")
		assert.Equal(t, 0, blobCount(t, db))
	})

	t.Run("a commit that fails is an error that says so", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		err := database.WithTx(ctx, db, func(q *dbq.Queries) error {
			require.NoError(t, insertBlob(ctx, q, sha1))
			cancel()
			return nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "commit: ")
		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 0, blobCount(t, db))
	})

	t.Run("results are handed out through the closure", func(t *testing.T) {
		t.Parallel()
		db := fileDB(t)

		var got dbq.Blob
		err := database.WithTx(ctx, db, func(q *dbq.Queries) error {
			if err := insertBlob(ctx, q, sha1); err != nil {
				return err
			}
			var err error
			got, err = q.GetBlob(ctx, sha1)
			return err
		})

		require.NoError(t, err)
		assert.Equal(t, sha1, got.Sha256)
	})
}
