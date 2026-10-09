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
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

		configPath := viper.ConfigFileUsed()
		if configPath == "" {
			configPath = filepath.Join(xdg.ConfigHome, "modctl", "config.toml")
		}

		style.Println(style.Header.Render("Configuration"))
		style.Println(style.Subtle.Render("  file: " + configPath))
		style.Println()

		for _, key := range knownConfigKeys {
			value, fromFile := resolveConfigKey(key)

			// mask the api key
			if key == "nexus.apikey" && value != "" {
				value = maskAPIKey(value)
			}

			origin := style.Subtle.Render("(default)")
			if fromFile {
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

func maskAPIKey(key string) string {
	if len(key) <= 4 {
		return strings.Repeat("*", len(key))
	}
	return strings.Repeat("*", len(key)-4) + key[len(key)-4:]
}
