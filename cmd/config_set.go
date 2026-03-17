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
	"slices"
	"strings"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/lipgloss"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
			return knownConfigKeys, cobra.ShellCompDirectiveNoFileComp
		}
		// second arg is the value; no completions
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract
		subtleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

		key, value := args[0], args[1]
		if !slices.Contains(knownConfigKeys, key) {
			return fmt.Errorf("unknown config key %q\n  valid keys: %s", key, strings.Join(knownConfigKeys, ", "))
		}

		if key == "nexus.apikey" {
			fmt.Println(subtleStyle.Render("  Please note: your API key will be stored in plain text in the config file."))
			fmt.Println()
		}

		configPath := viper.ConfigFileUsed()
		if configPath == "" {
			// config file doesn't exist yet; use the default location
			configPath = filepath.Join(xdg.ConfigHome, "modctl", "config.toml")
		}

		if err := ensureConfigFile(configPath); err != nil {
			return fmt.Errorf("create config file: %w", err)
		}

		cfg, err := readOrCreateToml(configPath)
		if err != nil {
			return fmt.Errorf("read config file: %w", err)
		}

		if key == "nexus.apikey" {
			nexus, ok := cfg["nexus"].(map[string]any)
			if !ok {
				nexus = make(map[string]any)
			}
			nexus["apikey"] = value
			cfg["nexus"] = nexus
		} else {
			cfg[key] = value
		}

		data, err := toml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("marshal config: %w", err)
		}

		if err := os.WriteFile(configPath, data, 0o600); err != nil {
			return fmt.Errorf("write config file: %w", err)
		}

		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
		fmt.Println(okStyle.Render(fmt.Sprintf("  ✓ %s set in %s", key, configPath)))

		return nil
	},
}

func init() {
	configCmd.AddCommand(configSetCmd)
}

func ensureConfigFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	return f.Close()
}

func readOrCreateToml(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any)
	if len(strings.TrimSpace(string(data))) == 0 {
		return out, nil
	}
	if err := toml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
