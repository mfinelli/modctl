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

package testbuilder

import (
	"context"
	"database/sql"
	"testing"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/mfinelli/modctl/dbq"
	"github.com/stretchr/testify/require"
	"go.finelli.dev/util"
)

// ProfileBuilder builds a profile row for a game install.
type ProfileBuilder struct {
	t             *testing.T
	db            *sql.DB
	gameInstallID *int64
	name          string
	isActive      bool
}

func NewProfile(t *testing.T, db *sql.DB) *ProfileBuilder {
	t.Helper()
	return &ProfileBuilder{
		t:        t,
		db:       db,
		name:     gofakeit.Word(),
		isActive: false,
	}
}

func (b *ProfileBuilder) WithGame(gi dbq.GameInstall) *ProfileBuilder {
	b.gameInstallID = &gi.ID
	return b
}

func (b *ProfileBuilder) WithName(name string) *ProfileBuilder {
	b.name = name
	return b
}

func (b *ProfileBuilder) Active() *ProfileBuilder {
	b.isActive = true
	return b
}

func (b *ProfileBuilder) Build() dbq.Profile {
	b.t.Helper()

	// Auto-create a game install if not provided
	gameInstallID := int64(0)
	if b.gameInstallID != nil {
		gameInstallID = *b.gameInstallID
	} else {
		gi := NewGame(b.t, b.db).Build()
		gameInstallID = gi.ID
	}

	q := dbq.New(b.db)
	id, err := q.CreateProfile(context.Background(), dbq.CreateProfileParams{
		GameInstallID: gameInstallID,
		Name:          b.name,
		IsActive:      util.SqliteBoolToInt(b.isActive),
	})
	require.NoError(b.t, err, "create profile")

	if b.isActive {
		err = q.ActivateProfileByName(context.Background(), dbq.ActivateProfileByNameParams{
			Name:          b.name,
			GameInstallID: gameInstallID,
		})
		require.NoError(b.t, err, "set active profile")
	}

	p, err := q.GetProfileByID(context.Background(), id)
	require.NoError(b.t, err, "get profile after create")
	return p
}
