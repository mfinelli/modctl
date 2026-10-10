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

package internal

import (
	"errors"

	"github.com/mattn/go-sqlite3"
)

// IsUniqueConstraint reports whether err is SQLite complaining that a UNIQUE
// constraint (or unique index) would have been violated. A PRIMARY KEY
// violation is a different error and is not matched.
func IsUniqueConstraint(err error) bool {
	return isConstraint(err, sqlite3.ErrConstraintUnique)
}

// IsForeignKeyConstraint reports whether err is SQLite complaining that a
// FOREIGN KEY constraint would have been violated: a row that refers to
// something that isn't there, or the deletion of something that is still
// referred to.
func IsForeignKeyConstraint(err error) bool {
	return isConstraint(err, sqlite3.ErrConstraintForeignKey)
}

// IsTriggerConstraint reports whether err is a trigger of ours stopping the
// statement with RAISE(ABORT, ...) and the like.
func IsTriggerConstraint(err error) bool {
	return isConstraint(err, sqlite3.ErrConstraintTrigger)
}

// isConstraint reports whether err is a SQLite constraint error of the given
// kind, however deeply it is wrapped.
func isConstraint(err error, kind sqlite3.ErrNoExtended) bool {
	var se sqlite3.Error

	return errors.As(err, &se) &&
		se.Code == sqlite3.ErrConstraint &&
		se.ExtendedCode == kind
}
