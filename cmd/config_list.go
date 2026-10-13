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
	"github.com/mfinelli/modctl/internal/config"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
)

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all config keys and their current values",
	Long: `List all config keys and their current values.

Shows both explicitly set values and defaults. Keys that have been set
in the config file are marked as such; all others show their built-in
default value.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		kv := style.KV{Indent: 2, Width: 16}

		style.Println(style.Header.Render("Configuration"))
		style.Println(style.Subtle.Render("  file: " + configPath()))
		style.Println()

		for _, key := range config.Keys() {
			value, _ := cfg.Value(key)

			// mask the api key
			if key == "nexus.apikey" && value != "" {
				value = config.Mask(value)
			}

			origin := style.Subtle.Render("(default)")
			if cfg.FromFile(key) {
				origin = style.Subtle.Render("(set)")
			}

			kv.Print(key+":", value+"  "+origin)
		}

		return nil
	},
}

func init() {
	configCmd.AddCommand(configListCmd)
}
