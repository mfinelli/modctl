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
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/mfinelli/modctl/internal/nexusclient/dbc"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"go.finelli.dev/util"
)

var (
	profilesStatusGame    string
	profilesStatusProfile string
)

var profilesStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the contents and state of a profile",
	Long: `Show the contents and state of a profile

Displays the mods in the profile in priority order, their enabled/disabled
state, version information, and any warnings such as missing inventory scans
or mod incompatibilities.`,
	Args: cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
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

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesStatusGame)
		if err != nil {
			return err
		}

		p, err := internal.ResolveProfileArg(ctx, q, &gi, profilesStatusProfile)
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

		fmt.Println(renderProfileStatus(
			p,
			gi,
			items,
			appliedState,
			incompatibilities,
			nexusInfo,
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

	profilesStatusCmd.Flags().StringVar(&profilesStatusProfile, "profile", "p",
		"Override the currently active profile")
	profilesStatusCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
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
) string {
	// styles TODO extract somewhere...
	cardBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)
	titleStyle := lipgloss.NewStyle().
		Bold(true)
	selectorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))
	sectionTitleStyle := lipgloss.NewStyle().
		Bold(true).
		MarginTop(1)
	activeTagStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("10"))
	disabledTagStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))
	subtleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))
	warnStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("3"))
	warningBanner := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("11")).
		Padding(0, 1)
	nexusUpdateStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Bold(true)
	activeDot := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("●")
	inactiveDot := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("○")

	var b strings.Builder

	// header card
	fullSel := internal.FullSelector(gi.StoreID, gi.StoreGameID, gi.InstanceID)
	shortSel := internal.ShortSelector(gi.StoreID, gi.StoreGameID, gi.InstanceID)
	selText := fullSel
	if shortSel != fullSel {
		selText = fmt.Sprintf("%s (short: %s)", fullSel, shortSel)
	}
	headerContent := titleStyle.Render(profile.Name)
	if profile.IsActive != 0 {
		headerContent += "   " + activeTagStyle.Render("(active)")
	}
	headerContent += "\n" + selectorStyle.Render(selText)
	if profile.Description.Valid && strings.TrimSpace(profile.Description.String) != "" {
		headerContent += "\n" + subtleStyle.Render(profile.Description.String)
	}
	b.WriteString(cardBorder.Render(headerContent))
	b.WriteString("\n")

	// apply state (omitted if profile has never been applied)
	if appliedState.AppliedProfileID.Valid &&
		appliedState.AppliedProfileID.Int64 == profile.ID {
		b.WriteString(sectionTitleStyle.Render("Apply State") + "\n")
		writeKV16(&b, "Applied at:", appliedState.AppliedAt.String)
		if appliedState.AppliedOperationID.Valid {
			writeKV16(&b, "Operation:", fmt.Sprintf("#%d", appliedState.AppliedOperationID.Int64))
		}
	}

	// mods section
	b.WriteString(sectionTitleStyle.Render(fmt.Sprintf("Mods (%d)", len(items))) + "\n")

	if len(items) == 0 {
		b.WriteString(subtleStyle.Render("  (none)") + "\n")
	} else {
		for _, item := range items {
			dot := inactiveDot
			if util.SqliteIntToBool(item.Enabled) {
				dot = activeDot
			}

			// mod header line: ● [1] Mod Name
			modLine := fmt.Sprintf("  %s [%d] %s", dot, item.Priority, item.ModPageName)
			if !util.SqliteIntToBool(item.Enabled) {
				modLine += "   " + disabledTagStyle.Render("(disabled)")
			}
			b.WriteString(modLine + "\n")

			// nested KV fields
			writeKVIndented16(&b, "file:", item.FileLabel)

			if item.VersionString.Valid {
				writeKVIndented16(&b, "version:", item.VersionString.String)
			} else {
				writeKVIndented16(&b, "version:", subtleStyle.Render("(none)"))
			}

			if item.NexusFileID.Valid {
				if info, ok := nexusInfo[item.ModFileVersionID]; ok {
					if info.HasUpdate {
						writeKVIndented16(&b, "nexus version:",
							nexusUpdateStyle.Render(fmt.Sprintf("%s ↑ update available", info.LatestVersion)))
					} else {
						writeKVIndented16(&b, "nexus version:",
							fmt.Sprintf("%s ✓ %s",
								info.CachedVersion,
								subtleStyle.Render(fmt.Sprintf("(last fetched %s)", formatAge(info.FetchedAt))),
							))
					}
				} else {
					writeKVIndented16(&b, "nexus version:",
						subtleStyle.Render("(run 'mods nexus check-updates' to fetch)"))
				}
			}

			writeKVIndented16(&b, "archive:", truncateSha(item.ArchiveSha256))
			writeKVIndented16(&b, "size:", formatBytes(item.SizeBytes))

			if item.ItemNotes.Valid && strings.TrimSpace(item.ItemNotes.String) != "" {
				writeKVIndented16(&b, "notes:", item.ItemNotes.String)
			}

			b.WriteString("\n")
		}
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

	for _, pair := range incompatibilities {
		warnings = append(warnings, fmt.Sprintf(
			"⚠  %s and %s are marked incompatible",
			pair.ModPageNameA, pair.ModPageNameB,
		))
		if pair.Reason.Valid && strings.TrimSpace(pair.Reason.String) != "" {
			warnings = append(warnings, fmt.Sprintf(
				"   reason: %s", pair.Reason.String,
			))
		}
	}

	if len(warnings) > 0 {
		b.WriteString(sectionTitleStyle.Render("Warnings") + "\n")
		for _, w := range warnings {
			b.WriteString(warningBanner.Render(warnStyle.Render(w)) + "\n")
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

// truncateSha returns the first 16 hex characters of a sha256 followed by "..."
func truncateSha(sha string) string {
	if len(sha) <= 16 {
		return sha
	}
	return sha[:16] + "..."
}

// formatBytes renders a byte count as a human-readable size string
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

func writeKV16(b *strings.Builder, label, value string) {
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")).
		Width(16)

	b.WriteString("  " + labelStyle.Render(label) + " " + value + "\n")
}

func writeKVIndented16(b *strings.Builder, label, value string) {
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")).
		Width(16)

	b.WriteString("      " + labelStyle.Render(label) + " " + value + "\n")
}
