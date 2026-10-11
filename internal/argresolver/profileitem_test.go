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
	"testing"

	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileItemByVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("the item of a version in the profile", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		profile := testbuilder.NewProfile(t, f.db).WithGame(f.game).WithName("Mine").Build()
		_, file := f.page(f.game, "Big Mod", "local")
		v1, v2 := f.version(file, "1.0"), f.version(file, "2.0")
		item1 := f.item(profile, v1, 1, true)
		item2 := f.item(profile, v2, 2, true)

		got1, err := ProfileItemByVersion(ctx, f.q, profile, v1)
		require.NoError(t, err)
		got2, err := ProfileItemByVersion(ctx, f.q, profile, v2)
		require.NoError(t, err)

		assert.Equal(t, item1, got1)
		assert.Equal(t, item2, got2, "each version has its own")
	})

	t.Run("an item that is disabled is still the item", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		profile := testbuilder.NewProfile(t, f.db).WithGame(f.game).WithName("Mine").Build()
		_, file := f.page(f.game, "Big Mod", "local")
		v := f.version(file, "1.0")
		item := f.item(profile, v, 1, false)

		got, err := ProfileItemByVersion(ctx, f.q, profile, v)

		require.NoError(t, err)
		assert.Equal(t, item, got)
	})

	t.Run("a version that is not in the profile is an error that says so", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		profile := testbuilder.NewProfile(t, f.db).WithGame(f.game).WithName("Mine").Build()
		_, file := f.page(f.game, "Big Mod", "local")
		v := f.version(file, "1.0")

		_, err := ProfileItemByVersion(ctx, f.q, profile, v)

		require.Error(t, err)
		assert.EqualError(t, err, fmt.Sprintf("version %d is not in profile %q", v, "Mine"))
	})

	t.Run("a version that does not exist is the same error", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		profile := testbuilder.NewProfile(t, f.db).WithGame(f.game).WithName("Mine").Build()

		_, err := ProfileItemByVersion(ctx, f.q, profile, 424242)

		assert.EqualError(t, err, `version 424242 is not in profile "Mine"`)
	})

	t.Run("an item of another profile is not the item of this one", func(t *testing.T) {
		t.Parallel()
		f := newModFixture(t)
		mine := testbuilder.NewProfile(t, f.db).WithGame(f.game).WithName("Mine").Build()
		other := testbuilder.NewProfile(t, f.db).WithGame(f.game).WithName("Other").Build()
		_, file := f.page(f.game, "Big Mod", "local")
		v := f.version(file, "1.0")
		theirs := f.item(other, v, 1, true)

		_, err := ProfileItemByVersion(ctx, f.q, mine, v)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `in profile "Mine"`)

		got, err := ProfileItemByVersion(ctx, f.q, other, v)
		require.NoError(t, err)
		assert.Equal(t, theirs, got)
	})
}
