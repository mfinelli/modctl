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

package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrateFromFiles is a migrate for openWith that applies the migrations of the
// repository, which are not embedded in the binary of a test.
func migrateFromFiles(ctx context.Context, db *sql.DB) error {
	return repositorySchema().Migrate(ctx, db)
}

// emptyDatabaseFile makes a database file that has nothing in it yet, which is
// what init leaves for the first migration.
func emptyDatabaseFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "modctl.db")
	require.NoError(t, os.WriteFile(path, nil, 0o644))
	return path
}

func TestOpenDB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("a database that is there is opened, and migrated", func(t *testing.T) {
		t.Parallel()

		db, err := openWith(ctx, emptyDatabaseFile(t), migrateFromFiles)
		require.NoError(t, err)
		defer db.Close()

		// the schema is there
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_installs`).Scan(&n))
		assert.Equal(t, 0, n)
	})

	t.Run("foreign keys are enforced on what it returns", func(t *testing.T) {
		t.Parallel()

		db, err := openWith(ctx, emptyDatabaseFile(t), migrateFromFiles)
		require.NoError(t, err)
		defer db.Close()

		var enabled int
		require.NoError(t, db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled))
		assert.Equal(t, 1, enabled)
	})

	t.Run("the database is not configured", func(t *testing.T) {
		t.Parallel()

		db, err := openWith(ctx, "", migrateFromFiles)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "database path is not configured")
		assert.Nil(t, db)
	})

	t.Run("a database that is not there says what to do", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nothing.db")

		db, err := openWith(ctx, path, migrateFromFiles)

		require.Error(t, err)
		assert.Contains(t, err.Error(), path)
		assert.Contains(t, err.Error(), "modctl init")
		assert.Nil(t, db)
		assert.NoFileExists(t, path, "it is not made by looking for it")
	})

	t.Run("a path that is not a file is not a database", func(t *testing.T) {
		t.Parallel()

		db, err := openWith(ctx, t.TempDir(), migrateFromFiles)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a regular file")
		assert.Nil(t, db)
	})

	t.Run("nothing is migrated that is not there", func(t *testing.T) {
		t.Parallel()

		called := false
		_, err := openWith(ctx, filepath.Join(t.TempDir(), "nothing.db"), func(context.Context, *sql.DB) error {
			called = true
			return nil
		})

		require.Error(t, err)
		assert.False(t, called)
	})

	t.Run("migrating is done on the database that is handed over", func(t *testing.T) {
		t.Parallel()

		var migrated *sql.DB
		db, err := openWith(ctx, emptyDatabaseFile(t), func(_ context.Context, db *sql.DB) error {
			migrated = db
			return nil
		})
		require.NoError(t, err)
		defer db.Close()

		assert.Same(t, migrated, db)
		assert.NoError(t, db.PingContext(ctx), "and it is open")
	})

	t.Run("a migration that fails is an error, and the database is closed", func(t *testing.T) {
		t.Parallel()

		boom := errors.New("migration 7 failed")
		var migrated *sql.DB
		db, err := openWith(ctx, emptyDatabaseFile(t), func(_ context.Context, db *sql.DB) error {
			migrated = db
			return boom
		})

		require.ErrorIs(t, err, boom)
		assert.Nil(t, db)
		require.NotNil(t, migrated)
		assert.Error(t, migrated.PingContext(ctx), "it is not left open")
	})

	t.Run("the schema that is given is the one that is migrated to", func(t *testing.T) {
		t.Parallel()

		db, err := repositorySchema().Open(ctx, emptyDatabaseFile(t))

		require.NoError(t, err)
		defer db.Close()
		current, err := repositorySchema().CurrentVersion(ctx, db)
		require.NoError(t, err)
		latest, err := repositorySchema().LatestVersion()
		require.NoError(t, err)
		assert.Equal(t, latest, current)
	})

	t.Run("a schema that has no migrations is an error, and not an empty schema", func(t *testing.T) {
		t.Parallel()

		path := emptyDatabaseFile(t)

		db, err := NewSchema(nil).Open(ctx, path)

		require.Error(t, err)
		assert.Nil(t, db)
		assert.Contains(t, err.Error(), "no migrations are embedded")
	})
}
