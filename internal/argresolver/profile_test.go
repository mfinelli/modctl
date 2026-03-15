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
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveProfileArg(t *testing.T) {
	t.Run("named profile found", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).Build()
		p := testbuilder.NewProfile(t, db).WithGame(gi).WithName("my profile").Build()
		q := dbq.New(db)

		result, err := ResolveProfileArg(context.Background(), q, &gi, "my profile")
		require.NoError(t, err)
		assert.Equal(t, p.ID, result.ID)
		assert.Equal(t, "my profile", result.Name)
	})

	t.Run("named profile not found", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).Build()
		q := dbq.New(db)

		_, err := ResolveProfileArg(context.Background(), q, &gi, "nonexistent")
		assert.ErrorContains(t, err, "nonexistent")
	})

	t.Run("named profile belongs to different game", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi1 := testbuilder.NewGame(t, db).Build()
		gi2 := testbuilder.NewGame(t, db).Build()
		testbuilder.NewProfile(t, db).WithGame(gi2).WithName("other game profile").Build()
		q := dbq.New(db)

		_, err := ResolveProfileArg(context.Background(), q, &gi1, "other game profile")
		assert.ErrorContains(t, err, "other game profile")
	})

	t.Run("empty arg returns active profile", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		// NewGame auto-creates a default active profile via EnsureDefaultProfile
		gi := testbuilder.NewGame(t, db).Build()
		q := dbq.New(db)

		result, err := ResolveProfileArg(context.Background(), q, &gi, "")
		require.NoError(t, err)
		assert.NotZero(t, result.ID)
		assert.Equal(t, int64(1), result.IsActive)
	})

	t.Run("empty arg no active profile", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).Build()
		q := dbq.New(db)

		// Deactivate all profiles for this game
		err := q.DeactivateProfilesForGame(context.Background(), gi.ID)
		require.NoError(t, err)

		_, err = ResolveProfileArg(context.Background(), q, &gi, "")
		assert.ErrorContains(t, err, "no active profile")
	})

	t.Run("active profile when multiple profiles exist", func(t *testing.T) {
		db := testbuilder.SetupDB(t)
		gi := testbuilder.NewGame(t, db).Build()
		// Add a second inactive profile
		inactive := testbuilder.NewProfile(t, db).WithGame(gi).WithName("inactive").Build()
		q := dbq.New(db)

		result, err := ResolveProfileArg(context.Background(), q, &gi, "")
		require.NoError(t, err)
		assert.NotEqual(t, inactive.ID, result.ID)
		assert.Equal(t, int64(1), result.IsActive)
	})
}
