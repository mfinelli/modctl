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

var authNexusLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove the stored Nexus Mods API key",
	Long: `Remove the Nexus Mods API key from the modctl config file.

This only removes the key locally. The key remains active on Nexus Mods
and can continue to be used by other applications. To fully revoke it,
visit your Nexus Mods account settings at:

  https://www.nexusmods.com/users/myaccount?tab=api`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if cfg.NexusAPIKey == "" {
			style.Println(style.Warning.Render("  ⚠ no Nexus Mods API key is configured"))
			style.Println()
			return nil
		}

		if err := config.Unset(configPath(), "nexus.apikey"); err != nil {
			return err
		}

		style.Println(style.Success.Render("  ✓ Nexus Mods API key removed"))
		style.Println(style.Subtle.Render("    Note: the key is still active on Nexus Mods, to revoke it visit:"))
		style.Println(style.Subtle.Render("    https://www.nexusmods.com/users/myaccount?tab=api"))
		style.Println()
		return nil
	},
}

func init() {
	authNexusCmd.AddCommand(authNexusLogoutCmd)
}
