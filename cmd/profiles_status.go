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
	"strconv"
	"strings"
	"time"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/mfinelli/modctl/internal/nexusclient/dbc"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
	"go.finelli.dev/util"
)

var (
	profilesStatusGame    string
	profilesStatusProfile string
	profilesStatusCompact bool
)

var profilesStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the contents and state of a profile",
	Long: `Show the contents and state of a profile

Displays the mods in the profile in priority order, their enabled/disabled
state, version information, and any warnings such as missing inventory scans
or mod incompatibilities.

When the profile is currently applied, pending changes are detected by
comparing the set of enabled mod versions against installed files. This
check does not account for priority reordering between mods that conflict
on the same path - run 'modctl apply --dry-run' for a precise diff.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

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

		cacheReader, err := nexusclient.NewCacheReader(ctx, logger)
		if err != nil {
			// non-fatal, just means we can't show nexus version info
			logger.Warn("failed to open nexus cache", "error", err)
		} else {
			defer cacheReader.Close()
		}

		q := dbq.New(db)

		// Resolve game install id: --game overrides active selection
		if profilesStatusGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesStatusGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesStatusGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesStatusProfile)
		if err != nil {
			return err
		}

		items, err := q.GetProfileStatusItems(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("loading profile items: %w", err)
		}

		appliedState, err := q.GetGameInstallAppliedState(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("loading applied state: %w", err)
		}

		var hasPendingChanges bool
		if appliedState.AppliedProfileID.Valid && appliedState.AppliedProfileID.Int64 == p.ID {
			pendingResult, err := q.GetProfileHasPendingChanges(ctx, dbq.GetProfileHasPendingChangesParams{
				ProfileID:     p.ID,
				GameInstallID: gi.ID,
			})
			if err == nil {
				hasPendingChanges = pendingResult.Bool
			}
		}

		// Only fetch incompatibilities if there are mods to check
		var incompatibilities []dbq.GetIncompatibleModPairsForProfileRow
		if len(items) > 0 {
			incompatibilities, err = q.GetIncompatibleModPairsForProfile(ctx, p.ID)
			if err != nil {
				return fmt.Errorf("loading incompatibilities: %w", err)
			}
		}

		// pass cacheReader (possibly nil) to the nexusInfo builder
		nexusInfo := buildNexusInfo(ctx, items, cacheReader)

		// fetch override count and staleness heuristic
		overrideCount, err := q.CountOverridesByProfile(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("loading override count: %w", err)
		}

		var staleOverrides []dbq.GetStalenessHeuristicForProfileRow
		if overrideCount > 0 {
			staleOverrides, err = q.GetStalenessHeuristicForProfile(ctx, p.ID)
			if err != nil {
				return fmt.Errorf("loading override staleness: %w", err)
			}
		}

		fmt.Println(renderProfileStatus(
			p,
			gi,
			items,
			appliedState,
			incompatibilities,
			nexusInfo,
			hasPendingChanges,
			overrideCount,
			staleOverrides,
			profilesStatusCompact,
		))

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesStatusCmd)

	profilesStatusCmd.Flags().StringVarP(&profilesStatusGame, "game", "g", "",
		"Override the currently active game")
	profilesStatusCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesStatusCmd.Flags().StringVarP(&profilesStatusProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesStatusCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	profilesStatusCmd.Flags().BoolVar(&profilesStatusCompact, "compact", false,
		"Show a condensed one-line-per-mod summary")
}

type nexusVersionInfo struct {
	CachedVersion string // version of what the user has installed
	LatestVersion string // version of the latest available (only set when HasUpdate is true)
	FetchedAt     time.Time
	HasUpdate     bool
}

func renderProfileStatus(
	profile dbq.Profile,
	gi dbq.GameInstall,
	items []dbq.GetProfileStatusItemsRow,
	appliedState dbq.GetGameInstallAppliedStateRow,
	incompatibilities []dbq.GetIncompatibleModPairsForProfileRow,
	nexusInfo map[int64]*nexusVersionInfo,
	hasPendingChanges bool,
	overrideCount int64,
	staleOverrides []dbq.GetStalenessHeuristicForProfileRow,
	compact bool,
) string {
	kv := style.KV{Indent: 2, Width: 16}
	kvIndented := style.KV{Indent: 6, Width: 16}

	var b strings.Builder

	// header card
	fullSel := internal.FullSelector(gi.StoreID, gi.StoreGameID, gi.InstanceID)
	shortSel := internal.ShortSelector(gi.StoreID, gi.StoreGameID, gi.InstanceID)
	selText := fullSel
	if shortSel != fullSel {
		selText = fmt.Sprintf("%s (short: %s)", fullSel, shortSel)
	}
	headerContent := style.Title.Render(profile.Name)
	if profile.IsActive != 0 {
		headerContent += "   " + style.ActiveTag.Render("(active)")
	}
	headerContent += "\n" + style.Dim.Render(selText)
	if profile.Description.Valid && strings.TrimSpace(profile.Description.String) != "" {
		headerContent += "\n" + style.Subtle.Render(profile.Description.String)
	}
	b.WriteString(style.Card.Render(headerContent))
	b.WriteString("\n")

	// apply state (omitted if profile has never been applied)
	if appliedState.AppliedProfileID.Valid &&
		appliedState.AppliedProfileID.Int64 == profile.ID {
		if compact {
			pendingText := style.Subtle.Render("none")
			if hasPendingChanges {
				pendingText = style.Warning.Render("yes ⚠")
			}
			b.WriteString(style.Section.Render("Apply State") + "\n")
			b.WriteString(fmt.Sprintf("  Applied · %s · pending changes: %s\n",
				appliedState.AppliedAt.String, pendingText))
		} else {
			b.WriteString(style.Section.Render("Apply State") + "\n")
			kv.Write(&b, "Applied at:", appliedState.AppliedAt.String)
			if appliedState.AppliedOperationID.Valid {
				kv.Write(&b, "Operation:", fmt.Sprintf("#%d", appliedState.AppliedOperationID.Int64))
			}
			if hasPendingChanges {
				kv.Write(&b, "Pending changes:", style.Warning.Render("yes ⚠"))
			} else {
				kv.Write(&b, "Pending changes:", style.Subtle.Render("none"))
			}
		}
	}

	// mods section
	b.WriteString(style.Section.Render(fmt.Sprintf("Mods (%d)", len(items))) + "\n")

	if len(items) == 0 {
		b.WriteString(style.Subtle.Render("  (none)") + "\n")
	} else if compact {
		for _, item := range items {
			dot := style.InactiveDot()
			if util.SqliteIntToBool(item.Enabled) {
				dot = style.ActiveDot()
			}

			versionStr := style.Subtle.Render("(no version)")
			if item.VersionString.Valid {
				versionStr = item.VersionString.String
			}

			line := fmt.Sprintf("  %s [%d] %s  %s  %s",
				dot, item.Priority, item.ModPageName, item.FileLabel, versionStr)

			if info, ok := nexusInfo[item.ModFileVersionID]; ok && info.HasUpdate {
				line += "  " + style.UpdateAvailable.Render("↑")
			}

			if !util.SqliteIntToBool(item.Enabled) {
				line += "   " + style.Inactive.Render("(disabled)")
			}

			b.WriteString(line + "\n")
		}
	} else {
		for _, item := range items {
			dot := style.InactiveDot()
			if util.SqliteIntToBool(item.Enabled) {
				dot = style.ActiveDot()
			}

			// mod header line: ● [1] Mod Name
			modLine := fmt.Sprintf("  %s [%d] %s", dot, item.Priority, item.ModPageName)
			if !util.SqliteIntToBool(item.Enabled) {
				modLine += "   " + style.Inactive.Render("(disabled)")
			}
			b.WriteString(modLine + "\n")

			// nested KV fields
			kvIndented.Write(&b, "file:", item.FileLabel)

			if item.VersionString.Valid {
				kvIndented.Write(&b, "version:", item.VersionString.String)
			} else {
				kvIndented.Write(&b, "version:", style.Subtle.Render("(none)"))
			}

			if item.NexusFileID.Valid {
				if info, ok := nexusInfo[item.ModFileVersionID]; ok {
					if info.HasUpdate {
						kvIndented.Write(&b, "nexus version:",
							style.UpdateAvailable.Render(fmt.Sprintf("%s ↑ update available", info.LatestVersion)))
					} else {
						kvIndented.Write(&b, "nexus version:",
							fmt.Sprintf("%s ✓ %s",
								info.CachedVersion,
								style.Subtle.Render(fmt.Sprintf("(last fetched %s)", style.Age(info.FetchedAt))),
							))
					}
				} else {
					kvIndented.Write(&b, "nexus version:",
						style.Subtle.Render("(run 'mods nexus check-updates' to fetch)"))
				}
			}

			kvIndented.Write(&b, "archive:", style.ShortSha(item.ArchiveSha256))
			kvIndented.Write(&b, "size:", style.Bytes(item.SizeBytes))

			if item.ItemNotes.Valid && strings.TrimSpace(item.ItemNotes.String) != "" {
				kvIndented.Write(&b, "notes:", item.ItemNotes.String)
			}

			if item.RemapRuleCount > 0 {
				kvIndented.Write(&b, "remap rules:",
					style.Subtle.Render(fmt.Sprintf("%d active (run 'profiles remap list %d' to view)",
						item.RemapRuleCount, item.ModFileVersionID)))
			}

			if item.TargetName != "game_dir" {
				kvIndented.Write(&b, "target:", item.TargetName)
			}

			b.WriteString("\n")
		}
	}

	// categorize stale overrides
	var staleCount, noBaseCount, anchorLostCount int
	for _, s := range staleOverrides {
		switch s.Staleness {
		case "stale":
			staleCount++
		case "no_base":
			noBaseCount++
		case "anchor_lost":
			anchorLostCount++
		}
	}

	// info section
	var infos []string

	if overrideCount > 0 {
		infos = append(infos, fmt.Sprintf(
			"ℹ  %d override(s) active - run 'profiles overrides list' for details",
			overrideCount,
		))
	}

	if len(infos) > 0 {
		b.WriteString(style.Section.Render("Info") + "\n")
		for _, info := range infos {
			b.WriteString(style.Info.Render(info) + "\n")
		}
		b.WriteString("\n")
	}

	// warnings section
	var warnings []string

	uninventoried := 0
	for _, item := range items {
		if !item.InventoryScannedAt.Valid {
			uninventoried++
		}
	}
	if uninventoried > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"⚠  %d mod(s) have no inventory scan - run 'mods scan-inventory' to populate",
			uninventoried,
		))
	}

	updatesAvailable := 0
	for _, info := range nexusInfo {
		if info.HasUpdate {
			updatesAvailable++
		}
	}
	if updatesAvailable > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"⚠  %d mod(s) have updates available", updatesAvailable,
		))
	}

	if staleCount > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"⚠  %d override(s) may be stale - run 'profiles overrides status' for details",
			staleCount,
		))
	}

	if noBaseCount > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"⚠  %d override(s) have no base mod - run 'profiles overrides status' for details",
			noBaseCount,
		))
	}

	if anchorLostCount > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"⚠  %d override(s) have lost their source anchor - run 'profiles overrides status' for details",
			anchorLostCount,
		))
	}

	for _, pair := range incompatibilities {
		w := fmt.Sprintf("⚠  %s and %s are marked incompatible",
			pair.ModPageNameA, pair.ModPageNameB)
		if pair.Reason.Valid && strings.TrimSpace(pair.Reason.String) != "" {
			w += fmt.Sprintf(" (%s)", pair.Reason.String)
		}
		warnings = append(warnings, w)
	}

	if len(warnings) > 0 {
		b.WriteString(style.Section.Render("Warnings") + "\n")
		for _, w := range warnings {
			b.WriteString(style.Banner.Render(style.Warning.Render(w)) + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func buildNexusInfo(
	ctx context.Context,
	items []dbq.GetProfileStatusItemsRow,
	cache *nexusclient.CacheReader,
) map[int64]*nexusVersionInfo {
	result := make(map[int64]*nexusVersionInfo)
	if cache == nil {
		return result
	}

	// cache update chains per mod page to avoid redundant lookups
	type modPageKey struct {
		domain string
		modID  int64
	}
	chains := make(map[modPageKey][]dbc.GetNexusFileUpdateChainRow)

	for _, item := range items {
		if !item.NexusFileID.Valid ||
			!item.NexusGameDomain.Valid ||
			!item.NexusModID.Valid {
			continue
		}

		key := modPageKey{item.NexusGameDomain.String, item.NexusModID.Int64}

		// fetch chain once per mod page
		if _, ok := chains[key]; !ok {
			chain, err := cache.GetNexusFileUpdateChain(
				item.NexusGameDomain.String,
				item.NexusModID.Int64,
			)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				logger.Warn("failed to fetch nexus file update chain",
					"game_domain", key.domain,
					"mod_id", key.modID,
					"error", err,
				)
			}
			chains[key] = chain // store even if empty/nil so we don't retry
		}

		// build next map from the cached chain for this mod page
		next := make(map[int64]int64, len(chains[key]))
		for _, row := range chains[key] {
			next[row.OldFileID] = row.NewFileID
		}
		latestFileID := internal.WalkUpdateChain(item.NexusFileID.Int64, next)
		hasUpdate := latestFileID != item.NexusFileID.Int64

		// fetch current file info for fetched_at and version
		row, err := cache.GetNexusFileInfo(
			item.NexusGameDomain.String,
			item.NexusModID.Int64,
			item.NexusFileID.Int64,
		)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			logger.Warn("failed to fetch nexus file info from cache",
				"mod_file_version_id", item.ModFileVersionID,
				"error", err,
			)
			continue
		}

		fetchedAt, err := time.Parse(time.RFC3339, row.FetchedAt)
		if err != nil {
			continue
		}

		info := &nexusVersionInfo{
			CachedVersion: row.Version.String,
			FetchedAt:     fetchedAt,
			HasUpdate:     hasUpdate,
		}

		if hasUpdate {
			latestRow, err := cache.GetNexusFileInfo(
				item.NexusGameDomain.String,
				item.NexusModID.Int64,
				latestFileID,
			)
			if err == nil && latestRow.Version.Valid {
				info.LatestVersion = latestRow.Version.String
			}
		}

		result[item.ModFileVersionID] = info
	}

	return result
}
