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

// GameBuilder builds a game_install row, creating a store if one is not provided
type GameBuilder struct {
	t           *testing.T
	db          *sql.DB
	storeID     *string
	storeGameID string
	instanceID  string
	displayName string
	installRoot string
	isPresent   bool
}

func NewGame(t *testing.T, db *sql.DB) *GameBuilder {
	t.Helper()
	return &GameBuilder{
		t:           t,
		db:          db,
		storeGameID: gofakeit.Numerify("#######"),
		instanceID:  "default",
		displayName: gofakeit.AppName(),
		installRoot: "/games/" + gofakeit.Word(),
		isPresent:   true,
	}
}

func (b *GameBuilder) WithStore(storeID string) *GameBuilder {
	b.storeID = &storeID
	return b
}

func (b *GameBuilder) WithName(name string) *GameBuilder {
	b.displayName = name
	return b
}

func (b *GameBuilder) WithStoreGameID(id string) *GameBuilder {
	b.storeGameID = id
	return b
}

func (b *GameBuilder) WithInstanceID(id string) *GameBuilder {
	b.instanceID = id
	return b
}

func (b *GameBuilder) WithInstallRoot(root string) *GameBuilder {
	b.installRoot = root
	return b
}

func (b *GameBuilder) NotPresent() *GameBuilder {
	b.isPresent = false
	return b
}

func (b *GameBuilder) Build() dbq.GameInstall {
	b.t.Helper()

	// Auto-create a store if not provided
	storeID := "steam"
	if b.storeID != nil {
		storeID = *b.storeID
	} else {
		// Ensure the default steam store exists
		NewStore(b.t, b.db).WithID("steam").WithImplementation("steam").Build()
	}

	q := dbq.New(b.db)
	id, err := q.UpsertGameInstall(context.Background(), dbq.UpsertGameInstallParams{
		StoreID:     storeID,
		StoreGameID: b.storeGameID,
		InstanceID:  b.instanceID,
		DisplayName: b.displayName,
		InstallRoot: b.installRoot,
		IsPresent:   util.SqliteBoolToInt(b.isPresent),
		LastSeenAt:  sql.NullString{String: "2026-01-01T00:00:00.000Z", Valid: true},
	})
	require.NoError(b.t, err, "upsert game install")

	// Ensure default profile exists and is active
	err = q.EnsureDefaultProfile(context.Background(), id)
	require.NoError(b.t, err, "ensure default profile")

	gi, err := q.GetGameInstallByID(context.Background(), id)
	require.NoError(b.t, err, "get game install after upsert")
	return gi
}
