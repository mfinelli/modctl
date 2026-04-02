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

var profilesDeploysSkipBackupCmd = &cobra.Command{
	Use:   "skip-backup",
	Short: "Manage skip-backup patterns",
	Long: `Manage skip-backup patterns for mod versions in a profile.

Files matching a skip-backup pattern (evaluated against the final remapped
destination path) are never backed up during apply. This includes both the
initial backup of a pre-existing file and any subsequent drift backups.

Use this for files that change frequently (e.g. cache files) where
accumulating backups is undesirable. Note that matched paths cannot be
restored by modctl on unapply; Steam's "verify integrity of game files"
can be used to recover game-owned files if needed.`,
}

func init() {
	profilesDeploysCmd.AddCommand(profilesDeploysSkipBackupCmd)
}
