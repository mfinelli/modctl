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
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	modsNexusCheckUpdatesGame      string
	modsNexusCheckUpdatesForce     bool
	modsNexusCheckUpdatesIgnoreTTL bool
)

var modsNexusCheckUpdatesCmd = &cobra.Command{
	Use:   "check-updates",
	Short: "Fetch latest file info from Nexus and update the local cache",
	Long: `Fetches the latest file information from Nexus Mods for all linked mod pages
in the active game install and updates the local cache.

For each linked mod file version, checks whether a newer version is available
by walking the Nexus file update chain. Results are displayed immediately and
cached for use by 'profiles status'.

Use --force to proceed even if the operation would exhaust your API quota.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract styles
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		nexusUpdateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)

		ctx := cmd.Context()

		apiKey := viper.GetString("nexus.apikey")
		if apiKey == "" {
			return fmt.Errorf("nexus api key not configured; set nexus.apikey in your config file")
		}

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

		client, err := nexusclient.New(ctx, apiKey, logger, rootCmd.Version)
		if err != nil {
			return fmt.Errorf("initializing nexus client: %w", err)
		}
		defer client.Close()

		q := dbq.New(db)

		// Resolve game install id: --game overrides active selection
		if modsNexusCheckUpdatesGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			modsNexusCheckUpdatesGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, modsNexusCheckUpdatesGame)
		if err != nil {
			return err
		}

		modPages, err := q.GetNexusLinkedModPages(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("fetching linked mod pages: %w", err)
		}
		if len(modPages) == 0 {
			fmt.Println(subtleStyle.Render("  no linked mod pages found; run 'mods nexus link' first"))
			return nil
		}

		// determine which mod pages actually need a fetch
		needsFetch := make(map[int64]bool, len(modPages))
		for _, mp := range modPages {
			if modsNexusCheckUpdatesIgnoreTTL {
				needsFetch[mp.ModPageID] = true
				continue
			}
			row, err := client.GetNexusFileInfoFetchedAt(
				mp.NexusGameDomain.String,
				mp.NexusModID.Int64,
			)
			if errors.Is(err, sql.ErrNoRows) {
				needsFetch[mp.ModPageID] = true
				continue
			}
			if err != nil {
				// if we can't check, assume we need to fetch
				needsFetch[mp.ModPageID] = true
				continue
			}
			fetchedAt, err := time.Parse(time.RFC3339, row)
			if err != nil || time.Since(fetchedAt) >= nexusclient.ModFilesTTL {
				needsFetch[mp.ModPageID] = true
			}
		}

		needed := int64(0)
		for _, needs := range needsFetch {
			if needs {
				needed++
			}
		}

		// Pre-flight rate limit check
		rateLimitState, err := client.RateLimitState()
		if err != nil {
			logger.Warn("failed to load rate limit state", "error", err)
		} else {
			hourly, daily := rateLimitState.EffectiveRemaining()
			if needed > int64(hourly) || needed > int64(daily) {
				fmt.Printf("  ⚠ this operation requires %d API calls (hourly remaining: %d, daily remaining: %d)\n",
					needed, hourly, daily)
				if !modsNexusCheckUpdatesForce {
					fmt.Println(warnStyle.Render("  operation aborted: not enough API quota remaining; pass --force to proceed anyway"))
					return nil
				}
				fmt.Println(warnStyle.Render("  proceeding anyway due to --force"))
			}
		}

		type updateResult struct {
			modPageName    string
			fileLabel      string
			currentVersion string
			latestVersion  string
			hasUpdate      bool
		}

		var results []updateResult
		failed := 0

		for _, mp := range modPages {
			var filesResp *nexusclient.ModFilesResponse

			if needsFetch[mp.ModPageID] {
				fresh, err := client.GetModFiles(mp.NexusGameDomain.String, int(mp.NexusModID.Int64))
				if err != nil {
					fmt.Println(warnStyle.Render(fmt.Sprintf(
						"  ⚠ failed to fetch file info for mod page %d: %s",
						mp.ModPageID, err,
					)))
					failed++
					continue
				}
				filesResp = fresh
			} else {
				cached, err := client.GetModFilesCached(mp.NexusGameDomain.String, int(mp.NexusModID.Int64))
				if err != nil {
					logger.Warn("failed to read cached mod files",
						"mod_page_id", mp.ModPageID,
						"error", err,
					)
					failed++
					continue
				}
				filesResp = cached
			}

			// build a map of file_id -> version for quick lookup
			fileVersions := make(map[int64]string, len(filesResp.Files))
			for _, f := range filesResp.Files {
				fileVersions[int64(f.FileID)] = f.Version
			}

			// build update chain map
			next := make(map[int64]int64, len(filesResp.FileUpdates))
			for _, u := range filesResp.FileUpdates {
				next[int64(u.OldFileID)] = int64(u.NewFileID)
			}

			// get all linked versions for this mod page
			linkedVersions, err := q.GetLinkedModFileVersionsForPage(ctx, mp.ModPageID)
			if err != nil {
				failed++
				continue
			}

			for _, v := range linkedVersions {
				latestFileID := internal.WalkUpdateChain(v.NexusFileID.Int64, next)
				hasUpdate := latestFileID != v.NexusFileID.Int64

				currentVersion := v.VersionString.String
				latestVersion := fileVersions[latestFileID]

				results = append(results, updateResult{
					modPageName:    v.ModPageName,
					fileLabel:      v.FileLabel,
					currentVersion: currentVersion,
					latestVersion:  latestVersion,
					hasUpdate:      hasUpdate,
				})
			}
		}

		// Output results
		if len(results) == 0 && failed == 0 {
			fmt.Println(subtleStyle.Render("  no linked mod file versions found"))
			return nil
		}

		updatesAvailable := 0
		for _, r := range results {
			if r.hasUpdate {
				updatesAvailable++
				fmt.Printf("  %s\n", nexusUpdateStyle.Render(fmt.Sprintf(
					"↑ %s / %s: %s → %s",
					r.modPageName, r.fileLabel, r.currentVersion, r.latestVersion,
				)))
			} else {
				fmt.Printf("  %s\n", subtleStyle.Render(fmt.Sprintf(
					"✓ %s / %s: %s",
					r.modPageName, r.fileLabel, r.currentVersion,
				)))
			}
		}

		fmt.Println()
		if updatesAvailable > 0 {
			fmt.Printf("  %s\n", nexusUpdateStyle.Render(fmt.Sprintf(
				"%d update(s) available", updatesAvailable,
			)))
		} else {
			fmt.Println(subtleStyle.Render("  all mods are up to date"))
		}
		if failed > 0 {
			fmt.Println(warnStyle.Render(fmt.Sprintf("  ⚠ %d mod page(s) failed to fetch", failed)))
		}

		return nil
	},
}

func init() {
	modsNexusCmd.AddCommand(modsNexusCheckUpdatesCmd)

	modsNexusCheckUpdatesCmd.Flags().StringVarP(&modsNexusCheckUpdatesGame, "game", "g", "",
		"Override the currently active game")
	modsNexusCheckUpdatesCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	modsNexusCheckUpdatesCmd.Flags().BoolVar(&modsNexusCheckUpdatesForce, "force", false,
		"proceed even if API quota may be exhausted")
	modsNexusCheckUpdatesCmd.Flags().BoolVar(&modsNexusCheckUpdatesIgnoreTTL, "ignore-ttl", false,
		"fetch data from the nexus even if cached data is fresh")
}
