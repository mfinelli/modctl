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

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
)

var (
	profilesOverridesStatusGame    string
	profilesOverridesStatusProfile string
)

var profilesOverridesStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show detailed staleness information for overrides",
	Long: `Show detailed staleness information for all overrides in a profile.

For each override, shows the staleness state, source anchor information,
and the current base mod providing that path (if any).

Pass a path argument to show detail for a single override only.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		kvIndented := style.KV{Indent: 6, Width: 20}

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

		if profilesOverridesStatusGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesStatusGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesStatusGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesStatusProfile)
		if err != nil {
			return err
		}

		rows, err := q.GetOverrideStatusDetail(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("loading override status: %w", err)
		}

		// filter to single path if arg provided
		if len(args) == 1 {
			path := args[0]
			var filtered []dbq.GetOverrideStatusDetailRow
			for _, r := range rows {
				if r.Relpath == path {
					filtered = append(filtered, r)
					break
				}
			}
			if len(filtered) == 0 {
				return fmt.Errorf("no override found for path %q in profile %q", path, p.Name)
			}
			rows = filtered
		}

		if len(rows) == 0 {
			fmt.Println(style.Subtle.Render(fmt.Sprintf(
				"  no overrides for profile %q", p.Name,
			)))
			return nil
		}

		fmt.Println(style.Bold.Render(fmt.Sprintf(
			"Override status for profile %q:", p.Name,
		)))

		for _, r := range rows {
			fmt.Println()
			fmt.Printf("  %s\n", style.Bold.Render(r.Relpath))

			kvIndented.Print("type:", formatOverrideType(r.OverrideType))

			// staleness state with explanation
			stateStr, explanation := formatStalenessState(r.StalenessState)
			kvIndented.Print("status:", stateStr)
			if explanation != "" {
				kvIndented.Print("", style.Subtle.Render(explanation))
			}

			// source anchor
			if r.SourceArchiveSha256.Valid {
				kvIndented.Print("source archive:", style.ShortSha(r.SourceArchiveSha256.String))
			}
			if r.SourceRawPath.Valid {
				kvIndented.Print("source path:", r.SourceRawPath.String)
			}

			// current base
			if r.CurrentArchiveSha256 != "" {
				same := r.SourceArchiveSha256.Valid &&
					r.CurrentArchiveSha256 == r.SourceArchiveSha256.String
				currentStr := style.ShortSha(r.CurrentArchiveSha256)
				if same {
					currentStr += "  " + style.Subtle.Render("(unchanged)")
				} else if r.SourceArchiveSha256.Valid {
					currentStr += "  " + style.Warning.Render("(changed)")
				}
				kvIndented.Print("current base:", currentStr)
			} else if r.SourceArchiveSha256.Valid {
				kvIndented.Print("current base:", style.Warning.Render("(none — base mod removed from profile)"))
			}

			if r.BlobSha256.Valid {
				kvIndented.Print("blob:", style.ShortSha(r.BlobSha256.String))
			}
			if r.Notes.Valid && strings.TrimSpace(r.Notes.String) != "" {
				kvIndented.Print("notes:", r.Notes.String)
			}
			kvIndented.Print("updated:", r.UpdatedAt)
		}

		// summary line
		fmt.Println()
		counts := map[string]int{}
		for _, r := range rows {
			counts[r.StalenessState]++
		}
		var parts []string
		if n := counts["base_unchanged"]; n > 0 {
			parts = append(parts, style.Good.Render(fmt.Sprintf("%d current", n)))
		}
		if n := counts["net_new_no_anchor"]; n > 0 {
			parts = append(parts, style.Subtle.Render(fmt.Sprintf("%d net-new", n)))
		}
		if n := counts["stale"]; n > 0 {
			parts = append(parts, style.Warning.Render(fmt.Sprintf("%d stale", n)))
		}
		if n := counts["no_base"]; n > 0 {
			parts = append(parts, style.Warning.Render(fmt.Sprintf("%d no base", n)))
		}
		if n := counts["anchor_lost"]; n > 0 {
			parts = append(parts, style.Warning.Render(fmt.Sprintf("%d anchor lost", n)))
		}
		if len(parts) > 0 {
			fmt.Println(style.Subtle.Render("  " + strings.Join(parts, "  ·  ")))
		}

		return nil
	},
}

func init() {
	profilesOverridesCmd.AddCommand(profilesOverridesStatusCmd)

	profilesOverridesStatusCmd.Flags().StringVarP(&profilesOverridesStatusGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesStatusCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesOverridesStatusCmd.Flags().StringVarP(&profilesOverridesStatusProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesStatusCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}

func formatStalenessState(state string) (display, explanation string) {
	switch state {
	case "base_unchanged":
		return style.Good.Render("✓ current"),
			"base file has not changed since override was created"
	case "stale":
		return style.Warning.Render("⚠ may be stale"),
			"base file has changed; review your override"
	case "redundant":
		return style.Changed.Render("~ redundant"),
			"override content matches the base file (it may no longer be necessary)"
	case "no_base":
		return style.Warning.Render("⚠ no base mod"),
			"no mod in this profile provides this path"
	case "anchor_lost":
		return style.Warning.Render("⚠ anchor lost"),
			"source archive was removed, staleness cannot be determined"
	case "net_new_no_anchor":
		return style.Subtle.Render("net-new"),
			"override writes a file not provided by any mod"
	default:
		return state, ""
	}
}
