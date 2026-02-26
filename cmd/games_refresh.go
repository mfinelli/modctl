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

	"github.com/mfinelli/modctl/internal"
	"github.com/spf13/cobra"
)

// gamesRefreshCmd represents the gamesRefresh command
var gamesRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Discover installed games from enabled stores",
	Long: `Scan all enabled stores and update the list of discovered game installs.

This command detects installed games, updates their install paths, and marks
missing installs as not present.

It is safe to run multiple times.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		err := internal.EnsureDBExists()
		if err != nil {
			return err
		}

		db, err := internal.SetupDB()
		if err != nil {
			return err
		}
		defer db.Close()

		err = internal.MigrateDB(ctx, db)
		if err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		return internal.ScanStores(ctx, db)
	},
}

func init() {
	gamesCmd.AddCommand(gamesRefreshCmd)
}
