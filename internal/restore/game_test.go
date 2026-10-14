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
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/exporter"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRow(query, args...).Scan(&n))
	return n
}

func gameInstallID(t *testing.T, q *dbq.Queries, game dbq.GameInstall) (int64, error) {
	t.Helper()

	return q.ImportGetGameInstallByStoreKey(context.Background(), dbq.ImportGetGameInstallByStoreKeyParams{
		StoreID:     game.StoreID,
		StoreGameID: game.StoreGameID,
		InstanceID:  game.InstanceID,
	})
}

func TestGameImportsABundle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	dest := testbuilder.SetupDB(t)
	bundleDB := testbuilder.SetupDB(t)
	inBundle := newBundleGame(t, bundleDB, "1")
	bundle := testBundle(t, bundleDB, inBundle.game, exporter.ExportKindGame)

	res, err := Game(ctx, dest, dbq.New(dest), blobstore.Store{}, bundle, Options{Schema: testbuilder.Schema(), SkipInventory: true}, "test", quietLogger())

	require.NoError(t, err)
	newID, err := gameInstallID(t, dbq.New(dest), inBundle.game)
	require.NoError(t, err, "the game is there")
	assert.Equal(t, 1, count(t, dest, `SELECT COUNT(*) FROM game_installs`))
	assert.Equal(t,
		count(t, bundleDB, `SELECT COUNT(*) FROM targets WHERE game_install_id = ?`, inBundle.game.ID),
		count(t, dest, `SELECT COUNT(*) FROM targets WHERE game_install_id = ?`, newID), "with its targets")
	assert.Equal(t,
		count(t, bundleDB, `SELECT COUNT(*) FROM profiles WHERE game_install_id = ?`, inBundle.game.ID),
		count(t, dest, `SELECT COUNT(*) FROM profiles WHERE game_install_id = ?`, newID), "and its profiles")
	assert.Positive(t, res.Profiles)
}

func TestGameWithAGameThatIsThere(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	newBundle := func(t *testing.T) (dest *sql.DB, existing *bundleGame, bundle *Bundle, inBundle *bundleGame) {
		dest = testbuilder.SetupDB(t)
		existing = newBundleGame(t, dest, "1")
		existing.rename("The game that was there")
		existing.override("my-own.cfg")
		bundleDB := testbuilder.SetupDB(t)
		inBundle = newBundleGame(t, bundleDB, "1")
		inBundle.rename("The game of the bundle")
		return dest, existing, testBundle(t, bundleDB, inBundle.game, exporter.ExportKindGame), inBundle
	}

	t.Run("it is not replaced without --force", func(t *testing.T) {
		t.Parallel()

		dest, existing, bundle, _ := newBundle(t)

		_, err := Game(ctx, dest, existing.q, blobstore.Store{}, bundle, Options{Schema: testbuilder.Schema(), SkipInventory: true}, "test", quietLogger())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists; use --force")
		id, getErr := gameInstallID(t, existing.q, existing.game)
		require.NoError(t, getErr)
		assert.Equal(t, existing.game.ID, id, "it is the game that was there")
		assert.Equal(t, 1, count(t, dest, `SELECT COUNT(*) FROM overrides WHERE relpath = 'my-own.cfg'`))
	})

	t.Run("it is replaced with --force", func(t *testing.T) {
		t.Parallel()

		dest, existing, bundle, inBundle := newBundle(t)

		_, err := Game(ctx, dest, existing.q, blobstore.Store{}, bundle, Options{Schema: testbuilder.Schema(), Force: true, SkipInventory: true}, "test", quietLogger())

		require.NoError(t, err)
		id, getErr := gameInstallID(t, existing.q, inBundle.game)
		require.NoError(t, getErr)
		var name string
		require.NoError(t, dest.QueryRow(`SELECT display_name FROM game_installs WHERE id = ?`, id).Scan(&name))
		assert.Equal(t, "The game of the bundle", name, "it is the game of the bundle")
		assert.Equal(t, 1, count(t, dest, `SELECT COUNT(*) FROM game_installs`))
		assert.Equal(t, 0, count(t, dest, `SELECT COUNT(*) FROM overrides WHERE relpath = 'my-own.cfg'`), "what it had went with it")
	})
}

func TestGameThatCannotBeImportedToTheEndChangesNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// the game is there, with something of its own
	dest := testbuilder.SetupDB(t)
	existing := newBundleGame(t, dest, "1")
	existing.rename("The game that was there")
	existing.override("my-own.cfg")
	before := map[string]int{
		"game installs": count(t, dest, `SELECT COUNT(*) FROM game_installs`),
		"targets":       count(t, dest, `SELECT COUNT(*) FROM targets`),
		"profiles":      count(t, dest, `SELECT COUNT(*) FROM profiles`),
		"overrides":     count(t, dest, `SELECT COUNT(*) FROM overrides`),
		"blobs":         count(t, dest, `SELECT COUNT(*) FROM blobs`),
	}

	// The bundle has an override that is fine, whose blob is not in the bundle
	// (a bundle that was cut short): this is only found out at the end of what is
	// imported, after the game that was there is replaced
	bundleDB := testbuilder.SetupDB(t)
	inBundle := newBundleGame(t, bundleDB, "1")
	inBundle.override("user.cfg")
	bundle := testBundle(t, bundleDB, inBundle.game, exporter.ExportKindGame)

	_, err := Game(ctx, dest, existing.q, blobstore.Store{}, bundle, Options{Schema: testbuilder.Schema(), Force: true, SkipInventory: true}, "test", quietLogger())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "import overrides", "that is where it failed")
	assert.NotErrorIs(t, err, ErrUnsafePaths)

	id, getErr := gameInstallID(t, existing.q, existing.game)
	require.NoError(t, getErr, "the game that was there was deleted")
	var name string
	require.NoError(t, dest.QueryRow(`SELECT display_name FROM game_installs WHERE id = ?`, id).Scan(&name))
	assert.Equal(t, "The game that was there", name, "it was replaced by one that is not whole")
	after := map[string]int{
		"game installs": count(t, dest, `SELECT COUNT(*) FROM game_installs`),
		"targets":       count(t, dest, `SELECT COUNT(*) FROM targets`),
		"profiles":      count(t, dest, `SELECT COUNT(*) FROM profiles`),
		"overrides":     count(t, dest, `SELECT COUNT(*) FROM overrides`),
		"blobs":         count(t, dest, `SELECT COUNT(*) FROM blobs`),
	}
	assert.Equal(t, before, after)
	assert.Equal(t, 1, count(t, dest, `SELECT COUNT(*) FROM overrides WHERE relpath = 'my-own.cfg'`))
}
