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

// StoreBuilder builds a store row
type StoreBuilder struct {
	t              *testing.T
	db             *sql.DB
	id             string
	displayName    string
	implementation string
	enabled        bool
}

func NewStore(t *testing.T, db *sql.DB) *StoreBuilder {
	t.Helper()
	return &StoreBuilder{
		t:              t,
		db:             db,
		id:             gofakeit.Word(),
		displayName:    gofakeit.AppName(),
		implementation: "steam",
		enabled:        true,
	}
}

func (b *StoreBuilder) WithID(id string) *StoreBuilder {
	b.id = id
	return b
}

func (b *StoreBuilder) WithImplementation(impl string) *StoreBuilder {
	b.implementation = impl
	return b
}

func (b *StoreBuilder) Build() dbq.Store {
	b.t.Helper()
	q := dbq.New(b.db)
	err := q.UpsertStore(context.Background(), dbq.UpsertStoreParams{
		ID:             b.id,
		DisplayName:    b.displayName,
		Implementation: b.implementation,
		Enabled:        util.SqliteBoolToInt(b.enabled),
	})
	require.NoError(b.t, err, "insert store")

	store, err := q.GetStoreById(context.Background(), b.id)
	require.NoError(b.t, err, "get store after insert")
	return store
}
