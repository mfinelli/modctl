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
	"go.finelli.dev/util"
)

var (
	profilesDiffGame        string
	profilesDiffNoUnchanged bool
)

// diffKind classifies a single diff row.
type diffKind int

const (
	diffAdded     diffKind = iota // in B only
	diffRemoved                   // in A only
	diffChanged                   // in both but something differs
	diffUnchanged                 // in both, identical
)

var profilesDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Compare two profiles for the same game install",
	Long: `Compare two profiles for the same game install.

Shows mods that have been added, removed, or changed between profile A
and profile B. Use --no-unchanged to hide mods that are identical in
both profiles.

Both profiles must belong to the same game install. The comparison is
directional: profile A is the source and profile B is the target.`,
	Args: cobra.RangeArgs(1, 2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Complete up to two positional args, both are profile names.
		if len(args) >= 2 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.ProfileNames(cmd, toComplete)
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		// Resolve game install
		if profilesDiffGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesDiffGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesDiffGame)
		if err != nil {
			return err
		}

		// Resolve both profiles
		var profileA, profileB dbq.Profile

		if len(args) == 1 {
			// Diff active profile against the named one
			profileA, err = argresolver.ResolveProfileArg(ctx, q, &gi, "")
			if err != nil {
				return fmt.Errorf("active profile: %w", err)
			}
			profileB, err = argresolver.ResolveProfileArg(ctx, q, &gi, args[0])
			if err != nil {
				return fmt.Errorf("profile B: %w", err)
			}
		} else {
			profileA, err = argresolver.ResolveProfileArg(ctx, q, &gi, args[0])
			if err != nil {
				return fmt.Errorf("profile A: %w", err)
			}
			profileB, err = argresolver.ResolveProfileArg(ctx, q, &gi, args[1])
			if err != nil {
				return fmt.Errorf("profile B: %w", err)
			}
		}

		if profileA.ID == profileB.ID {
			return fmt.Errorf("profile A and profile B are the same profile")
		}

		items, err := q.GetProfileDiffItems(ctx, dbq.GetProfileDiffItemsParams{
			ProfileID:   profileA.ID,
			ProfileID_2: profileB.ID,
		})
		if err != nil {
			return fmt.Errorf("get diff items: %w", err)
		}

		fmt.Println(renderProfileDiff(
			items,
			profileA.Name,
			profileB.Name,
			gi.DisplayName,
			profilesDiffNoUnchanged,
		))

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesDiffCmd)

	profilesDiffCmd.Flags().StringVarP(&profilesDiffGame, "game", "g", "",
		"Override the currently active game")
	profilesDiffCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesDiffCmd.Flags().BoolVar(&profilesDiffNoUnchanged, "no-unchanged", false,
		"Hide mods that are identical in both profiles")
}

func classifyDiffRow(row dbq.GetProfileDiffItemsRow) diffKind {
	inA := util.SqliteIntToBool(row.InProfileA)
	inB := util.SqliteIntToBool(row.InProfileB)

	switch {
	case inA && !inB:
		return diffRemoved
	case !inA && inB:
		return diffAdded
	default:
		// Both present - check if anything differs.
		if row.PriorityA != row.PriorityB ||
			row.EnabledA != row.EnabledB ||
			row.RemapConfigIDA.Valid != row.RemapConfigIDB.Valid {
			return diffChanged
		}
		return diffUnchanged
	}
}

func renderProfileDiff(
	items []dbq.GetProfileDiffItemsRow,
	nameA, nameB string,
	gameName string,
	noUnchanged bool,
) string {
	// TODO extract these somewhere
	boldStyle := lipgloss.NewStyle().Bold(true)
	subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	var b strings.Builder

	b.WriteString(boldStyle.Render(fmt.Sprintf("Diff: %q → %q", nameA, nameB)))
	b.WriteString(subtleStyle.Render(fmt.Sprintf("  (%s)", gameName)))
	b.WriteString("\n\n")

	var countAdded, countRemoved, countChanged, countUnchanged int

	for _, item := range items {
		kind := classifyDiffRow(item)

		modLabel := item.ModPageName
		if item.VersionString.Valid && item.VersionString.String != "" {
			modLabel += " " + item.VersionString.String
		}
		modLabel += subtleStyle.Render(fmt.Sprintf(" / %s", item.FileLabel))

		switch kind {
		case diffAdded:
			b.WriteString(fmt.Sprintf("  %s %-55s %s\n",
				greenStyle.Render("+"),
				modLabel,
				subtleStyle.Render("(added)")))
			countAdded++

		case diffRemoved:
			b.WriteString(fmt.Sprintf("  %s %-55s %s\n",
				redStyle.Render("-"),
				modLabel,
				subtleStyle.Render("(removed)")))
			countRemoved++

		case diffChanged:
			b.WriteString(fmt.Sprintf("  %s %s\n",
				yellowStyle.Render("~"),
				modLabel))
			// Show what changed.
			if item.PriorityA != item.PriorityB {
				b.WriteString(fmt.Sprintf("      %s priority %d → %d\n",
					subtleStyle.Render("·"),
					item.PriorityA, item.PriorityB))
			}
			if item.EnabledA != item.EnabledB {
				fromStr := "disabled"
				toStr := "enabled"
				if util.SqliteIntToBool(item.EnabledA) {
					fromStr = "enabled"
					toStr = "disabled"
				}
				b.WriteString(fmt.Sprintf("      %s %s → %s\n",
					subtleStyle.Render("·"),
					fromStr, toStr))
			}
			if item.RemapConfigIDA.Valid != item.RemapConfigIDB.Valid {
				if item.RemapConfigIDA.Valid {
					b.WriteString(fmt.Sprintf("      %s remap rules removed\n",
						subtleStyle.Render("·")))
				} else {
					b.WriteString(fmt.Sprintf("      %s remap rules added\n",
						subtleStyle.Render("·")))
				}
			}
			countChanged++

		case diffUnchanged:
			if !noUnchanged {
				b.WriteString(fmt.Sprintf("  %s %-55s %s\n",
					dimStyle.Render("="),
					modLabel,
					subtleStyle.Render("(unchanged)")))
			}
			countUnchanged++
		}
	}

	b.WriteString("\n")

	// Summary line
	parts := []string{}
	if countAdded > 0 {
		parts = append(parts, greenStyle.Render(fmt.Sprintf("%d added", countAdded)))
	}
	if countRemoved > 0 {
		parts = append(parts, redStyle.Render(fmt.Sprintf("%d removed", countRemoved)))
	}
	if countChanged > 0 {
		parts = append(parts, yellowStyle.Render(fmt.Sprintf("%d changed", countChanged)))
	}
	if countUnchanged > 0 && !noUnchanged {
		parts = append(parts, dimStyle.Render(fmt.Sprintf("%d unchanged", countUnchanged)))
	}

	if len(parts) == 0 {
		b.WriteString(subtleStyle.Render("  profiles are identical"))
	} else {
		b.WriteString("  " + strings.Join(parts, ", "))
	}

	return strings.TrimRight(b.String(), "\n")
}
