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
	"os"
	"testing"

	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/testbuilder"
)

// The restores migrate the databases that they make, with the migrations that
// are embedded in the binary, which a test does not have. They are the ones of
// the repository for all of the tests, set before any of them runs: they are
// not set by each test, since that would change something that is global while
// the other tests are using it.
func TestMain(m *testing.M) {
	database.Migrations = testbuilder.MigrationsFS()
	os.Exit(m.Run())
}
