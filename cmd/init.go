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
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/mfinelli/modctl/internal"
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "initializes the modctl database and filesystem",
	Long: `Initialize modctl's local state.

Creates the required data directories (archives, backups, overrides, tmp) and
initializes or upgrades the internal database. This command is safe to run
multiple times and will not overwrite existing data.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		err := os.MkdirAll(viper.GetString("archives_dir"), 0o0755)
		if err != nil {
			return fmt.Errorf("error creating archives directory: %w", err)
		}

		err = os.MkdirAll(viper.GetString("backups_dir"), 0o0755)
		if err != nil {
			return fmt.Errorf("error creating backups directory: %w", err)
		}

		err = os.MkdirAll(viper.GetString("overrides_dir"), 0o0755)
		if err != nil {
			return fmt.Errorf("error creating overrides directory: %w", err)
		}

		err = os.MkdirAll(viper.GetString("tmp_dir"), 0o0755)
		if err != nil {
			return fmt.Errorf("error creating tmp directory: %w", err)
		}

		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error opening database: %w", err)
		}
		defer db.Close()

		err = internal.MigrateDB(ctx, db)
		if err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
