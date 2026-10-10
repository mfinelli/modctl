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

package internal_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newUnmigratedDB returns an empty in-memory database and a provider that
// reads the repository's migrations from the filesystem.
func newUnmigratedDB(t *testing.T) *goose.Provider {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err)
	// every connection to :memory: is its own database
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	return testbuilder.NewProvider(t, db)
}

func TestSchemaVersion(t *testing.T) {
	t.Parallel()

	t.Run("new database is at version 0", func(t *testing.T) {
		t.Parallel()

		p := newUnmigratedDB(t)

		got, err := internal.SchemaVersion(context.Background(), p)
		require.NoError(t, err)
		assert.Equal(t, int64(0), got)
	})

	t.Run("fully migrated database is at the latest version", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		p := newUnmigratedDB(t)

		_, err := p.Up(ctx)
		require.NoError(t, err)

		_, latest, err := p.GetVersions(ctx)
		require.NoError(t, err)
		require.Greater(t, latest, int64(0))

		got, err := internal.SchemaVersion(ctx, p)
		require.NoError(t, err)
		assert.Equal(t, latest, got)
	})

	t.Run("partially migrated database reports what is applied, not the latest", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		p := newUnmigratedDB(t)

		sources := p.ListSources()
		require.GreaterOrEqual(t, len(sources), 2)
		first := sources[0].Version

		_, err := p.UpTo(ctx, first)
		require.NoError(t, err)

		_, latest, err := p.GetVersions(ctx)
		require.NoError(t, err)
		require.Greater(t, latest, first)

		got, err := internal.SchemaVersion(ctx, p)
		require.NoError(t, err)
		assert.Equal(t, first, got)
	})
}
