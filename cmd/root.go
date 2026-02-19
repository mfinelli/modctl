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
	"errors"
	"fmt"
	"os"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	verbose bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "modctl",
	Short: "mod control: command line mod manager",
	Long: `mod control (modctl) is a command line tool to manage game mods. It uses mod
profiles to support deterministic install/uninstall of game mods.

mod control (modctl)  Copyright © 2026  Mario Finelli
This program comes with ABSOLUTELY NO WARRANTY; This program is free
software, and you are welcome to redistribute it under certain conditions;
You should have received a copy of the GNU General Public License (version
3) along with this program. If not, see https://www.gnu.org/licenses/.`,
	Version: "1.0.0",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(
		&cfgFile,
		"config",
		"",
		"config file (default is $XDG_CONFIG_HOME/modctl/config.toml",
	)

	rootCmd.PersistentFlags().BoolVarP(
		&verbose,
		"verbose",
		"v",
		false,
		"enable verbose output",
	)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	// if unspecified just search $PATH
	viper.SetDefault("bsdtar", "bsdtar")

	dbPath, err := xdg.DataFile("modctl/modctl.db")
	cobra.CheckErr(err)
	viper.SetDefault("database", dbPath)

	if cfgFile != "" {
		// User explicitly provided a config file: it must work.
		viper.SetConfigFile(cfgFile)
		viper.SetConfigType("toml")

		if err := viper.ReadInConfig(); err != nil {
			cobra.CheckErr(err)
		}

		if verbose {
			fmt.Fprintln(os.Stderr, "Using config file: ",
				viper.ConfigFileUsed())
		}

		return
	}

	defaultPath, err := xdg.ConfigFile("modctl/config.toml")
	cobra.CheckErr(err)

	if _, err := os.Stat(defaultPath); errors.Is(err, os.ErrNotExist) {
		return // default config file doesn't exist -- use defaults
	}

	viper.SetConfigFile(defaultPath)
	viper.SetConfigType("toml")

	if err := viper.ReadInConfig(); err != nil {
		// missing config file is fine -- use the built-in defaults
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			return
		}

		// parse/permission errors should fail loudly
		cobra.CheckErr(err)
		return
	}

	if verbose {
		fmt.Fprintln(os.Stderr, "Using config file: ",
			viper.ConfigFileUsed())
	}
}
