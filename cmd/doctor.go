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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/lipgloss"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		// styles
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
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
