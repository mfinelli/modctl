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

var gamesBackupsCmd = &cobra.Command{
	Use:   "backups",
	Short: "Inspect and manage backed-up game files",
	Long: `Inspect and manage files that modctl backed up before overwriting them.

Backups are created automatically when modctl overwrites a file it did not
install. They are restored automatically on unapply. These commands let you
inspect, preview, and manage individual backup entries.`,
}

func init() {
	gamesCmd.AddCommand(gamesBackupsCmd)
}
