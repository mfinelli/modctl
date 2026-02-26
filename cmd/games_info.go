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
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"go.finelli.dev/util"
)

var gamesInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show detailed information about a game install",
	Long: `Show detailed information about a discovered game install.

You may specify either the numeric install ID or a selector such as:

  steam:1091500
  steam:1091500#default

If multiple installs exist for the same game, an explicit instance must be
provided.`,
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

		a, err := state.LoadActive()
		if err != nil {
			return err
		}
		isCurrent := a.ActiveGameInstallID == gi.ID

		fmt.Println(renderGameInfo(gi, targets, profiles, isCurrent))
		return nil
	},
}

func init() {
	gamesCmd.AddCommand(gamesInfoCmd)
}

func renderGameInfo(gi dbq.GameInstall, targets []dbq.Target, profiles []dbq.Profile, isCurrentContext bool) string {
	// styles
	cardBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)

	titleStyle := lipgloss.NewStyle().
		Bold(true)

	selectorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")) // gray

	sectionTitleStyle := lipgloss.NewStyle().
		Bold(true).
		MarginTop(1)

	activeTagStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("10"))

	warningBanner := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("11")).
		Padding(0, 1)

	contextBadge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("10")).
		Padding(0, 1).
		Bold(true)

	inactiveProfileStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	activeDot := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("●")
	inactiveDot := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("○")

	// Header card
	fullSel := internal.FullSelector(gi.StoreID, gi.StoreGameID, gi.InstanceID)
	shortSel := internal.ShortSelector(gi.StoreID, gi.StoreGameID, gi.InstanceID)
	selText := fullSel
	if shortSel != fullSel {
		selText = fmt.Sprintf("%s (short: %s)", fullSel, shortSel)
	}

	headerContent := titleStyle.Render(gi.DisplayName) + "\n" +
		selectorStyle.Render(selText)

	if isCurrentContext {
		headerContent += "\n\n" + contextBadge.Render("CURRENT ACTIVE CONTEXT")
	}

	header := cardBorder.Render(headerContent)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")

	// Not present warning
	if gi.IsPresent == 0 {
		b.WriteString("\n")
		b.WriteString(warningBanner.Render("⚠  This install is not currently present on disk"))
		b.WriteString("\n")
	}

	// Install section
	b.WriteString(sectionTitleStyle.Render("Install") + "\n")
	writeKV(&b, "ID:", fmt.Sprintf("%d", gi.ID))
	writeKV(&b, "Store:", gi.StoreID)
	writeKV(&b, "Store ID:", gi.StoreGameID)
	writeKV(&b, "Instance:", gi.InstanceID)
	writeKV(&b, "Path:", gi.InstallRoot)

	present := "yes"
	if gi.IsPresent == 0 {
		present = "no"
	}
	writeKV(&b, "Present:", present)

	if gi.LastSeenAt.Valid {
		writeKV(&b, "Last seen:", gi.LastSeenAt.String)
	}

	// Targets
	b.WriteString("\n" + sectionTitleStyle.Render("Targets") + "\n")
	if len(targets) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, t := range targets {
			b.WriteString("  • " + t.Name + "\n")
			writeKVIndented(&b, "path:", t.RootPath)
			writeKVIndented(&b, "origin:", t.Origin)
		}
	}

	// Profiles
	b.WriteString("\n" + sectionTitleStyle.Render("Profiles") + "\n")
	if len(profiles) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, p := range profiles {
			dot := inactiveDot
			line := "  "

			if p.IsActive != 0 {
				dot = activeDot
			}

			line += dot + " " + p.Name

			if util.SqliteIntToBool(p.IsActive) {
				line += "   " + activeTagStyle.Render("(active)")
			}

			b.WriteString(inactiveProfileStyle.Render(line) + "\n")

			if p.Description.Valid && strings.TrimSpace(p.Description.String) != "" {
				writeKVIndentedInactive(&b, "description:", p.Description.String)
			}

			b.WriteString("\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func writeKV(b *strings.Builder, label, value string) {
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")).
		Width(12)

	b.WriteString("  " + labelStyle.Render(label) + " " + value + "\n")
}

func writeKVIndented(b *strings.Builder, label, value string) {
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")).
		Width(12)

	b.WriteString("      " + labelStyle.Render(label) + " " + value + "\n")
}

func writeKVIndentedInactive(b *strings.Builder, label, value string) {
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")).
		Width(12)

	inactiveProfileStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	line := "      " + labelStyle.Render(label) + " " + value + "\n"
	b.WriteString(inactiveProfileStyle.Render(line))
}
