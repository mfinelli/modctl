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

package internal

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLatestMigrationVersion(t *testing.T) {
	t.Parallel()

	file := &fstest.MapFile{Data: []byte("-- +goose Up\n")}

	successes := []struct {
		name string
		fsys fstest.MapFS
		want int64
	}{
		{
			"single migration",
			fstest.MapFS{"00001_create_things.sql": file},
			1,
		},
		{
			"several migrations in order",
			fstest.MapFS{
				"00001_a.sql": file,
				"00002_b.sql": file,
				"00003_c.sql": file,
			},
			3,
		},
		{
			"versions are compared as numbers, not as strings",
			fstest.MapFS{
				"9_nine.sql": file,
				"10_ten.sql": file,
			},
			10,
		},
		{
			"versions do not have to be contiguous",
			fstest.MapFS{
				"00001_a.sql": file,
				"00007_b.sql": file,
			},
			7,
		},
		{
			"files that are not sql migrations are ignored",
			fstest.MapFS{
				"00001_a.sql":     file,
				"README.md":       file,
				"00099_notes.txt": file,
			},
			1,
		},
		{
			"subdirectories are ignored",
			fstest.MapFS{
				"00001_a.sql":         file,
				"archive/00200_b.sql": file,
			},
			1,
		},
	}

	for _, tc := range successes {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := latestMigrationVersion(tc.fsys)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	failures := []struct {
		name    string
		fsys    fstest.MapFS
		wantErr string
	}{
		{
			"no files",
			fstest.MapFS{},
			"no migrations found",
		},
		{
			"only files that are not sql migrations",
			fstest.MapFS{"README.md": file},
			"no migrations found",
		},
		{
			"only a subdirectory",
			fstest.MapFS{"archive/00001_a.sql": file},
			"no migrations found",
		},
		{
			"file name without a version separator",
			fstest.MapFS{"00001_a.sql": file, "init.sql": file},
			`"init.sql"`,
		},
		{
			"file name whose version is not a number",
			fstest.MapFS{"abc_init.sql": file},
			`"abc_init.sql"`,
		},
		{
			"version zero",
			fstest.MapFS{"0_zero.sql": file},
			`"0_zero.sql"`,
		},
	}

	for _, tc := range failures {
		tc := tc
		t.Run("error: "+tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := latestMigrationVersion(tc.fsys)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Equal(t, int64(0), got)
		})
	}

	t.Run("error: directory that can not be read", func(t *testing.T) {
		t.Parallel()

		got, err := latestMigrationVersion(os.DirFS(filepath.Join(t.TempDir(), "missing")))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read migrations")
		assert.Equal(t, int64(0), got)
	})
}

// The repository's real migrations must give the same answer that goose
// itself does, otherwise a bundle could be judged against the wrong version.
func TestLatestMigrationVersionMatchesGoose(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS("../migrations")

	got, err := latestMigrationVersion(fsys)
	require.NoError(t, err)
	require.Greater(t, got, int64(0))

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	p, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	require.NoError(t, err)

	var want int64
	for _, src := range p.ListSources() {
		want = max(want, src.Version)
	}

	assert.Equal(t, want, got)
}

func TestLatestSchemaVersion(t *testing.T) {
	t.Parallel()

	t.Run("errors when no migrations are embedded", func(t *testing.T) {
		t.Parallel()

		// Migrations is only populated by main, never in tests
		got, err := LatestSchemaVersion()
		require.Error(t, err)
		assert.Equal(t, int64(0), got)
	})
}
