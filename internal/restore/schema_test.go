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
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullNeedsASchema(t *testing.T) {
	t.Parallel()

	db := testbuilder.SetupDB(t)

	res, err := Full(context.Background(), db, dbq.New(db), blobstore.Store{}, nil, Options{Force: true}, "", "test", quietLogger())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no schema")
	assert.Equal(t, Result{}, res)
}

func TestGameNeedsASchema(t *testing.T) {
	t.Parallel()

	db := testbuilder.SetupDB(t)

	res, err := Game(context.Background(), db, dbq.New(db), blobstore.Store{}, nil, Options{Force: true}, "test", quietLogger())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no schema")
	assert.Equal(t, Result{}, res)
}
