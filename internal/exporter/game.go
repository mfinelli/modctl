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

package exporter

import (
	"context"
	"database/sql"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
)

// Game performs a game-scoped export containing only data relevant to a
// single game install.
func Game(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	bs blobstore.Store,
	gi dbq.GameInstall,
	opts Options,
) error {
	return nil
}
