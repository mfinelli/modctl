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

package completion

import (
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Transitional: the functions in this file are the completions of a Completer
// that is made with the global configuration and the environment, at the time
// that a completion is asked for (the commands are set up before the
// configuration is read, so it can't be before). They are here for the commands
// that do not have a Completer of their own yet, and go with them.
// configured is the only thing in this package that reads either.
func configured() Completer {
	return Completer{
		DatabasePath: viper.GetString("database"),
		StateDir:     state.DefaultDir(),
	}
}

func GameInstallSelectors(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	return configured().GameInstallSelectors(cmd, toComplete)
}

func ModFileVersionIDs(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	return configured().ModFileVersionIDs(cmd, toComplete)
}

func ModFileVersionIDsForPage(cmd *cobra.Command, modPageArg string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return configured().ModFileVersionIDsForPage(cmd, modPageArg, toComplete)
}

func ModPageIDs(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	return configured().ModPageIDs(cmd, toComplete)
}

func ProfileNames(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	return configured().ProfileNames(cmd, toComplete)
}

func StoreIDs(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	return configured().StoreIDs(cmd, toComplete)
}

func TargetNames(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	return configured().TargetNames(cmd, toComplete)
}
