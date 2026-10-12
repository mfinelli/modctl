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

package argresolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplicitGameInstall(t *testing.T) {
	t.Run("numeric id", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).WithName("Cyberpunk 2077").Build()
		q := dbq.New(db)

		result, err := ExplicitGameInstall(context.Background(), q, fmt.Sprintf("%d", gi.ID))
		require.NoError(t, err)
		assert.Equal(t, gi.ID, result.ID)
		assert.Equal(t, "Cyberpunk 2077", result.DisplayName)
	})

	t.Run("numeric id not found", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		q := dbq.New(db)

		_, err := ExplicitGameInstall(context.Background(), q, "99999")
		assert.ErrorContains(t, err, "99999")
	})

	t.Run("full selector", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).
			WithStoreGameID("1091500").
			WithInstanceID("default").
			Build()
		q := dbq.New(db)

		result, err := ExplicitGameInstall(context.Background(), q, "steam:1091500#default")
		require.NoError(t, err)
		assert.Equal(t, gi.ID, result.ID)
	})

	t.Run("short selector without instance", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).WithStoreGameID("1091500").Build()
		q := dbq.New(db)

		result, err := ExplicitGameInstall(context.Background(), q, "steam:1091500")
		require.NoError(t, err)
		assert.Equal(t, gi.ID, result.ID)
	})

	t.Run("selector with explicit instance not found", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		q := dbq.New(db)

		_, err := ExplicitGameInstall(context.Background(), q, "steam:1091500#library_2")
		assert.ErrorContains(t, err, "steam:1091500#library_2")
	})

	t.Run("selector ambiguous multiple instances", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		// Both installs have non-default instance IDs so the default lookup misses
		testbuilder.NewGame(t, db).WithStoreGameID("1091500").WithInstanceID("library_2").Build()
		testbuilder.NewGame(t, db).WithStoreGameID("1091500").WithInstanceID("library_3").Build()
		q := dbq.New(db)

		_, err := ExplicitGameInstall(context.Background(), q, "steam:1091500")
		assert.ErrorContains(t, err, "Multiple installs found")
		assert.ErrorContains(t, err, "steam:1091500#library_2")
		assert.ErrorContains(t, err, "steam:1091500#library_3")
	})

	t.Run("name exact match", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).WithName("Cyberpunk 2077").Build()
		q := dbq.New(db)

		result, err := ExplicitGameInstall(context.Background(), q, "Cyberpunk 2077")
		require.NoError(t, err)
		assert.Equal(t, gi.ID, result.ID)
	})

	t.Run("name case insensitive", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).WithName("Cyberpunk 2077").Build()
		q := dbq.New(db)

		result, err := ExplicitGameInstall(context.Background(), q, "cyberpunk 2077")
		require.NoError(t, err)
		assert.Equal(t, gi.ID, result.ID)
	})

	t.Run("name with colon falls through to name search", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).WithName("My Game: The Sequel").Build()
		q := dbq.New(db)

		result, err := ExplicitGameInstall(context.Background(), q, "My Game: The Sequel")
		require.NoError(t, err)
		assert.Equal(t, gi.ID, result.ID)
	})

	t.Run("name not found", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		q := dbq.New(db)

		_, err := ExplicitGameInstall(context.Background(), q, "Nonexistent Game")
		assert.ErrorContains(t, err, "Nonexistent Game")
	})

	t.Run("name ambiguous multiple games", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		// Two different games with the same display name under different store game IDs
		testbuilder.NewGame(t, db).WithName("Cyberpunk 2077").WithStoreGameID("1091500").Build()
		testbuilder.NewGame(t, db).WithName("Cyberpunk 2077").WithStoreGameID("9999999").Build()
		q := dbq.New(db)

		_, err := ExplicitGameInstall(context.Background(), q, "Cyberpunk 2077")
		assert.ErrorContains(t, err, "Multiple installs found")
	})

	t.Run("name matches missing install", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).WithName("Cyberpunk 2077").NotPresent().Build()
		q := dbq.New(db)

		result, err := ExplicitGameInstall(context.Background(), q, "Cyberpunk 2077")
		require.NoError(t, err)
		assert.Equal(t, gi.ID, result.ID)
	})

	t.Run("non-ascii name", func(t *testing.T) {
		t.Parallel()
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).WithName("モンスターハンター").Build()
		q := dbq.New(db)

		result, err := ExplicitGameInstall(context.Background(), q, "モンスターハンター")
		require.NoError(t, err)
		assert.Equal(t, gi.ID, result.ID)
	})
}

