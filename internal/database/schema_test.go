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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repositorySchema is the schema of the repository: the migrations that are
// embedded in the binary, which a test does not have, are the ones in the
// directory that they are in. Every test makes the one that it uses, so none of
// them is anything but what it is given.
func repositorySchema() *Schema {
	return NewSchema(os.DirFS("../.."))
}

// newFileDB is an empty database in a file of the test.
func newFileDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", DSN(filepath.Join(t.TempDir(), "modctl.db"), false))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSchemaMigrate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("a database is brought to the latest migration of the schema", func(t *testing.T) {
		t.Parallel()

		db := newFileDB(t)
		schema := repositorySchema()

		require.NoError(t, schema.Migrate(ctx, db))

		current, err := schema.CurrentVersion(ctx, db)
		require.NoError(t, err)
		latest, err := schema.LatestVersion()
		require.NoError(t, err)
		assert.Equal(t, latest, current)
	})

	t.Run("a database that is migrated is migrated again as it is", func(t *testing.T) {
		t.Parallel()

		db := newFileDB(t)
		schema := repositorySchema()
		require.NoError(t, schema.Migrate(ctx, db))

		assert.NoError(t, schema.Migrate(ctx, db))
	})

	t.Run("a schema that has no migrations is an error that says so, and migrates nothing", func(t *testing.T) {
		t.Parallel()

		db := newFileDB(t)

		err := NewSchema(nil).Migrate(ctx, db)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no migrations are embedded")
		var tables int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables))
		assert.Zero(t, tables)
	})
}

func TestSchemaCurrentVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("a database that has not been migrated is at the start", func(t *testing.T) {
		t.Parallel()

		got, err := repositorySchema().CurrentVersion(ctx, newFileDB(t))

		require.NoError(t, err)
		assert.Equal(t, int64(0), got)
	})

	t.Run("a database is at the version that it was migrated to", func(t *testing.T) {
		t.Parallel()

		db := newFileDB(t)
		schema := repositorySchema()
		p, err := schema.Provider(db)
		require.NoError(t, err)
		first, err := p.UpByOne(ctx)
		require.NoError(t, err)

		got, err := schema.CurrentVersion(ctx, db)

		require.NoError(t, err)
		assert.Equal(t, first.Source.Version, got, "and not the latest one")
	})

	t.Run("a schema that has no migrations is an error that says so", func(t *testing.T) {
		t.Parallel()

		_, err := NewSchema(nil).CurrentVersion(ctx, newFileDB(t))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no migrations are embedded")
	})
}

func TestSchemaProvider(t *testing.T) {
	t.Parallel()

	t.Run("it has the migrations of the schema", func(t *testing.T) {
		t.Parallel()

		p, err := repositorySchema().Provider(newFileDB(t))

		require.NoError(t, err)
		sources := p.ListSources()
		require.NotEmpty(t, sources)
		latest, err := repositorySchema().LatestVersion()
		require.NoError(t, err)
		assert.Equal(t, latest, sources[len(sources)-1].Version)
	})

	t.Run("a schema that has no migrations is an error that says so", func(t *testing.T) {
		t.Parallel()

		p, err := NewSchema(nil).Provider(newFileDB(t))

		require.Error(t, err)
		assert.Nil(t, p)
		assert.Contains(t, err.Error(), "no migrations are embedded")
	})

	t.Run("two schemas are their own, and one that has none does not take the other's", func(t *testing.T) {
		t.Parallel()

		with, without := repositorySchema(), NewSchema(nil)

		_, errWith := with.LatestVersion()
		_, errWithout := without.LatestVersion()

		assert.NoError(t, errWith)
		assert.Error(t, errWithout)
	})
}
