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
	"path/filepath"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	gamesTargetsAddGame  string
	gamesTargetsAddRelTo string
)

var gamesTargetsAddCmd = &cobra.Command{
	Use:   "add <name> <path>",
	Short: "Add a custom install target for a game",
	Long: `Add a user-defined install target for a game.

The path is stored as an absolute path. Use --relative-to to specify another
target name to resolve the path relative to. For example:

  modctl games targets add unitymodmanager \
    "users/steamuser/AppData/LocalLow/Owlcat Games/Rogue Trader/UnityModManager" \
    --relative-to proton_prefix

The path is resolved to an absolute path at creation time. If the base target
moves later, this target will not be updated automatically.`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		name := args[0]
		rawPath := args[1]

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

		if gamesTargetsAddGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			gamesTargetsAddGame = fmt.Sprintf("%d", active.ActiveGameInstallID)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, gamesTargetsAddGame)
		if err != nil {
			return err
		}

		// Resolve the final absolute path
		var absPath string
		if gamesTargetsAddRelTo != "" {
			base, err := q.GetTargetByGameInstallAndName(ctx, dbq.GetTargetByGameInstallAndNameParams{
				GameInstallID: gi.ID,
				Name:          gamesTargetsAddRelTo,
			})
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("base target %q not found; run `modctl games targets list` to see available targets", gamesTargetsAddRelTo)
				}
				return fmt.Errorf("resolve base target %q: %w", gamesTargetsAddRelTo, err)
			}
			absPath = filepath.Join(base.RootPath, rawPath)
		} else {
			if !filepath.IsAbs(rawPath) {
				return fmt.Errorf("path %q is not absolute; use --relative-to to specify a base target or provide an absolute path", rawPath)
			}
			absPath = filepath.Clean(rawPath)
		}

		target, err := q.InsertUserTarget(ctx, dbq.InsertUserTargetParams{
			GameInstallID: gi.ID,
			Name:          name,
			RootPath:      absPath,
		})
		if err != nil {
			return fmt.Errorf("add target: %w", err)
		}

		fmt.Printf("Added target %q (id=%d) → %s\n", target.Name, target.ID, target.RootPath)
		return nil
	},
}

func init() {
	gamesTargetsCmd.AddCommand(gamesTargetsAddCmd)

	gamesTargetsAddCmd.Flags().StringVarP(&gamesTargetsAddGame, "game", "g", "",
		"Override the currently active game")
	gamesTargetsAddCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	gamesTargetsAddCmd.Flags().StringVar(&gamesTargetsAddRelTo, "relative-to", "",
		"Name of an existing target to resolve the path relative to")
	gamesTargetsAddCmd.RegisterFlagCompletionFunc("relative-to",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.TargetNames(cmd, toComplete)
		})
}
