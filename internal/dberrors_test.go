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
	"database/sql"
	"errors"
	"fmt"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newConstraintDB returns an in-memory database with a few tables that are
// easy to violate in different ways.
func newConstraintDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err)
	// every connection to :memory: is its own database
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE parent (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		);
		CREATE TABLE child (
			id        INTEGER PRIMARY KEY,
			parent_id INTEGER NOT NULL REFERENCES parent(id)
		);
		CREATE TRIGGER parent_no_forbidden BEFORE INSERT ON parent
		WHEN NEW.name = 'forbidden'
		BEGIN
			SELECT RAISE(ABORT, 'that name is not allowed');
		END;
		INSERT INTO parent (id, name) VALUES (1, 'first');
	`)
	require.NoError(t, err)

	return db
}

func TestConstraintErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// what to do to the database to get the error (nil for no error)
		do         func(db *sql.DB) error
		wantUnique bool
		wantFK     bool
		wantTrig   bool
	}{
		{
			name: "unique violation",
			do: func(db *sql.DB) error {
				_, err := db.Exec(`INSERT INTO parent (name) VALUES ('first')`)
				return err
			},
			wantUnique: true,
		},
		{
			name: "foreign key violation on insert",
			do: func(db *sql.DB) error {
				_, err := db.Exec(`INSERT INTO child (parent_id) VALUES (999)`)
				return err
			},
			wantFK: true,
		},
		{
			name: "foreign key violation on delete",
			do: func(db *sql.DB) error {
				if _, err := db.Exec(`INSERT INTO child (parent_id) VALUES (1)`); err != nil {
					return err
				}
				_, err := db.Exec(`DELETE FROM parent WHERE id = 1`)
				return err
			},
			wantFK: true,
		},
		{
			name: "trigger abort",
			do: func(db *sql.DB) error {
				_, err := db.Exec(`INSERT INTO parent (name) VALUES ('forbidden')`)
				return err
			},
			wantTrig: true,
		},
		{
			name: "primary key violation is not a unique violation",
			do: func(db *sql.DB) error {
				_, err := db.Exec(`INSERT INTO parent (id, name) VALUES (1, 'other')`)
				return err
			},
		},
		{
			name: "not null violation",
			do: func(db *sql.DB) error {
				_, err := db.Exec(`INSERT INTO parent (name) VALUES (NULL)`)
				return err
			},
		},
		{
			name: "syntax error",
			do: func(db *sql.DB) error {
				_, err := db.Exec(`INSERT INTO nowhere VALUES (1)`)
				return err
			},
		},
		{
			name: "no rows",
			do: func(db *sql.DB) error {
				return db.QueryRow(`SELECT id FROM parent WHERE id = 42`).Scan(new(int))
			},
		},
		{
			name: "an error that is not from sqlite",
			do:   func(db *sql.DB) error { return errors.New("unique constraint failed") },
		},
		{
			name: "no error",
			do:   func(db *sql.DB) error { return nil },
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.do(newConstraintDB(t))

			assert.Equal(t, tc.wantUnique, IsUniqueConstraint(err), "IsUniqueConstraint")
			assert.Equal(t, tc.wantFK, IsForeignKeyConstraint(err), "IsForeignKeyConstraint")
			assert.Equal(t, tc.wantTrig, IsTriggerConstraint(err), "IsTriggerConstraint")

			// the same goes for the error once it has been wrapped, which is
			// how it usually reaches the caller
			if err != nil {
				wrapped := fmt.Errorf("insert thing: %w", err)
				assert.Equal(t, tc.wantUnique, IsUniqueConstraint(wrapped), "wrapped IsUniqueConstraint")
				assert.Equal(t, tc.wantFK, IsForeignKeyConstraint(wrapped), "wrapped IsForeignKeyConstraint")
				assert.Equal(t, tc.wantTrig, IsTriggerConstraint(wrapped), "wrapped IsTriggerConstraint")
			}
		})
	}
}
