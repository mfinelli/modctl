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

package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mfinelli/modctl/dbq"
)

// CommitError is the error of a transaction that could not be committed: what
// was done in it is not there. It is told apart from the errors of what was
// done in the transaction (which are returned as they are) for the callers that
// have something to do about it, such as marking an operation as failed.
type CommitError struct {
	Err error
}

func (e *CommitError) Error() string {
	return "commit: " + e.Err.Error()
}

func (e *CommitError) Unwrap() error {
	return e.Err
}

// InTx runs fn in a transaction on db. It is committed if fn returns nil, and
// rolled back if fn returns an error (which is returned as it is) or panics (as
// it goes up). A transaction that can't be committed is an error that is a
// *CommitError, and one that can't be started is an error that says so, in
// which case fn is not called.
//
// fn must not use db for what has to be in the transaction: the transaction is
// its own connection, and what is done through db is not part of it.
func InTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Whatever is not committed is rolled back, which is everything when fn
	// fails or panics. After a commit this does nothing (sql.ErrTxDone), and an
	// error from it would not change what the caller has to know.
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return &CommitError{Err: err}
	}

	return nil
}

// WithTx is InTx for what is done with the queries of dbq: fn is given
// the queries for the transaction.
func WithTx(ctx context.Context, db *sql.DB, fn func(q *dbq.Queries) error) error {
	return InTx(ctx, db, func(tx *sql.Tx) error {
		return fn(dbq.New(tx))
	})
}
