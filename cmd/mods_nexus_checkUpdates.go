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
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var (
	modsNexusCheckUpdatesGame              string
	modsNexusCheckUpdatesForce             bool
	modsNexusCheckUpdatesIgnoreTTL         bool
	modsNexusCheckUpdatesLimit             int
	modsNexusCheckUpdatesPrintAll          bool
	modsNexusCheckUpdatesIncludeSuperseded bool
)

var modsNexusCheckUpdatesCmd = &cobra.Command{
	Use:   "check-updates",
	Short: "Fetch latest file info from Nexus and update the local cache",
	Long: `Fetches the latest file information from Nexus Mods for all linked mod pages
in the active game install and updates the local cache.

For each linked mod file version, checks whether a newer version is available
by walking the Nexus file update chain. Results are displayed immediately and
cached for use by 'profiles status'.

Mods are checked oldest-cached-first so the most stale data is always
refreshed first. Superseded mod versions (where a newer version is already
imported) are skipped by default; pass --include-superseded to check them too.

Use --limit to cap the number of mods checked without erroring. Use --force
to proceed even if the operation would exhaust your API quota.`,
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

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, modsNexusCheckUpdatesGame)
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

		// Free quota check via validate endpoint - also gives us fresh rate limit state
		if _, err := client.ValidateUser(); err != nil {
			logger.Warn("failed to validate nexus user for quota check", "error", err)
		}
		rateLimitState, err := nexusclient.LoadRateLimitState()
		if err != nil {
			logger.Warn("failed to load rate limit state", "error", err)
		}

		// Build set of all imported nexus_file_ids for this game install so we
		// can detect superseded versions (chain head already in local DB)
		allVersions, err := q.GetAllNexusLinkedModFileVersions(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("fetching linked mod file versions: %w", err)
		}
		importedNexusFileIDs := make(map[int64]struct{}, len(allVersions))
		for _, v := range allVersions {
			if v.NexusFileID.Valid {
				importedNexusFileIDs[v.NexusFileID.Int64] = struct{}{}
			}
		}

		// Open cache reader to check TTL and build superseded set
		cacheReader, err := nexusclient.NewCacheReader(ctx, logger)
		if err != nil {
			return fmt.Errorf("opening nexus cache: %w", err)
		}
		defer cacheReader.Close()

		type modPageEntry struct {
			mp        dbq.GetNexusLinkedModPagesRow
			coldCache bool
			fetchedAt time.Time // zero if cold
		}

		// Classify each mod page and build superseded set from cache
		var entries []modPageEntry
		for _, mp := range modPages {
			entry := modPageEntry{mp: mp}

			if modsNexusCheckUpdatesIgnoreTTL {
				entry.coldCache = true
				entries = append(entries, entry)
				continue
			}

			row, err := client.GetNexusFileInfoFetchedAt(
				mp.NexusGameDomain.String,
				mp.NexusModID.Int64,
			)
			if errors.Is(err, sql.ErrNoRows) || err != nil {
				entry.coldCache = true
				entries = append(entries, entry)
				continue
			}
			fetchedAt, err := time.Parse(time.RFC3339, row)
			if err != nil || time.Since(fetchedAt) >= nexusclient.ModFilesTTL {
				entry.coldCache = true
			} else {
				entry.coldCache = false
				entry.fetchedAt = fetchedAt
			}
			entries = append(entries, entry)
		}

		// Filter superseded mod pages unless --include-superseded.
		// A mod page is superseded if every linked version's nexus_file_id
		// appears as an old_file_id in the update chain AND the chain head
		// is already in our local DB.
		if !modsNexusCheckUpdatesIncludeSuperseded {
			var filtered []modPageEntry
			for _, e := range entries {
				chain, err := cacheReader.GetNexusFileUpdateChain(
					e.mp.NexusGameDomain.String,
					e.mp.NexusModID.Int64,
				)
				if err != nil || len(chain) == 0 {
					// no chain data: can't determine superseded, keep it
					filtered = append(filtered, e)
					continue
				}
				next := make(map[int64]int64, len(chain))
				for _, row := range chain {
					next[row.OldFileID] = row.NewFileID
				}
				// Get linked versions for this mod page
				linkedVersions, err := q.GetLinkedModFileVersionsForPage(ctx, e.mp.ModPageID)
				if err != nil || len(linkedVersions) == 0 {
					filtered = append(filtered, e)
					continue
				}
				allSuperseded := true
				for _, v := range linkedVersions {
					if !v.NexusFileID.Valid {
						allSuperseded = false
						break
					}
					head := internal.WalkUpdateChain(v.NexusFileID.Int64, next)
					if head == v.NexusFileID.Int64 {
						// not superseded: this version is already at the head
						allSuperseded = false
						break
					}
					if _, headImported := importedNexusFileIDs[head]; !headImported {
						// superseded but head not imported: still needs attention
						allSuperseded = false
						break
					}
				}
				if !allSuperseded {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}

		if len(entries) == 0 {
			fmt.Println(subtleStyle.Render("  all mods are up to date or superseded"))
			return nil
		}

		// Sort: cold cache first, then warm ordered by fetchedAt ascending
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].coldCache != entries[j].coldCache {
				return entries[i].coldCache
			}
			return entries[i].fetchedAt.Before(entries[j].fetchedAt)
		})

		// Apply --limit
		if modsNexusCheckUpdatesLimit > 0 && modsNexusCheckUpdatesLimit < len(entries) {
			entries = entries[:modsNexusCheckUpdatesLimit]
		}

		// Count how many cold fetches we need and check quota
		needed := int64(0)
		for _, e := range entries {
			if e.coldCache {
				needed++
			}
		}

		if rateLimitState != nil {
			hourly, daily := rateLimitState.EffectiveRemaining()
			conservative := min(int64(hourly), int64(daily)) - 10 // leave headroom of 10 api calls
			if needed > int64(hourly) || needed > int64(daily) {
				fmt.Printf("  ⚠ this operation requires ~%d API calls (hourly remaining: %d, daily remaining: %d)\n",
					needed, hourly, daily)
				if !modsNexusCheckUpdatesForce {
					suggested := max(0, conservative)
					fmt.Println(warnStyle.Render(fmt.Sprintf(
						"  operation aborted: not enough API quota remaining; use --limit %d to check as many mods as your quota allows",
						suggested,
					)))
					return nil
				}
				fmt.Println(warnStyle.Render("  proceeding anyway due to --force"))
			}
		}

		// Get terminal width, fall back to 80 if unavailable
		termWidth := 80
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			termWidth = w
		}

		// Initial progress line
		total := len(entries)
		width := len(strconv.Itoa(total))
		fmtCounter := fmt.Sprintf("[%%%dd/%%%dd]", width, width)

		remaining := int64(0)
		if rateLimitState != nil {
			hourly, daily := rateLimitState.EffectiveRemaining()
			remaining = min(int64(hourly), int64(daily))
		}

		printProgress := func(current int, modName string) {
			line := fmt.Sprintf("  "+fmtCounter+" Checking: %s... (%d calls remaining)",
				current, total, modName, remaining)
			if modsNexusCheckUpdatesPrintAll {
				fmt.Println(line)
			} else {
				fmt.Printf("\r%-*s", termWidth, line)
			}
		}

		if !modsNexusCheckUpdatesPrintAll {
			fmt.Printf("  [%*d/%d] Checking... (%d calls remaining)", width, 0, total, remaining)
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
		coldFetchCount := 0

		for i, e := range entries {
			mp := e.mp
			printProgress(i+1, mp.ModPageName)

			var filesResp *nexusclient.ModFilesResponse

			if e.coldCache {
				fresh, err := client.GetModFiles(mp.NexusGameDomain.String, mp.NexusModID.Int64)
				if err != nil {
					if !modsNexusCheckUpdatesPrintAll {
						fmt.Println()
					}
					fmt.Println(warnStyle.Render(fmt.Sprintf(
						"  ⚠ failed to fetch file info for mod page %d: %s",
						mp.ModPageID, err,
					)))
					failed++
					continue
				}
				filesResp = fresh
				coldFetchCount++
				remaining--

				// Re-sync remaining count from state file every 10 cold fetches
				if coldFetchCount%10 == 0 {
					if s, err := nexusclient.LoadRateLimitState(); err == nil {
						h, d := s.EffectiveRemaining()
						remaining = min(int64(h), int64(d))
					}
				}
			} else {
				cached, err := client.GetModFilesCached(mp.NexusGameDomain.String, mp.NexusModID.Int64)
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

			// Build file version map and update chain
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

			// Group versions by mod_file_id, keeping the one closest to the chain head
			type modFileKey struct {
				modFileID   int64
				modPageName string
				fileLabel   string
			}

			type bestVersion struct {
				nexusFileID    int64
				versionString  string
				distanceToHead int
			}

			best := make(map[modFileKey]bestVersion)

			for _, v := range linkedVersions {
				latestFileID := internal.WalkUpdateChain(v.NexusFileID.Int64, next)

				// compute distance to head by walking the chain
				distance := 0
				cur := v.NexusFileID.Int64
				for cur != latestFileID {
					cur = next[cur]
					distance++
					if distance > 1000 { // safety valve
						break
					}
				}

				key := modFileKey{
					modFileID:   v.ModFileID,
					modPageName: v.ModPageName,
					fileLabel:   v.FileLabel,
				}

				if existing, ok := best[key]; !ok || distance < existing.distanceToHead {
					best[key] = bestVersion{
						nexusFileID:    v.NexusFileID.Int64,
						versionString:  v.VersionString.String,
						distanceToHead: distance,
					}
				}
			}

			for key, b := range best {
				latestFileID := internal.WalkUpdateChain(b.nexusFileID, next)
				hasUpdate := latestFileID != b.nexusFileID
				latestVersion := fileVersions[latestFileID]

				r := updateResult{
					modPageName:    key.modPageName,
					fileLabel:      key.fileLabel,
					currentVersion: b.versionString,
					latestVersion:  latestVersion,
					hasUpdate:      hasUpdate,
				}
				results = append(results, r)

				if hasUpdate {
					// Print update line immediately, breaking out of the \r
					if !modsNexusCheckUpdatesPrintAll {
						fmt.Print("\r" + strings.Repeat(" ", termWidth) + "\r")
					}
					fmt.Printf("  %s\n", nexusUpdateStyle.Render(fmt.Sprintf(
						"↑ %s / %s: %s → %s",
						r.modPageName, r.fileLabel, r.currentVersion, r.latestVersion,
					)))
					// Resume progress line
					if !modsNexusCheckUpdatesPrintAll {
						printProgress(i+1, mp.ModPageName)
					}
				} else if modsNexusCheckUpdatesPrintAll {
					fmt.Printf("  %s\n", subtleStyle.Render(fmt.Sprintf(
						"✓ %s / %s: %s",
						r.modPageName, r.fileLabel, r.currentVersion,
					)))
				}
			}
		}

		// Clear progress line
		if !modsNexusCheckUpdatesPrintAll {
			fmt.Print("\r" + strings.Repeat(" ", termWidth) + "\r")
		}

		// Summary
		updatesAvailable := 0
		for _, r := range results {
			if r.hasUpdate {
				updatesAvailable++
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
		"Proceed even if API quota may be exhausted")
	modsNexusCheckUpdatesCmd.Flags().BoolVar(&modsNexusCheckUpdatesIgnoreTTL, "ignore-ttl", false,
		"Fetch data from Nexus even if cached data is still fresh")
	modsNexusCheckUpdatesCmd.Flags().IntVarP(&modsNexusCheckUpdatesLimit, "limit", "l", 0,
		"Maximum number of mods to check (0 = unlimited)")
	modsNexusCheckUpdatesCmd.Flags().BoolVar(&modsNexusCheckUpdatesPrintAll, "print-all", false,
		"Print each mod on its own line instead of using a progress indicator")
	modsNexusCheckUpdatesCmd.Flags().BoolVar(&modsNexusCheckUpdatesIncludeSuperseded, "include-superseded", false,
		"Include superseded mod versions in the update check")

	modsNexusCheckUpdatesCmd.MarkFlagsMutuallyExclusive("force", "limit")
}
