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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/internal"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// doctorCmd represents the doctor command
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		err := checkDb(ctx)
		if err != nil {
			return err
		}

		err = checkPaths()
		if err != nil {
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

// CheckDatabase verifies the DB exists and is usable, and warns if migrations
// are pending. Returns error only for non-recoverable failures.
func checkDb(ctx context.Context) error {
	// TODO: extract these somewhere else
	headerStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("63"))
	subtleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))
	errStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("1"))
	okStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))
	warnStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("3"))

	fmt.Println(headerStyle.Render("Database Checks"))
	fmt.Println(subtleStyle.Render("  db: " + viper.GetString("database")))
	fmt.Println()

	// 1) DB file existence
	dbPath := viper.GetString("database")
	info, err := os.Stat(dbPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println(errStyle.Render("  ✗ database does not exist"))
			fmt.Println(subtleStyle.Render("    run `modctl init` to create the state directory and database"))
			fmt.Println()
			return fmt.Errorf("database missing: %s", dbPath)
		}
		fmt.Println(errStyle.Render("  ✗ could not stat database file"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		fmt.Println()
		return fmt.Errorf("cannot stat database: %w", err)
	}
	if info.IsDir() {
		fmt.Println(errStyle.Render("  ✗ database path is a directory, expected a file"))
		fmt.Println()
		return fmt.Errorf("database path is a directory: %s", dbPath)
	}
	fmt.Println(okStyle.Render("  ✓ database file exists"))

	// Keep doctor snappy.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// 2) Open DB + trivial query
	db, err := internal.SetupDB()
	if err != nil {
		fmt.Println(errStyle.Render("  ✗ could not open database"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		fmt.Println()
		return fmt.Errorf("cannot open database: %w", err)
	}
	defer db.Close()

	var one int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		fmt.Println(errStyle.Render("  ✗ basic query failed (SELECT 1)"))
		if err != nil {
			fmt.Println(subtleStyle.Render("    " + err.Error()))
		}
		fmt.Println()
		return fmt.Errorf("database not usable: %w", err)
	}
	fmt.Println(okStyle.Render("  ✓ basic query OK (SELECT 1)"))

	p, err := internal.GooseProvider(db)
	if err != nil {
		// if we can't determine migration state treat it as fatal
		fmt.Println(errStyle.Render("  ✗ could not determine migration status"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		fmt.Println()
		return fmt.Errorf("cannot determine migration status: %w", err)
	}

	pending, err := p.HasPending(ctx)
	if err != nil {
		// if we can't determine migration state treat it as fatal
		fmt.Println(errStyle.Render("  ✗ could not determine migration status"))
		fmt.Println(subtleStyle.Render("    " + err.Error()))
		fmt.Println()
		return fmt.Errorf("cannot determine migration status: %w", err)
	}

	if pending {
		current, target, verr := p.GetVersions(ctx)
		if verr == nil {
			fmt.Println(warnStyle.Render(fmt.Sprintf(
				"  ⚠ pending migrations (db=%d, target=%d)",
				current, target,
			)))
		} else {
			fmt.Println(warnStyle.Render("  ⚠ pending migrations — other commands will auto-migrate"))
		}
	} else {
		fmt.Println(okStyle.Render("  ✓ migrations up to date"))
	}

	fmt.Println()

	return nil
}

func checkPaths() error {
	// TODO: extract these somewhere else
	headerStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("63"))
	subtleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))
	errStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("1"))
	okStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))

	fmt.Println(headerStyle.Render("State Directory Checks"))
	fmt.Println(subtleStyle.Render("  root: " + filepath.Join(xdg.DataHome, "modctl")))
	fmt.Println()

	required := []string{
		viper.GetString("archives_dir"),
		viper.GetString("backups_dir"),
		viper.GetString("overrides_dir"),
		viper.GetString("tmp_dir"),
	}

	var fatalErr error

	for _, path := range required {
		name := filepath.Base(path)
		info, err := os.Stat(path)
		if err != nil {
			fmt.Println(errStyle.Render(fmt.Sprintf("  ✗ %s: does not exist (%s)", name, path)))
			fatalErr = errors.New("missing required state directory")
			continue
		}

		if !info.IsDir() {
			fmt.Println(errStyle.Render(fmt.Sprintf("  ✗ %s: not a directory (%s)", name, path)))
			fatalErr = errors.New("invalid state directory type")
			continue
		}

		// Test writability by creating a temp file
		testFile := filepath.Join(path, ".modctl-doctor-write-test")
		if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
			fmt.Println(errStyle.Render(fmt.Sprintf("  ✗ %s: not writable (%s)", name, path)))
			fatalErr = errors.New("state directory not writable")
			continue
		}
		_ = os.Remove(testFile)

		fmt.Println(okStyle.Render(fmt.Sprintf("  ✓ %s: OK (%s)", name, path)))
	}

	fmt.Println()

	return fatalErr
}
