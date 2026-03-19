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
)

var profilesOverridesCmd = &cobra.Command{
	Use:   "overrides",
	Short: "Manage file overrides for a profile",
	Long: `Manage file overrides for a profile.

Overrides allow you to replace or patch specific files in the game directory
regardless of what mods provide them. They sit above the mod priority layer
and are the final word on a file's content.

Full-file overrides replace a file's content entirely. Patch overrides apply
structured key-value mutations on top of the base mod's file (v2).

Overrides are profile-scoped. Use 'profiles overrides copy' to duplicate
overrides from another profile.`,
}

func init() {
	profilesCmd.AddCommand(profilesOverridesCmd)
}
