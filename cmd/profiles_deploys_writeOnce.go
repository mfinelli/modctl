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

var profilesDeploysWriteOnceCmd = &cobra.Command{
	Use:   "write-once",
	Short: "Manage write-once patterns",
	Long: `Manage write-once patterns for mod versions in a profile.

Files matching a write-once pattern (evaluated against the final remapped
destination path) are deployed on first apply and then left untouched on
subsequent applies, preserving any in-game changes made after the initial
deployment.

If a matched file is missing from disk it will be re-deployed. The initial
backup of any pre-existing non-tool-owned file is still performed, so
unapply can restore the original file correctly.

Use this for config files that the game modifies at runtime (e.g. settings
changed via an in-game menu) where you want the mod to provide the initial
defaults without overwriting the player's subsequent changes.`,
}

func init() {
	profilesDeploysCmd.AddCommand(profilesDeploysWriteOnceCmd)
}