func TestGameInstallIn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("a game that is asked for is the one that is resolved", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		db := testbuilder.SetupDB(t)
		q := dbq.New(db)
		asked := testbuilder.NewGame(t, db).WithName("Cyberpunk 2077").WithStoreGameID("1").Build()
		active := testbuilder.NewGame(t, db).WithName("Skyrim").WithStoreGameID("2").Build()
		require.NoError(t, state.SaveActiveIn(dir, state.Active{ActiveGameInstallID: active.ID}))

		byID, err := GameInstallIn(ctx, q, dir, fmt.Sprintf("%d", asked.ID))
		require.NoError(t, err)
		assert.Equal(t, asked.ID, byID.ID, "not the active one")

		byName, err := GameInstallIn(ctx, q, dir, "Cyberpunk 2077")
		require.NoError(t, err)
		assert.Equal(t, asked.ID, byName.ID)
	})

	t.Run("a game that is asked for and is not there is an error that says so", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		db := testbuilder.SetupDB(t)
		active := testbuilder.NewGame(t, db).Build()
		require.NoError(t, state.SaveActiveIn(dir, state.Active{ActiveGameInstallID: active.ID}))

		_, err := GameInstallIn(ctx, dbq.New(db), dir, "999999")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no game install with id 999999")
	})

	t.Run("without one it is the active game", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		db := testbuilder.SetupDB(t)
		q := dbq.New(db)
		testbuilder.NewGame(t, db).WithName("Not Active").WithStoreGameID("1").Build()
		active := testbuilder.NewGame(t, db).WithName("Active").WithStoreGameID("2").Build()
		require.NoError(t, state.SaveActiveIn(dir, state.Active{ActiveGameInstallID: active.ID}))

		got, err := GameInstallIn(ctx, q, dir, "")

		require.NoError(t, err)
		assert.Equal(t, active.ID, got.ID)
		assert.Equal(t, "Active", got.DisplayName)
	})

	t.Run("with no active game it says what to do", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		db := testbuilder.SetupDB(t)
		testbuilder.NewGame(t, db).Build()

		_, err := GameInstallIn(ctx, dbq.New(db), dir, "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no active game selected")
		assert.Contains(t, err.Error(), "modctl games set-active")
		assert.Contains(t, err.Error(), "--game")
	})

	t.Run("an active selection that has no game in it is no active game", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		db := testbuilder.SetupDB(t)
		// a store has been chosen, but not a game
		require.NoError(t, state.SaveActiveIn(dir, state.Active{ActiveStoreID: "steam"}))

		_, err := GameInstallIn(ctx, dbq.New(db), dir, "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no active game selected")
	})

	t.Run("an active game that is not there any more is an error that says so", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		db := testbuilder.SetupDB(t)
		require.NoError(t, state.SaveActiveIn(dir, state.Active{ActiveGameInstallID: 424242}))

		_, err := GameInstallIn(ctx, dbq.New(db), dir, "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no game install with id 424242")
	})

	t.Run("a state file that can't be read is an error that says so", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		db := testbuilder.SetupDB(t)
		testbuilder.NewGame(t, db).Build()
		path := filepath.Join(dir, "active.json")
		require.NoError(t, os.WriteFile(path, []byte("{ not json"), 0o644))

		_, err := GameInstallIn(ctx, dbq.New(db), dir, "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "load active selection")
		assert.Contains(t, err.Error(), "active.json")
	})

	t.Run("a state file that can't be read is not looked at when a game is asked for", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).Build()
		path := filepath.Join(dir, "active.json")
		require.NoError(t, os.WriteFile(path, []byte("{ not json"), 0o644))

		got, err := GameInstallIn(ctx, dbq.New(db), dir, fmt.Sprintf("%d", gi.ID))

		require.NoError(t, err)
		assert.Equal(t, gi.ID, got.ID)
	})
}
