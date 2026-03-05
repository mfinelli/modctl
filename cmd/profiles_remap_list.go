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
	"os"
	"os/signal"
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesRemapListGame    string
	profilesRemapListProfile string
)

var profilesRemapListCmd = &cobra.Command{
	Use:   "list",
	Short: "List remap rules for a mod version in a profile",
	Long: `List all remap rules for a mod version in a profile.

Rules are shown in the order they will be applied during planning.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract elsewhere
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		boldStyle := lipgloss.NewStyle().Bold(true)

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		versionID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || versionID <= 0 {
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

		if profilesRemapListGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesRemapListGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesRemapListGame)
		if err != nil {
			return err
		}

		p, err := internal.ResolveProfileArg(ctx, q, &gi, profilesRemapListProfile)
		if err != nil {
			return err
		}

		itemID, err := internal.ResolveProfileItemByVersion(ctx, &p, q, versionID)
		if err != nil {
			return err
		}

		rules, err := q.ListRemapRulesForProfileItem(ctx, itemID)
		if err != nil {
			return fmt.Errorf("list remap rules: %w", err)
		}

		if len(rules) == 0 {
			fmt.Println(subtleStyle.Render(fmt.Sprintf("  no remap rules for version %d in profile %q", versionID, p.Name)))
			return nil
		}

		fmt.Println(boldStyle.Render(fmt.Sprintf("Remap rules for version %d in profile %q:", versionID, p.Name)))
		for _, rule := range rules {
			fmt.Println(formatRemapRule(rule))
		}

		return nil
	},
}

func init() {
	profilesRemapCmd.AddCommand(profilesRemapListCmd)

	profilesRemapListCmd.PersistentFlags().StringVarP(&profilesRemapListGame, "game", "g", "",
		"Override the currently active game")
	profilesRemapListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesRemapListCmd.PersistentFlags().StringVarP(&profilesRemapListProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesRemapListCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}

// formatRemapRule returns a human-readable string for a remap rule.
func formatRemapRule(rule dbq.ListRemapRulesForProfileItemRow) string {
	switch rule.RuleType {
	case "strip_components":
		return fmt.Sprintf("  [%d] strip_components: %d", rule.Position, rule.IntValue.Int64)
	case "select_subdir":
		return fmt.Sprintf("  [%d] select_subdir: %q", rule.Position, rule.TextValue.String)
	case "dest_prefix":
		return fmt.Sprintf("  [%d] dest_prefix: %q", rule.Position, rule.TextValue.String)
	case "include_glob":
		return fmt.Sprintf("  [%d] include_glob: %q", rule.Position, rule.TextValue.String)
	case "exclude_glob":
		return fmt.Sprintf("  [%d] exclude_glob: %q", rule.Position, rule.TextValue.String)
	default:
		return fmt.Sprintf("  [%d] %s", rule.Position, rule.RuleType)
	}
}
