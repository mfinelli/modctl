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
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/mattn/go-sqlite3"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
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
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		name := args[0]

		err := internal.EnsureDBExists()
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
		if profilesCreateGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesCreateGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesCreateGame)
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
		})
		if err != nil {
			var se sqlite3.Error
			if errors.As(err, &se) && se.Code == sqlite3.ErrConstraint && se.ExtendedCode == sqlite3.ErrConstraintUnique {
				return fmt.Errorf("profile %q already exists for this game", name)
			}
			return fmt.Errorf("create profile: %w", err)
		}

		fmt.Printf("Created profile %q (id=%d)\n", name, id)

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
