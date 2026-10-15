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
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mfinelli/modctl/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
	logger  *slog.Logger
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
	Version: "0.9.0",

	CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		level := slog.LevelWarn
		if verbose {
			level = slog.LevelDebug
		}
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: level,
		}))
		slog.SetDefault(logger)

		if cfg.File != "" {
			logger.Debug("using config file", "path", cfg.File)
		}

		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		return 1
	}
	return 0
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(
		&cfgFile,
		"config",
		"",
		"config file (default is $XDG_CONFIG_HOME/modctl/config.toml)",
	)
	rootCmd.MarkFlagFilename("config", "toml")

	rootCmd.PersistentFlags().BoolVarP(
		&verbose,
		"verbose",
		"v",
		false,
		"enable verbose output",
	)
}

// initConfig reads in the config file, if there is one (the one that was asked
// for must be there and be right, and the default one is only read if it is
// there). It makes nothing: not the directories that the configuration names,
// or the one that the config file would be in.
func initConfig() {
	c, err := config.Load(configDirs(), cfgFile)
	cobra.CheckErr(err)

	cfg = c
}
