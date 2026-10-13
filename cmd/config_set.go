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

	"github.com/mfinelli/modctl/internal/config"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
)

var configSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set a config key",
	Long: `Set a config key in the config file.

The config file is created if it does not exist. Note that setting a value
will rewrite the config file, which will remove any comments or custom
formatting you may have added by hand.

Valid keys:
  bsdtar, database, archives_dir, backups_dir, cache_dir, overrides_dir,
  locks_dir, tmp_dir, nexus.apikey`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return config.Keys(), cobra.ShellCompDirectiveNoFileComp
		}
		// second arg is the value; no completions
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		if _, ok := cfg.Value(key); !ok {
			return config.UnknownKeyError{Key: key}
		}

		if key == "nexus.apikey" {
			style.Println(style.Subtle.Render("  Please note: your API key will be stored in plain text in the config file."))
			style.Println()
		}

		path := configPath()
		if err := config.Set(path, key, value); err != nil {
			return err
		}

		style.Println(style.Success.Render(fmt.Sprintf("  ✓ %s set in %s", key, path)))

		return nil
	},
}

func init() {
	configCmd.AddCommand(configSetCmd)
}
