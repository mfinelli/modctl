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
	"fmt"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
	"go.finelli.dev/util"
)

var (
	profilesCreateGame        string
	profilesCreateDescription string
)

var profilesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a profile for the current game",
	Long: `Create a new profile for the current game install.

Profiles are named mod sets. Exactly one profile can be active per game install.
New profiles start inactive; use ` + "`modctl profiles set-active`" + ` to activate one.

Note: modctl automatically creates a "default" profile during game refresh.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		name := args[0]

		db, err := database.Open(ctx)
		if err != nil {
			return err
		}
		defer db.Close()

		q := dbq.New(db)

		// Resolve game install id: --game overrides active selection
		gi, err := argresolver.GameInstall(ctx, q, profilesCreateGame)
		if err != nil {
			return err
		}

		var desc sql.NullString
		if profilesCreateDescription != "" {
			desc = sql.NullString{String: profilesCreateDescription, Valid: true}
		}

		id, err := q.CreateProfile(ctx, dbq.CreateProfileParams{
			GameInstallID: gi.ID,
			Name:          name,
			Description:   desc,
			IsActive:      util.SqliteBoolToInt(false),
		})
		if err != nil {
			if database.IsUniqueConstraint(err) {
				return fmt.Errorf("profile %q already exists for this game", name)
			}
			return fmt.Errorf("create profile: %w", err)
		}

		style.Printf("Created profile %q (id=%d)\n", name, id)

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesCreateCmd)

	profilesCreateCmd.Flags().StringVarP(&profilesCreateGame, "game", "g", "",
		"Override the currently active game")
	profilesCreateCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesCreateCmd.Flags().StringVarP(&profilesCreateDescription, "description", "d", "",
		"Optional profile description")
}
