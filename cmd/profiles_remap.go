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

var profilesRemapCmd = &cobra.Command{
	Use:   "remap",
	Short: "Manage remap rules for a mod version in a profile",
	Long: `Manage remap rules for a mod version in a profile.

Remap rules control how archive entries are transformed before installation.
Rules are applied sequentially in position order. Use 'remap list' to see
current rules and their positions.`,
}

func init() {
	profilesCmd.AddCommand(profilesRemapCmd)
}
