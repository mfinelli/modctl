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
	"fmt"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/spf13/cobra"
	"go.finelli.dev/util"
)

var gamesInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show detailed info about a discovered game install",
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
		gi, err := internal.ResolveGameInstallArg(ctx, q, args[0])
		if err != nil {
			return err
		}

		targets, err := q.ListTargetsForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list targets: %w", err)
		}

		profiles, err := q.GetProfilesForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list profiles: %w", err)
		}

		fullSel := internal.FullSelector(gi.StoreID, gi.StoreGameID, gi.InstanceID)
		shortSel := internal.ShortSelector(gi.StoreID, gi.StoreGameID, gi.InstanceID)

		// Print
		fmt.Printf("%s\n", gi.DisplayName)
		fmt.Printf("  ID:        %d\n", gi.ID)
		fmt.Printf("  Selector:  %s\n", fullSel)
		if shortSel != fullSel {
			fmt.Printf("  Short:     %s\n", shortSel)
		}
		fmt.Printf("  Store:     %s\n", gi.StoreID)
		fmt.Printf("  Store ID:  %s\n", gi.StoreGameID)
		fmt.Printf("  Instance:  %s\n", gi.InstanceID)
		fmt.Printf("  Path:      %s\n", gi.InstallRoot)

		present := "yes"
		if gi.IsPresent == 0 {
			present = "no"
		}
		fmt.Printf("  Present:   %s\n", present)

		if gi.LastSeenAt.Valid {
			fmt.Printf("  Last seen: %s\n", gi.LastSeenAt.String)
		}

		// Targets
		fmt.Println()
		fmt.Println("Targets:")
		if len(targets) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, t := range targets {
				fmt.Printf("  - %s\n", t.Name)
				fmt.Printf("      path:   %s\n", t.RootPath)
				fmt.Printf("      origin: %s\n", t.Origin)
			}
		}

		// Profiles
		fmt.Println()
		fmt.Println("Profiles:")
		if len(profiles) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, p := range profiles {
				active := "\n"
				if util.SqliteIntToBool(p.IsActive) {
					active = " (active)\n"
				}
				fmt.Printf("  - %s%s", p.Name, active)
				if p.Description.Valid {
					fmt.Printf("      desc:    %s\n", p.Description.String)
				}
			}
		}

		return nil
	},
}

func init() {
	gamesCmd.AddCommand(gamesInfoCmd)
}
