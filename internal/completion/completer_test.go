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

package completion

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/mfinelli/modctl/internal/state"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCompleter is a Completer for a database and a state directory of the
// test, and the database for what the test puts in it.
func newCompleter(t *testing.T) (Completer, *sql.DB) {
	t.Helper()

	db, path := testbuilder.SetupDBFile(t)
	return Completer{DatabasePath: path, StateDir: t.TempDir()}, db
}

// commandWithGame is a command that has the --game flag, set to game unless
// that is empty.
func commandWithGame(t *testing.T, game string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int64("game", 0, "")
	if game != "" {
		require.NoError(t, cmd.Flags().Set("game", game))
	}
	return cmd
}

func TestCompleterGameInstallSelectors(t *testing.T) {
	t.Parallel()

	t.Run("by title the title is completed, and the selector says which", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		testbuilder.NewGame(t, db).WithName("Skyrim").WithStoreGameID("72850").Build()
		testbuilder.NewGame(t, db).WithName("Fallout 4").WithStoreGameID("377160").Build()

		got, directive := c.GameInstallSelectors(&cobra.Command{}, "sky")

		assert.Equal(t, []string{"Skyrim\tsteam:72850#default"}, got)
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	})

	t.Run("by selector the selector is completed, and the title says which", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		testbuilder.NewGame(t, db).WithName("Skyrim").WithStoreGameID("72850").Build()
		testbuilder.NewGame(t, db).WithName("Fallout 4").WithStoreGameID("377160").Build()

		got, _ := c.GameInstallSelectors(&cobra.Command{}, "steam:728")

		assert.Equal(t, []string{"steam:72850#default\tSkyrim"}, got)
	})

	t.Run("a game that is not there says so, and is after the ones that are", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		testbuilder.NewGame(t, db).WithName("A gone game").WithStoreGameID("1").NotPresent().Build()
		testbuilder.NewGame(t, db).WithName("B game").WithStoreGameID("2").Build()

		got, _ := c.GameInstallSelectors(&cobra.Command{}, "")

		assert.Equal(t, []string{
			"B game\tsteam:2#default",
			"A gone game\tsteam:1#default (missing)",
		}, got)
	})

	t.Run("what is typed is not a pattern", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		testbuilder.NewGame(t, db).WithName("Skyrim").Build()

		for _, typed := range []string{"%", "_", `\`} {
			got, _ := c.GameInstallSelectors(&cobra.Command{}, typed)
			assert.Empty(t, got, "%q", typed)
		}
	})

	t.Run("with no database there is nothing to complete, and files are not", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nothing.db")
		c := Completer{DatabasePath: path, StateDir: t.TempDir()}

		got, directive := c.GameInstallSelectors(&cobra.Command{}, "")

		assert.Empty(t, got)
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
		assert.NoFileExists(t, path, "it is not made by looking for it")
	})
}

func TestCompleterStoreIDs(t *testing.T) {
	t.Parallel()

	t.Run("the stores that are enabled are completed, with their names", func(t *testing.T) {
		t.Parallel()

		c, _ := newCompleter(t)

		got, directive := c.StoreIDs(&cobra.Command{}, "")

		assert.Equal(t, []string{"steam\tSteam"}, got)
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	})

	t.Run("a prefix is matched whatever the case", func(t *testing.T) {
		t.Parallel()

		c, _ := newCompleter(t)

		upper, _ := c.StoreIDs(&cobra.Command{}, "STE")
		other, _ := c.StoreIDs(&cobra.Command{}, "gog")

		assert.Equal(t, []string{"steam\tSteam"}, upper)
		assert.Empty(t, other)
	})

	t.Run("a store that is not enabled is not completed", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		_, err := db.Exec(`UPDATE stores SET enabled = FALSE WHERE id = 'steam'`)
		require.NoError(t, err)

		got, _ := c.StoreIDs(&cobra.Command{}, "")

		assert.Empty(t, got)
	})

	t.Run("with no database there is nothing to complete", func(t *testing.T) {
		t.Parallel()

		c := Completer{DatabasePath: filepath.Join(t.TempDir(), "nothing.db"), StateDir: t.TempDir()}

		got, directive := c.StoreIDs(&cobra.Command{}, "")

		assert.Empty(t, got)
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	})
}

func TestCompleterProfileNames(t *testing.T) {
	t.Parallel()

	t.Run("without --game it is the profiles of the active game, the active one first", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		active := testbuilder.NewGame(t, db).WithName("Active").WithStoreGameID("1").Build()
		testbuilder.NewProfile(t, db).WithGame(active).WithName("Hardcore").Build()
		other := testbuilder.NewGame(t, db).WithName("Other").WithStoreGameID("2").Build()
		testbuilder.NewProfile(t, db).WithGame(other).WithName("Of the other game").Build()
		require.NoError(t, state.SaveActive(c.StateDir, state.Active{ActiveGameInstallID: active.ID}))

		got, directive := c.ProfileNames(commandWithGame(t, ""), "")

		assert.Equal(t, []string{"default\tactive", "Hardcore"}, got)
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	})

	t.Run("with --game it is the profiles of that game", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		active := testbuilder.NewGame(t, db).WithName("Active").WithStoreGameID("1").Build()
		other := testbuilder.NewGame(t, db).WithName("Other").WithStoreGameID("2").Build()
		testbuilder.NewProfile(t, db).WithGame(other).WithName("Of the other game").Build()
		require.NoError(t, state.SaveActive(c.StateDir, state.Active{ActiveGameInstallID: active.ID}))

		got, _ := c.ProfileNames(commandWithGame(t, "2"), "of")

		assert.Equal(t, []string{"Of the other game"}, got, "and not the one of the active game")
	})

	t.Run("a prefix is matched whatever the case", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		active := testbuilder.NewGame(t, db).Build()
		testbuilder.NewProfile(t, db).WithGame(active).WithName("Hardcore").Build()
		require.NoError(t, state.SaveActive(c.StateDir, state.Active{ActiveGameInstallID: active.ID}))

		got, _ := c.ProfileNames(commandWithGame(t, ""), "HARD")

		assert.Equal(t, []string{"Hardcore"}, got)
	})

	t.Run("without an active game, or a --game, there is nothing to complete", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		testbuilder.NewGame(t, db).Build()

		got, directive := c.ProfileNames(commandWithGame(t, ""), "")

		assert.Empty(t, got)
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	})

	t.Run("an active selection that can't be read is nothing to complete", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		testbuilder.NewGame(t, db).Build()
		require.NoError(t, os.WriteFile(filepath.Join(c.StateDir, "active.json"), []byte("{ not json"), 0o644))

		got, directive := c.ProfileNames(commandWithGame(t, ""), "")

		assert.Empty(t, got)
		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	})

	t.Run("a --game that is not a game is nothing to complete", func(t *testing.T) {
		t.Parallel()

		c, db := newCompleter(t)
		active := testbuilder.NewGame(t, db).Build()
		require.NoError(t, state.SaveActive(c.StateDir, state.Active{ActiveGameInstallID: active.ID}))

		got, _ := c.ProfileNames(commandWithGame(t, "-5"), "")

		assert.Empty(t, got, "the active game is not what it falls back to")
	})

	t.Run("with no database there is nothing to complete", func(t *testing.T) {
		t.Parallel()

		c := Completer{DatabasePath: filepath.Join(t.TempDir(), "nothing.db"), StateDir: t.TempDir()}

		got, _ := c.ProfileNames(commandWithGame(t, "1"), "")

		assert.Empty(t, got)
	})
}
