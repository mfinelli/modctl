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

package restore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/exporter"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fileDatabase is a database in a file, with the migrations applied (what a
// full restore replaces, and what a bundle has).
func fileDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", database.DSN(path, false))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = testbuilder.NewProvider(t, db).Up(context.Background())
	require.NoError(t, err)
	return db
}

func gameNames(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.Query(`SELECT display_name FROM game_installs ORDER BY display_name`)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	return names
}

// fullRestoreFixture is the database that is there (in a file, as it is in
// real life, with a game in it) and a bundle (with another game in it).
type fullRestoreFixture struct {
	dbPath string
	db     *sql.DB
	bundle *Bundle
}

func newFullRestoreFixture(t *testing.T) fullRestoreFixture {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "modctl.db")
	db := fileDatabase(t, dbPath)
	newBundleGame(t, db, "1").rename("The game that was there")

	bundleDir := t.TempDir()
	bundleDB := fileDatabase(t, filepath.Join(bundleDir, exporter.DatabaseFilename))
	inBundle := newBundleGame(t, bundleDB, "2")
	inBundle.rename("The game of the bundle")
	// what a bundle has is a snapshot that is in one file, which is not what a
	// database that is in use is until what is in its log has been written to it
	_, err := bundleDB.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	require.NoError(t, err)
	bundle := testBundle(t, bundleDB, inBundle.game, exporter.ExportKindFull)
	bundle.BundleDir = bundleDir

	return fullRestoreFixture{dbPath: dbPath, db: db, bundle: bundle}
}

func TestFullReplacesTheDatabase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newFullRestoreFixture(t)

	_, err := Full(ctx, f.db, dbq.New(f.db), blobstore.Store{}, f.bundle, Options{Force: true}, f.dbPath, "test", quietLogger())

	require.NoError(t, err)
	restored, err := sql.Open("sqlite3", database.DSN(f.dbPath, true))
	require.NoError(t, err)
	defer restored.Close()
	assert.Equal(t, []string{"The game of the bundle"}, gameNames(t, restored), "it is the database of the bundle")
	assert.NoFileExists(t, f.dbPath+".restoring", "the copy was moved into place")
}

func TestFullThatCannotCopyTheBundleLeavesTheDatabase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f := newFullRestoreFixture(t)
	// a bundle that is cut short: its database is not there to be copied
	require.NoError(t, f.bundle.BundleDB.Close())
	require.NoError(t, os.Remove(filepath.Join(f.bundle.BundleDir, exporter.DatabaseFilename)))
	f.bundle.BundleDB = testbuilder.SetupDB(t) // what it is checked with is not what is wrong

	_, err := Full(ctx, f.db, dbq.New(f.db), blobstore.Store{}, f.bundle, Options{Force: true}, f.dbPath, "test", quietLogger())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "restore database")
	assert.FileExists(t, f.dbPath, "the database that was there is gone")
	assert.NoFileExists(t, f.dbPath+".restoring")
	// and it is the same database, which is still open and works
	assert.Equal(t, []string{"The game that was there"}, gameNames(t, f.db))
	other, err := sql.Open("sqlite3", database.DSN(f.dbPath, true))
	require.NoError(t, err)
	defer other.Close()
	assert.Equal(t, []string{"The game that was there"}, gameNames(t, other), "in the file too")
}
