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
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesOverridesListGame    string
	profilesOverridesListProfile string
)

var profilesOverridesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List overrides for a profile",
	Long: `List all overrides for the active profile.

Shows each overridden path, its type, and a summary staleness status.
Run 'profiles overrides status' for full staleness detail.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		boldStyle := lipgloss.NewStyle().Bold(true)
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

		ctx := cmd.Context()

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

		if profilesOverridesListGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesListGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesListGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesListProfile)
		if err != nil {
			return err
		}

		overrides, err := q.ListOverridesByProfile(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("list overrides: %w", err)
		}

		if len(overrides) == 0 {
			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"  no overrides for profile %q", p.Name,
			)))
			return nil
		}

		// build staleness map from heuristic query
		staleRows, err := q.GetStalenessHeuristicForProfile(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("loading staleness: %w", err)
		}
		stalenessMap := make(map[int64]string, len(staleRows))
		for _, s := range staleRows {
			stalenessMap[s.OverrideID] = s.Staleness
		}

		fmt.Println(boldStyle.Render(fmt.Sprintf(
			"Overrides for profile %q (%d):", p.Name, len(overrides),
		)))
		fmt.Println()

		for _, o := range overrides {
			staleness := stalenessMap[o.ID]

			var statusTag string
			switch staleness {
			case "stale":
				statusTag = "  " + warnStyle.Render("⚠ may be stale")
			case "no_base":
				statusTag = "  " + warnStyle.Render("⚠ no base mod")
			case "anchor_lost":
				statusTag = "  " + warnStyle.Render("⚠ anchor lost")
			default:
				if o.SourceArchiveSha256.Valid {
					statusTag = "  " + greenStyle.Render("✓")
				}
			}

			labelStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("7")).
				Width(16)

			fmt.Printf("  %s%s\n", o.Relpath, statusTag)
			fmt.Printf("      %s %s\n", labelStyle.Render("type:"), formatOverrideType(o.OverrideType))
			if o.BlobSha256.Valid {
				fmt.Printf("      %s %s\n", labelStyle.Render("blob:"), truncateSha(o.BlobSha256.String))
			}
			if o.Notes.Valid && strings.TrimSpace(o.Notes.String) != "" {
				fmt.Printf("      %s %s\n", labelStyle.Render("notes:"), o.Notes.String)
			}
			fmt.Println()
		}

		return nil
	},
}

func init() {
	profilesOverridesCmd.AddCommand(profilesOverridesListCmd)

	profilesOverridesListCmd.Flags().StringVarP(&profilesOverridesListGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesOverridesListCmd.Flags().StringVarP(&profilesOverridesListProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesListCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}

func formatOverrideType(t string) string {
	switch t {
	case "full_file":
		return "full file"
	case "ini_patch":
		return "ini patch"
	case "yaml_patch":
		return "yaml patch"
	case "json_patch":
		return "json patch"
	default:
		return t
	}
}
