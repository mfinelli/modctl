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
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var authNexusLoginForce bool

var authNexusLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Nexus Mods via SSO",
	Long: `Authenticate with Nexus Mods using Single Sign-On.

Opens your browser to authorize modctl with Nexus Mods. Once you approve
the request, your API key is saved automatically; no manual key copying
required.

In headless environments the authorization URL is printed so you can open
it on any other machine or device. The key is received automatically once
you complete authorization.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract these
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
		errStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))

		if viper.GetString("nexus.apikey") != "" && !authNexusLoginForce {
			fmt.Println(errStyle.Render("  ✗ a Nexus Mods API key is already configured"))
			fmt.Println(subtleStyle.Render("    run with --force to replace it"))
			fmt.Println()
			return fmt.Errorf("nexus API key already set")
		}

		fmt.Println(subtleStyle.Render("  Connecting to Nexus Mods SSO..."))

		ctx, cancel := context.WithTimeout(cmd.Context(), nexusclient.DefaultSSOTimeout)
		defer cancel()

		apiKey, err := nexusclient.Login(ctx, os.Stderr)
		if err != nil {
			fmt.Println()
			fmt.Println(errStyle.Render("  ✗ authentication failed"))
			fmt.Println(subtleStyle.Render("    " + err.Error()))
			fmt.Println()
			return fmt.Errorf("nexus SSO login: %w", err)
		}

		configPath := viper.ConfigFileUsed()
		if configPath == "" {
			configPath = filepath.Join(xdg.ConfigHome, "modctl", "config.toml")
		}
		if err := ensureConfigFile(configPath); err != nil {
			return fmt.Errorf("create config file: %w", err)
		}
		cfg, err := readOrCreateToml(configPath)
		if err != nil {
			return fmt.Errorf("read config file: %w", err)
		}

		nexusCfg, ok := cfg["nexus"].(map[string]any)
		if !ok {
			nexusCfg = make(map[string]any)
		}
		nexusCfg["apikey"] = apiKey
		cfg["nexus"] = nexusCfg

		data, err := toml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("marshal config: %w", err)
		}
		if err := os.WriteFile(configPath, data, 0o600); err != nil {
			return fmt.Errorf("write config file: %w", err)
		}

		fmt.Println()
		fmt.Println(okStyle.Render("  ✓ authenticated with Nexus Mods"))
		fmt.Println(subtleStyle.Render("    API key saved to " + configPath))
		fmt.Println(subtleStyle.Render("    Note: the key is stored in plain text (mode 0600)"))
		fmt.Println()

		return nil
	},
}

func init() {
	authNexusCmd.AddCommand(authNexusLoginCmd)

	authNexusLoginCmd.Flags().BoolVar(&authNexusLoginForce, "force", false,
		"Replace an existing API key")
}
