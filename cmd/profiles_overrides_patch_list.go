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
	"path/filepath"
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
	profilesOverridesPatchListGame    string
	profilesOverridesPatchListProfile string
)

var profilesOverridesPatchListCmd = &cobra.Command{
	Use:   "list <path>",
	Short: "List patch entries for a file override",
	Long: `List all patch entries for a file override in the active profile.

Entries are shown in the order they will be applied.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		boldStyle := lipgloss.NewStyle().Bold(true)

		ctx := cmd.Context()
		relpath := filepath.Clean(args[0])

		if filepath.IsAbs(relpath) {
			return fmt.Errorf("path must be relative, got %q", relpath)
		}

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

		if profilesOverridesPatchListGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesPatchListGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesPatchListGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesPatchListProfile)
		if err != nil {
			return err
		}

		target, err := q.GetTargetByName(ctx, dbq.GetTargetByNameParams{
			GameInstallID: gi.ID,
			Name:          "game_dir",
		})
		if err != nil {
			return fmt.Errorf("resolve game_dir target: %w", err)
		}

		override, err := q.GetOverride(ctx, dbq.GetOverrideParams{
			ProfileID: p.ID,
			TargetID:  target.ID,
			Relpath:   relpath,
		})
		if err != nil {
			return fmt.Errorf("no patch override found for %q in profile %q", relpath, p.Name)
		}

		if override.OverrideType == "full_file" {
			return fmt.Errorf(
				"override for %q is a full-file override — use 'profiles overrides list' instead",
				relpath,
			)
		}

		entries, err := q.ListOverridePatchEntries(ctx, override.ID)
		if err != nil {
			return fmt.Errorf("list patch entries: %w", err)
		}

		if len(entries) == 0 {
			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"  no patch entries for %q in profile %q", relpath, p.Name,
			)))
			return nil
		}

		fmt.Println(boldStyle.Render(fmt.Sprintf(
			"Patch entries for %q in profile %q (%s):",
			relpath, p.Name, formatOverrideType(override.OverrideType),
		)))
		fmt.Println()

		for _, e := range entries {
			fmt.Println(formatPatchEntry(e))
		}

		return nil
	},
}

func init() {
	profilesOverridesPatchCmd.AddCommand(profilesOverridesPatchListCmd)

	profilesOverridesPatchListCmd.Flags().StringVarP(&profilesOverridesPatchListGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesPatchListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesOverridesPatchListCmd.Flags().StringVarP(&profilesOverridesPatchListProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesPatchListCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}

func formatPatchEntry(e dbq.OverridePatchEntry) string {
	var b strings.Builder
	switch {
	case e.PatchType == "ini_set" || e.PatchType == "ini_unset":
		if e.EntrySection.Valid {
			b.WriteString(fmt.Sprintf("  [%d] [%s] %s", e.Position, e.EntrySection.String, e.EntryKey))
		} else {
			b.WriteString(fmt.Sprintf("  [%d] %s", e.Position, e.EntryKey))
		}
	default:
		b.WriteString(fmt.Sprintf("  [%d] %s", e.Position, e.EntryKey))
	}

	if e.EntryValue.Valid {
		b.WriteString(fmt.Sprintf(" = %s", e.EntryValue.String))
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(" (unset)"))
	}

	opStr := ""
	switch e.PatchType {
	case "ini_unset", "yaml_unset", "json_unset":
		opStr = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(" [-]")
	case "ini_set", "yaml_set", "json_set":
		opStr = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(" [+]")
	}
	b.WriteString(opStr)

	return b.String()
}
