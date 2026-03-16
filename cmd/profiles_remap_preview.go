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

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/remap"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesRemapPreviewGame         string
	profilesRemapPreviewProfile      string
	profilesRemapPreviewShowFiltered bool
)

var profilesRemapPreviewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Preview how remap rules transform a mod's archive entries",
	Long: `Preview how remap rules transform a mod's archive entries.

Shows every file entry in the mod archive and the destination path it would
be installed to after all remap rules have been applied. Entries filtered
out by the rules are hidden by default; pass --show-filtered to see them
alongside the reason they were excluded.

The archive must have been inventoried first. Run 'modctl mods scan-inventory'
if needed.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.ModFileVersionIDs(cmd, toComplete)
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		boldStyle := lipgloss.NewStyle().Bold(true)
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

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

		if profilesRemapPreviewGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesRemapPreviewGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesRemapPreviewGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesRemapPreviewProfile)
		if err != nil {
			return err
		}

		mfv, err := internal.ResolveModFileVersionArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		if !mfv.InventoryScannedAt.Valid {
			return fmt.Errorf(
				"archive for mod file version %d has not been inventoried\n"+
					"run 'modctl mods scan-inventory' to fix",
				mfv.ID,
			)
		}

		itemID, err := internal.ResolveProfileItemByVersion(ctx, &p, q, mfv.ID)
		if err != nil {
			return err
		}

		rules, err := q.ListRemapRulesForProfileItem(ctx, itemID)
		if err != nil {
			return fmt.Errorf("load remap rules: %w", err)
		}

		entries, err := q.GetInventoryEntriesForArchive(ctx, mfv.ArchiveSha256)
		if err != nil {
			return fmt.Errorf("load inventory: %w", err)
		}

		label, err := q.GetModFileVersionLabel(ctx, mfv.ID)
		if err != nil {
			return fmt.Errorf("load mod label: %w", err)
		}

		// Header
		versionSuffix := ""
		if mfv.VersionString.Valid && mfv.VersionString.String != "" {
			versionSuffix = fmt.Sprintf(" (%s)", mfv.VersionString.String)
		}
		fmt.Println(boldStyle.Render(fmt.Sprintf(
			"Remap preview for version %d in profile %q",
			mfv.ID, p.Name,
		)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf(
			"%s / %s%s", label.ModPageName, label.FileLabel, versionSuffix,
		)))
		fmt.Println()

		// Convert ListRemapRulesForProfileItemRow to dbq.RemapRule.
		remapRules := make([]dbq.RemapRule, len(rules))
		for i, r := range rules {
			remapRules[i] = dbq.RemapRule{
				RuleType:  r.RuleType,
				IntValue:  r.IntValue,
				TextValue: r.TextValue,
				Position:  r.Position,
			}
		}

		type previewEntry struct {
			source     string
			dest       string
			skipReason string
		}

		var included []previewEntry
		var excluded []previewEntry

		for _, entry := range entries {
			if entry.EntryType != "file" {
				continue
			}
			if !entry.RawPath.Valid {
				continue
			}

			result, err := remap.Apply(remapRules, entry.RawPath.String)
			if err != nil {
				fmt.Println(warnStyle.Render(fmt.Sprintf(
					"  ⚠  remap error for %q: %v", entry.RawPath.String, err,
				)))
				continue
			}

			if result.Skip {
				excluded = append(excluded, previewEntry{
					source:     entry.RawPath.String,
					skipReason: result.SkipReason,
				})
			} else {
				included = append(included, previewEntry{
					source: entry.RawPath.String,
					dest:   result.Path,
				})
			}
		}

		// Summary line
		if len(rules) == 0 {
			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"no remap rules:  all %d files pass through as-is",
				len(included),
			)))
		} else {
			total := len(included) + len(excluded)
			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"%d rule(s), %d of %d entries included",
				len(rules), len(included), total,
			)))
		}
		fmt.Println()

		// Included entries
		if len(included) == 0 {
			fmt.Println(warnStyle.Render("  ⚠  no entries pass the current remap rules"))
		} else {
			for _, e := range included {
				if e.source == e.dest {
					fmt.Printf("  %s\n", e.source)
				} else {
					fmt.Printf("  %s → %s\n", e.source, e.dest)
				}
			}
		}

		// Excluded entries
		if len(excluded) > 0 {
			if profilesRemapPreviewShowFiltered {
				fmt.Println()
				fmt.Println(subtleStyle.Render(fmt.Sprintf("Excluded (%d):", len(excluded))))
				for _, e := range excluded {
					fmt.Printf("  %s  %s\n",
						redStyle.Render("✗ "+e.source),
						subtleStyle.Render(e.skipReason),
					)
				}
			} else {
				fmt.Println()
				fmt.Println(subtleStyle.Render(fmt.Sprintf(
					"(%d entries excluded, pass --show-filtered to see them)",
					len(excluded),
				)))
			}
		}

		return nil
	},
}

func init() {
	profilesRemapCmd.AddCommand(profilesRemapPreviewCmd)

	profilesRemapPreviewCmd.Flags().StringVarP(&profilesRemapPreviewGame, "game", "g", "",
		"Override the currently active game")
	profilesRemapPreviewCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesRemapPreviewCmd.Flags().StringVarP(&profilesRemapPreviewProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesRemapPreviewCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	profilesRemapPreviewCmd.Flags().BoolVar(&profilesRemapPreviewShowFiltered, "show-filtered", false,
		"Show entries excluded by remap rules alongside the reason they were excluded")
}
