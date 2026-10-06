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
	"database/sql"
	"errors"
	"fmt"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/lock"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var gamesTargetsDisableGame string

var gamesTargetsDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable an install target",
	Long: `Disable an install target so that apply and unapply skip it.

This is useful for games that only use one of game_dir and proton_prefix.
Discovered targets can be disabled too; they stay disabled when games are
refreshed. Use 'games targets enable' to turn a target back on.

A target cannot be disabled while any installed files still reference it
(unapply first), while profile items or overrides still use it, or if it is
the only enabled target.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.TargetNames(cmd, toComplete)
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		name := args[0]

		if err := internal.EnsureDBExists(); err != nil {
			return err
		}

		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()

		if err := internal.MigrateDB(ctx, db); err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		q := dbq.New(db)

		if gamesTargetsDisableGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			gamesTargetsDisableGame = fmt.Sprintf("%d", active.ActiveGameInstallID)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, gamesTargetsDisableGame)
		if err != nil {
			return err
		}

		// Hold the per-game lock so an apply can't record installed files for
		// this target between the checks below and the update.
		unlock, err := lock.GameInstall(viper.GetString("locks_dir"), gi.ID)
		if err != nil {
			return err
		}
		defer unlock()

		targets, err := q.ListTargetsForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list targets: %w", err)
		}

		target, err := q.GetTargetByGameInstallAndName(ctx, dbq.GetTargetByGameInstallAndNameParams{
			GameInstallID: gi.ID,
			Name:          name,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("target %q not found", name)
			}
			return fmt.Errorf("get target: %w", err)
		}

		if !internal.TargetEnabled(target) {
			fmt.Printf("Target %q is already disabled.\n", name)
			return nil
		}

		installed, err := q.CountInstalledFilesForTarget(ctx, target.ID)
		if err != nil {
			return fmt.Errorf("check installed files: %w", err)
		}
		items, err := q.CountProfileItemsForTarget(ctx, target.ID)
		if err != nil {
			return fmt.Errorf("check profile items: %w", err)
		}
		overrides, err := q.CountOverridesForTarget(ctx, target.ID)
		if err != nil {
			return fmt.Errorf("check overrides: %w", err)
		}

		if err := internal.CheckTargetCanBeDisabled(target, internal.TargetUsage{
			InstalledFiles: installed,
			ProfileItems:   items,
			Overrides:      overrides,
			EnabledTargets: int64(len(internal.EnabledTargets(targets))),
		}); err != nil {
			return err
		}

		if err := q.SetTargetEnabled(ctx, dbq.SetTargetEnabledParams{
			Enabled: 0,
			ID:      target.ID,
		}); err != nil {
			return fmt.Errorf("disable target: %w", err)
		}

		fmt.Printf("Disabled target %q.\n", name)
		return nil
	},
}

func init() {
	gamesTargetsCmd.AddCommand(gamesTargetsDisableCmd)

	gamesTargetsDisableCmd.Flags().StringVarP(&gamesTargetsDisableGame, "game", "g", "",
		"Override the currently active game")
	gamesTargetsDisableCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
