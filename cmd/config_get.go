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
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var configGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get the value of a config key",
	Long: `Get the value of a config key.

Shows the current effective value and whether it comes from the config
file or is the built-in default.

Valid keys:
  bsdtar, database, archives_dir, backups_dir, overrides_dir,
  locks_dir, tmp_dir, nexus.apikey`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	ValidArgs:    knownConfigKeys,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract
		labelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")).
			Width(16)
		subtleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

		key := args[0]
		if !slices.Contains(knownConfigKeys, key) {
			return fmt.Errorf("unknown config key %q\n  valid keys: %s", key, strings.Join(knownConfigKeys, ", "))
		}

		value, fromFile := resolveConfigKey(key)
		origin := subtleStyle.Render("(default)")
		if fromFile {
			origin = subtleStyle.Render("(set)")
		}

		fmt.Printf("  %s %s  %s\n", labelStyle.Render(key+":"), value, origin)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configGetCmd)
}
