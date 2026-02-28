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
	"fmt"

	"github.com/charmbracelet/lipgloss/table"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/spf13/cobra"
	"go.finelli.dev/util"
)

var storesListNoDisabled bool

var storesListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists all stores that we know about",
	Long: `Display all configured stores and whether they are enabled.

Only enabled stores are scanned during discovery.`,
	Args: cobra.ExactArgs(0),
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

		q := dbq.New(db)
		var stores []dbq.Store
		if storesListNoDisabled {
			stores, err = q.ListEnabledStores(ctx)
		} else {
			stores, err = q.ListAllStores(ctx)
		}
		if err != nil {
			return fmt.Errorf("error fetching stores: %w", err)
		}

		rows := [][]string{}
		for _, store := range stores {
			en := "✗"
			if util.SqliteIntToBool(store.Enabled) {
				en = "✓"
			}

			rows = append(rows, []string{
				fmt.Sprintf(" %s ", en),
				fmt.Sprintf(" %s ", store.ID),
				fmt.Sprintf(" %s ", store.DisplayName),
			})
		}

		t := table.New().
			Headers(" Enabled ", " ID ", " Name ").
			Rows(rows...)

		fmt.Println(t)

		return nil
	},
}

func init() {
	storesCmd.AddCommand(storesListCmd)

	storesListCmd.Flags().BoolVar(&storesListNoDisabled, "no-disabled", false,
		"don't show disabled stores in the list")
}
