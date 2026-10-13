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
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mfinelli/modctl/internal/database"
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "initializes the modctl database and filesystem",
	Long: `Initialize modctl's local state.

Creates the required data directories (archives, backups, overrides, tmp) and
initializes or upgrades the internal database. This command is safe to run
multiple times and will not overwrite existing data.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		err := os.MkdirAll(cfg.ArchivesDir, 0o0755)
		if err != nil {
			return fmt.Errorf("error creating archives directory: %w", err)
		}

		err = os.MkdirAll(cfg.BackupsDir, 0o0755)
		if err != nil {
			return fmt.Errorf("error creating backups directory: %w", err)
		}

		err = os.MkdirAll(cacheDir(), 0o0755)
		if err != nil {
			return fmt.Errorf("error creating cache directory: %w", err)
		}

		err = os.MkdirAll(cfg.LocksDir, 0o0755)
		if err != nil {
			return fmt.Errorf("error creating locks directory: %w", err)
		}

		err = os.MkdirAll(cfg.OverridesDir, 0o0755)
		if err != nil {
			return fmt.Errorf("error creating overrides directory: %w", err)
		}

		err = os.MkdirAll(cfg.TmpDir, 0o0755)
		if err != nil {
			return fmt.Errorf("error creating tmp directory: %w", err)
		}

		err = os.MkdirAll(filepath.Dir(databasePath()), 0o0755)
		if err != nil {
			return fmt.Errorf("error creating database directory: %w", err)
		}

		db, err := database.Setup(databasePath())
		if err != nil {
			return fmt.Errorf("error opening database: %w", err)
		}
		defer db.Close()

		err = Schema.Migrate(ctx, db)
		if err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
