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
	"sort"
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	modsListGame    string
	modsListDetails bool
)

var modsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List imported mods for the current game",
	Long: `List imported mods (mod pages) for a game install.

By default, this shows one line per mod page with counts and the latest imported
archive across all files under that page.

With --details, the output expands each mod page to show its mod files and their
versions.

TODO:
- Show latest version information from the Nexus API for Nexus-linked mods and
  compare it with imported versions.`,
	Args: cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract these somewhere else
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

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
		if modsListGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			modsListGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, modsListGame)
		if err != nil {
			return err
		}

		rows, err := q.ListModsByGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list mods: %w", err)
		}

		if len(rows) == 0 {
			fmt.Println(subtleStyle.Render("No mods imported for this game yet."))
			fmt.Println(subtleStyle.Render("Use `modctl mods import <archive>` to add one."))
			return nil
		}

		fmt.Println(headerStyle.Render("Mods"))
		fmt.Println()

		// Summary query is already "one row per page" (rn=1). We'll build a stable list of page IDs.
		type pageSummary struct {
			ModPageID   int64
			ModName     string
			SourceKind  string
			NexusDomain sql.NullString
			NexusModID  sql.NullInt64

			FilesCount    int64
			VersionsCount int64

			LatestFileLabel  sql.NullString
			LatestVersionID  sql.NullInt64
			LatestVersionStr sql.NullString
			LatestArchiveSHA sql.NullString
			LatestImportedAt sql.NullString
		}

		pages := make([]pageSummary, 0, len(rows))
		for _, r := range rows {
			pages = append(pages, pageSummary{
				ModPageID:   r.ModPageID,
				ModName:     r.ModName,
				SourceKind:  r.SourceKind,
				NexusDomain: r.NexusGameDomain,
				NexusModID:  r.NexusModID,

				FilesCount:    r.FilesCount,
				VersionsCount: r.VersionsCount,

				LatestFileLabel:  r.ModFileLabel,
				LatestVersionID:  r.ModFileVersionID,
				LatestVersionStr: r.VersionString,
				LatestArchiveSHA: r.ArchiveSha256,
				LatestImportedAt: r.ImportedAt,
			})
		}

		// keep deterministic order even if SQL already sorted.
		sort.Slice(pages, func(i, j int) bool {
			if pages[i].ModName == pages[j].ModName {
				return pages[i].ModPageID < pages[j].ModPageID
			}
			return pages[i].ModName < pages[j].ModName
		})

		// Helper formatters
		shortSHA := func(ns sql.NullString) string {
			if !ns.Valid || ns.String == "" {
				return "—"
			}
			s := ns.String
			if len(s) > 12 {
				s = s[:12]
			}
			return s
		}
		strOrDash := func(ns sql.NullString) string {
			if !ns.Valid || ns.String == "" {
				return "—"
			}
			return ns.String
		}
		intOrDash := func(ni sql.NullInt64) string {
			if !ni.Valid {
				return "—"
			}
			return fmt.Sprintf("%d", ni.Int64)
		}

		if !modsListDetails {
			for _, p := range pages {
				// Header line
				fmt.Printf("%d  %s\n", p.ModPageID, p.ModName)

				// Optional nexus ref
				nexusRef := ""
				if p.NexusDomain.Valid && p.NexusModID.Valid {
					nexusRef = fmt.Sprintf("%s:%d", p.NexusDomain.String, p.NexusModID.Int64)
				}

				line := fmt.Sprintf(
					"  source=%s  files=%d  versions=%d",
					p.SourceKind, p.FilesCount, p.VersionsCount,
				)

				if p.LatestVersionID.Valid {
					line += fmt.Sprintf(
						"  latest_file=%q  latest_version_id=%s  imported_at=%s  sha=%s",
						strOrDash(p.LatestFileLabel),
						intOrDash(p.LatestVersionID),
						strOrDash(p.LatestImportedAt),
						shortSHA(p.LatestArchiveSHA),
					)
					if p.LatestVersionStr.Valid && p.LatestVersionStr.String != "" {
						line += fmt.Sprintf("  version=%q", p.LatestVersionStr.String)
					}
				} else {
					line += "  (no imported archives yet)"
				}

				if nexusRef != "" {
					line += fmt.Sprintf("  nexus=%s", nexusRef)
					// TODO: add "nexus_latest=..." once Nexus API integration exists
				}

				fmt.Println(subtleStyle.Render(line))
				fmt.Println()
			}

			return nil
		}

		for _, p := range pages {
			fmt.Printf("%d  %s\n", p.ModPageID, p.ModName)

			nexusRef := ""
			if p.NexusDomain.Valid && p.NexusModID.Valid {
				nexusRef = fmt.Sprintf("%s:%d", p.NexusDomain.String, p.NexusModID.Int64)
			}

			line := fmt.Sprintf(
				"  source=%s  files=%d  versions=%d",
				p.SourceKind, p.FilesCount, p.VersionsCount,
			)
			if nexusRef != "" {
				line += fmt.Sprintf("  nexus=%s", nexusRef)
				// TODO: add "nexus_latest=..." once Nexus API integration exists
			}
			fmt.Println(subtleStyle.Render(line))

			files, err := q.ListModFilesByPage(ctx, p.ModPageID)
			if err != nil {
				return fmt.Errorf("list mod files (page_id=%d): %w", p.ModPageID, err)
			}

			if len(files) == 0 {
				fmt.Println(subtleStyle.Render("  (no files)"))
				fmt.Println()
				continue
			}

			for _, f := range files {
				primaryTag := ""
				if f.IsPrimary != 0 {
					primaryTag = " (primary)"
				}
				fmt.Println(subtleStyle.Render(fmt.Sprintf("  File: %s%s", f.Label, primaryTag)))

				vers, err := q.ListModFileVersionsByFile(ctx, f.ID)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("list versions (file_id=%d): %w", f.ID, err)
				}
				if len(vers) == 0 {
					fmt.Println(subtleStyle.Render("    (no versions)"))
					continue
				}

				for _, v := range vers {
					vline := fmt.Sprintf(
						"    v%d  imported_at=%s  sha=%s",
						v.ID,
						v.CreatedAt,
						func() string {
							s := v.ArchiveSha256
							if len(s) > 12 {
								s = s[:12]
							}
							return s
						}(),
					)

					if v.VersionString.Valid && v.VersionString.String != "" {
						vline += fmt.Sprintf("  version=%q", v.VersionString.String)
					}

					// TODO: think about also showing v.OriginalName later (only if not-null)
					fmt.Println(subtleStyle.Render(vline))
				}
			}

			fmt.Println()
		}

		return nil
	},
}

func init() {
	modsCmd.AddCommand(modsListCmd)

	modsListCmd.Flags().BoolVarP(&modsListDetails, "details", "d", false,
		"Show per-file and per-version details")
	modsListCmd.Flags().StringVarP(&modsListGame, "game", "g", "",
		"Override the currently active game")
	modsListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
}
