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

var profilesOverridesPatchCmd = &cobra.Command{
	Use:   "patch",
	Short: "Manage structured patch entries for an override",
	Long: `Manage structured patch entries for a file override.

Patch overrides apply key-value mutations on top of the base mod's file
content rather than replacing the file entirely. Supported patch types
are ini, yaml, and json.

The patch type is inferred from the file extension on first use and
recorded on the override. Subsequent patch commands use the recorded
type automatically.

Use 'profiles overrides patch preview <path>' to see the result of
applying the current patch entries without performing a full apply.`,
}

func init() {
	profilesOverridesCmd.AddCommand(profilesOverridesPatchCmd)
}
