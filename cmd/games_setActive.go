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
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var gamesSetActiveCmd = &cobra.Command{
	Use:   "set-active",
	Short: "Set the active game",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.GameInstallSelectors(cmd, toComplete)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

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

		// Fast path: numeric ID
		// TODO: i'm not sure if I actually want this or not...
		if id, ok := internal.ParseInt64(args[0]); ok {
			gi, err := q.GetGameInstallByID(ctx, id)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("no game install with id %d", id)
				}
				return fmt.Errorf("get game install by id: %w", err)
			}
			return persistActiveGameInstall(gi)
		}

		// Selector path
		storeID, storeGameID, instanceID, err := internal.ParseSelector(args[0])
		if err != nil {
			return err
		}

		// If user provided an explicit instance, lookup is unambiguous.
		gi, err := q.GetGameInstallBySelector(ctx, dbq.GetGameInstallBySelectorParams{
			StoreID:     storeID,
			StoreGameID: storeGameID,
			InstanceID:  instanceID,
		})
		if err == nil {
			return persistActiveGameInstall(gi)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("get game install: %w", err)
		}

		// If instance was explicitly provided and we didn't find it -> not found.
		// ParseSelector defaults to "default" when omitted, so we need to distinguish.
		// We'll treat the input containing '#' as "explicit instance".
		if strings.Contains(args[0], "#") {
			return fmt.Errorf("no game install found for %s",
				internal.FullSelector(storeID, storeGameID, instanceID))
		}

		// Instance omitted: maybe ambiguous, list candidates
		rows, lerr := q.ListGameInstallsByStoreGameID(ctx, dbq.ListGameInstallsByStoreGameIDParams{
			StoreID:     storeID,
			StoreGameID: storeGameID,
		})
		if lerr != nil {
			return fmt.Errorf("list candidates: %w", lerr)
		}
		if len(rows) == 0 {
			return fmt.Errorf("no game installs found for %s:%s", storeID, storeGameID)
		}
		if len(rows) == 1 {
			// Only one install exists: treat it as the selected one
			only := rows[0]
			gi2, gerr := q.GetGameInstallByID(ctx, only.ID)
			if gerr != nil {
				return fmt.Errorf("get game install by id: %w", gerr)
			}
			return persistActiveGameInstall(gi2)
		}

		// Ambiguous: show choices and require instance
		var b strings.Builder
		fmt.Fprintf(&b, "Multiple installs found for %s:%s. Choose one:\n\n", storeID, storeGameID)
		for _, r := range rows {
			sel := internal.FullSelector(r.StoreID, r.StoreGameID, r.InstanceID)
			present := "present"
			if r.IsPresent == 0 {
				present = "missing"
			}
			lastSeen := ""
			if r.LastSeenAt.Valid {
				lastSeen = r.LastSeenAt.String
			}
			fmt.Fprintf(&b, "  %s  (%s)  %s  %s\n", sel, r.DisplayName, present, lastSeen)
		}
		fmt.Fprintf(&b, "\nRun: modctl games set-active %s\n",
			internal.FullSelector(storeID, storeGameID, "<instance>"))
		return errors.New(b.String())
	},
}

func init() {
	gamesCmd.AddCommand(gamesSetActiveCmd)
}

func persistActiveGameInstall(gi dbq.GameInstall) error {
	a, err := state.LoadActive()
	if err != nil {
		return err
	}

	fullSel := internal.FullSelector(gi.StoreID, gi.StoreGameID, gi.InstanceID)

	a.ActiveStoreID = gi.StoreID // keeps store context in sync
	a.ActiveGameInstallID = gi.ID
	a.ActiveGameInstallSelector = fullSel

	if err := state.SaveActive(a); err != nil {
		return err
	}

	fmt.Printf("Active game set to %s (%s)\n", fullSel, gi.DisplayName)
	return nil
}
