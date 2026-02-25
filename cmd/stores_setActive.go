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

package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"go.finelli.dev/util"
)

var setActiveCmd = &cobra.Command{
	Use:   "set-active",
	Short: "Set the active store",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		err := internal.EnsureDBExists()
		if err != nil {
			return err
		}

		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()

		err = internal.MigrateDB(ctx, db)
		if err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		storeID := normalizeStoreID(args[0])
		q := dbq.New(db)
		store, err := q.GetStoreById(ctx, storeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("unknown store %q", storeID)
			}
			return fmt.Errorf("get store: %w", err)
		}

		if !util.SqliteIntToBool(store.Enabled) {
			return fmt.Errorf("store %q is disabled", storeID)
		}

		a, err := state.LoadActive()
		if err != nil {
			return err
		}

		a.ActiveStoreID = store.ID

		if err := state.SaveActive(a); err != nil {
			return err
		}

		fmt.Printf("Active store set to %s (%s)\n", store.ID, store.DisplayName)

		return nil
	},
}

func init() {
	storesCmd.AddCommand(setActiveCmd)
}

func normalizeStoreID(s string) string {
	// store ids are meant to be stable identifiers like "steam"
	// normalize to lowercase to avoid surprising mismatches
	// TODO pull this somewhere that we can reuse it
	return strings.ToLower(strings.TrimSpace(s))
}
