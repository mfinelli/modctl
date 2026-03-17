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
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View and modify modctl configuration",
}

func init() {
	rootCmd.AddCommand(configCmd)
}

var knownConfigKeys = []string{
	"bsdtar",
	"database",
	"archives_dir",
	"backups_dir",
	"cache_dir",
	"locks_dir",
	"overrides_dir",
	"tmp_dir",
	"nexus.apikey",
}

// resolveConfigKey returns the effective value for a config key and whether
// it was explicitly set in the config file (as opposed to being a default).
func resolveConfigKey(key string) (value string, fromFile bool) {
	value = viper.GetString(key)
	// viper.IsSet returns true for defaults too, so we check the
	// in-file settings directly
	fromFile = viper.InConfig(key)
	return value, fromFile
}
