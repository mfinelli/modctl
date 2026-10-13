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

	"github.com/mfinelli/modctl/internal/config"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
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
		if cfg.NexusAPIKey != "" && !authNexusLoginForce {
			style.Println(style.Failure.Render("  ✗ a Nexus Mods API key is already configured"))
			style.Println(style.Subtle.Render("    run with --force to replace it"))
			style.Println()
			return fmt.Errorf("nexus API key already set")
		}

		style.Println(style.Subtle.Render("  Connecting to Nexus Mods SSO..."))

		ctx, cancel := context.WithTimeout(cmd.Context(), nexusclient.DefaultSSOTimeout)
		defer cancel()

		apiKey, err := nexusclient.Login(ctx, os.Stderr)
		if err != nil {
			style.Println()
			style.Println(style.Failure.Render("  ✗ authentication failed"))
			style.Println(style.Subtle.Render("    " + err.Error()))
			style.Println()
			return fmt.Errorf("nexus SSO login: %w", err)
		}

		path := configPath()
		if err := config.Set(path, "nexus.apikey", apiKey); err != nil {
			return err
		}

		style.Println()
		style.Println(style.Success.Render("  ✓ authenticated with Nexus Mods"))
		style.Println(style.Subtle.Render("    API key saved to " + path))
		style.Println(style.Subtle.Render("    Note: the key is stored in plain text (mode 0600)"))
		style.Println()

		return nil
	},
}

func init() {
	authNexusCmd.AddCommand(authNexusLoginCmd)

	authNexusLoginCmd.Flags().BoolVar(&authNexusLoginForce, "force", false,
		"Replace an existing API key")
}
