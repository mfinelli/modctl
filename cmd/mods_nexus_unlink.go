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
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var modsNexusUnlinkGame string

var modsNexusUnlinkCmd = &cobra.Command{
	Use:          "unlink",
	Short:        "Remove the Nexus file ID link from a mod file version",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract styles
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

		ctx := cmd.Context()

		nexusUnlinkVersionID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid mod_file_version_id %q (expected a positive integer)", args[0])
		}

		err = internal.EnsureDBExists()
		if err != nil {
			return err
		}

		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()

		err = internal.MigrateDB(ctx, db)
		if err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		q := dbq.New(db)

		// Resolve game install id: --game overrides active selection
		if modsNexusUnlinkGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			modsNexusUnlinkGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, modsNexusUnlinkGame)
		if err != nil {
			return err
		}

		// Check current state
		nexusFileID, err := q.GetModFileVersionNexusFileID(ctx, dbq.GetModFileVersionNexusFileIDParams{
			ID:            nexusUnlinkVersionID,
			GameInstallID: gi.ID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf(
				"mod_file_version %d not found or does not belong to the active game install; use --game to specify a different game",
				nexusUnlinkVersionID,
			)
		}
		if err != nil {
			return fmt.Errorf("fetching mod file version: %w", err)
		}

		// No-op check
		if !nexusFileID.Valid {
			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"  mod_file_version %d is already unlinked (no changes made)",
				nexusUnlinkVersionID,
			)))
			return nil
		}

		oldFileID := nexusFileID.Int64

		if err := q.UnlinkModFileVersionNexus(ctx, dbq.UnlinkModFileVersionNexusParams{
			ID:            nexusUnlinkVersionID,
			GameInstallID: gi.ID,
		}); err != nil {
			return fmt.Errorf("unlinking mod file version: %w", err)
		}

		fmt.Println(subtleStyle.Render(fmt.Sprintf(
			"  unlinked mod_file_version %d (was linked to nexus file_id %d)",
			nexusUnlinkVersionID, oldFileID,
		)))

		return nil
	},
}

func init() {
	modsNexusCmd.AddCommand(modsNexusUnlinkCmd)

	modsNexusUnlinkCmd.Flags().StringVarP(&modsNexusUnlinkGame, "game", "g", "",
		"Override the currently active game")
	modsNexusUnlinkCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
