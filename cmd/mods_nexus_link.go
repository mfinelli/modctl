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

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/nexus"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	modsNexusLinkVersionID   string
	modsNexusLinkNexusURL    string
	modsNexusLinkLabel       string
	modsNexusLinkFileVersion string
	modsNexusLinkFileName    string
	modsNexusLinkFileID      int64
	modsNexusLinkGame        string
	nexusLinkForce           bool
)

var modsNexusLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Link mod file versions to their Nexus mod page entries",
	Long: `Attempt to identify and link mod file versions to their corresponding
Nexus file IDs. Without --version-id, runs automatically against all unlinked
mod file versions for the active game. With --version-id, targets a specific
version for manual linking.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if modsNexusLinkGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			modsNexusLinkGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, modsNexusLinkGame)
		if err != nil {
			return err
		}

		if modsNexusLinkVersionID != "" {
			mfv, err := internal.ResolveModFileVersionArg(ctx, q, gi, modsNexusLinkVersionID)
			if err != nil {
				return err
			}
			return runManualLink(ctx, q, client, gi.ID, mfv.ID)
		}
		return runAutoLink(ctx, q, client, gi.ID)
	},
}

func init() {
	modsNexusCmd.AddCommand(modsNexusLinkCmd)

	modsNexusLinkCmd.Flags().StringVarP(&modsNexusLinkGame, "game", "g", "",
		"Override the currently active game")
	modsNexusLinkCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	modsNexusLinkCmd.Flags().StringVar(&modsNexusLinkVersionID, "version-id", "",
		"mod file version ID to link (manual mode)")
	modsNexusLinkCmd.RegisterFlagCompletionFunc("version-id",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ModFileVersionIDs(cmd, toComplete)
		})

	modsNexusLinkCmd.Flags().StringVar(&modsNexusLinkNexusURL, "nexus-url", "",
		"nexus mod page URL (manual mode)")
	modsNexusLinkCmd.Flags().StringVar(&modsNexusLinkLabel, "label", "",
		"nexus file display name to match against")
	modsNexusLinkCmd.Flags().StringVar(&modsNexusLinkFileVersion, "file-version", "",
		"nexus file version string to match against")
	modsNexusLinkCmd.Flags().StringVar(&modsNexusLinkFileName, "file-name", "",
		"exact nexus filename to match against")
	modsNexusLinkCmd.Flags().Int64Var(&modsNexusLinkFileID, "file-id", 0,
		"nexus file ID (bypasses identification)")

	modsNexusLinkCmd.Flags().BoolVar(&nexusLinkForce, "force", false,
		"proceed even if API quota may be exhausted")
}

func runManualLink(
	ctx context.Context,
	q *dbq.Queries,
	client *nexusclient.Client,
	gameInstallID int64,
	versionID int64,
) error {
	// TODO: extract styles
	subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	// Fetch current link state and verify game scope in one shot
	row, err := q.GetModFileVersionLinkState(ctx, dbq.GetModFileVersionLinkStateParams{
		ID:            versionID,
		GameInstallID: gameInstallID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"mod_file_version %d not found or does not belong to the active game install; use --game to specify a different game",
			versionID,
		)
	}
	if err != nil {
		return fmt.Errorf("fetching mod file version: %w", err)
	}

	// Resolve nexus domain and mod id - --nexus-url always wins
	gameDomain := row.NexusGameDomain.String
	modID := row.NexusModID.Int64

	if modsNexusLinkNexusURL != "" {
		ref, err := nexus.ParseModURL(modsNexusLinkNexusURL)
		if err != nil {
			return fmt.Errorf("parsing --nexus-url: %w", err)
		}
		gameDomain = ref.GameDomain
		modID = ref.ModID

		// Update mod page nexus info if it changed
		if gameDomain != row.NexusGameDomain.String || modID != row.NexusModID.Int64 {
			if err := q.UpdateModPageNexusInfo(ctx, dbq.UpdateModPageNexusInfoParams{
				ID:              row.ModPageID,
				NexusGameDomain: sql.NullString{String: gameDomain, Valid: true},
				NexusModID:      sql.NullInt64{Int64: modID, Valid: true},
			}); err != nil {
				return fmt.Errorf("updating mod page nexus info: %w", err)
			}
			fmt.Println(subtleStyle.Render(fmt.Sprintf("  updated mod page nexus info: %s/mods/%d", gameDomain, modID)))
		}
	} else if gameDomain == "" || modID == 0 {
		return fmt.Errorf(
			"mod page for mod_file_version %d has no Nexus info; provide --nexus-url to specify the Nexus mod page",
			versionID,
		)
	}

	// --file-id bypasses identification entirely
	if modsNexusLinkFileID != 0 {
		if row.NexusFileID.Int64 == modsNexusLinkFileID {
			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"  mod_file_version %d is already linked to nexus file_id %d (no changes made)",
				versionID, modsNexusLinkFileID,
			)))
			return nil
		}
		if err := q.UpdateModFileVersionNexusFileID(ctx, dbq.UpdateModFileVersionNexusFileIDParams{
			ID:          versionID,
			NexusFileID: sql.NullInt64{Int64: modsNexusLinkFileID, Valid: true},
		}); err != nil {
			return fmt.Errorf("updating nexus file id: %w", err)
		}
		fmt.Println(subtleStyle.Render(fmt.Sprintf(
			"  linked mod_file_version %d to nexus file_id %d",
			versionID, modsNexusLinkFileID,
		)))
		return nil
	}

	// Fetch fresh file list
	filesResp, err := client.GetModFiles(gameDomain, int(modID))
	if err != nil {
		return fmt.Errorf("fetching nexus file list: %w", err)
	}

	match, warnings, err := nexus.IdentifyNexusFile(
		modsNexusLinkFileName,
		row.ArchiveSize,
		modsNexusLinkLabel,
		modsNexusLinkFileVersion,
		filesResp.Files,
	)
	for _, w := range warnings {
		fmt.Println(warnStyle.Render(fmt.Sprintf("  ⚠ %s", w)))
	}
	if err != nil {
		return fmt.Errorf("identifying nexus file: %w", err)
	}
	if match == nil {
		return fmt.Errorf(
			"could not identify nexus file for mod_file_version %d; try providing more specific flags (--file-name, --label, --file-version, --file-id)",
			versionID,
		)
	}

	// No-op check
	if row.NexusFileID.Int64 == int64(match.File.FileID) && row.Label == match.File.Name {
		fmt.Println(subtleStyle.Render(fmt.Sprintf(
			"  mod_file_version %d is already correctly linked (no changes made)",
			versionID,
		)))
		return nil
	}

	fmt.Println(subtleStyle.Render(fmt.Sprintf(
		"  identified nexus file: %s v%s (file_id: %d, confidence: %s)",
		match.File.Name, match.File.Version, match.File.FileID, match.Confidence,
	)))

	if err := q.UpdateModFileVersionNexusFileID(ctx, dbq.UpdateModFileVersionNexusFileIDParams{
		ID:          versionID,
		NexusFileID: sql.NullInt64{Int64: int64(match.File.FileID), Valid: true},
	}); err != nil {
		return fmt.Errorf("updating nexus file id: %w", err)
	}

	if row.Label != match.File.Name {
		if err := q.UpdateModFileLabel(ctx, dbq.UpdateModFileLabelParams{
			ID:    row.ModFileID,
			Label: match.File.Name,
		}); err != nil {
			return fmt.Errorf("updating mod file label: %w", err)
		}
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  updated mod file label: %s", match.File.Name)))
	}

	return nil
}

func runAutoLink(
	ctx context.Context,
	q *dbq.Queries,
	client *nexusclient.Client,
	gameInstallID int64,
) error {
	// TODO: extract styles
	subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	// Warn about versions we'll skip
	skippable, err := q.GetSkippableModFileVersions(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("fetching skippable mod file versions: %w", err)
	}
	for _, s := range skippable {
		fmt.Println(warnStyle.Render(fmt.Sprintf(
			"  ⚠ skipping mod_file_version %d (%s / %s): no nexus mod page info; use `mods nexus link --version-id %d --nexus-url <url>` to resolve",
			s.VersionID, s.ModPageName, s.Label, s.VersionID,
		)))
	}

	candidates, err := q.GetUnlinkedNexusModFileVersions(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("fetching unlinked mod file versions: %w", err)
	}
	if len(candidates) == 0 {
		fmt.Println(subtleStyle.Render("  all mod file versions are already linked"))
		return nil
	}

	// Group candidates by mod page to avoid redundant API calls
	type modPageKey struct {
		domain string
		modID  int64
	}

	grouped := make(map[modPageKey][]dbq.GetUnlinkedNexusModFileVersionsRow)
	for _, c := range candidates {
		key := modPageKey{c.NexusGameDomain.String, c.NexusModID.Int64}
		grouped[key] = append(grouped[key], c)
	}

	// Check rate limits before batch operation
	needed := int64(len(grouped))
	state, err := client.RateLimitState()
	if err != nil {
		logger.Warn("failed to load rate limit state", "error", err)
	} else {
		hourly, daily := state.EffectiveRemaining()
		if needed > int64(hourly) || needed > int64(daily) {
			fmt.Printf("  ⚠ this operation requires %d API calls (hourly remaining: %d, daily remaining: %d)\n",
				needed, hourly, daily)
			if !nexusLinkForce {
				fmt.Println(warnStyle.Render("  operation aborted: not enough API quota remaining; pass --force to proceed anyway"))
				return nil
			}
			fmt.Println(warnStyle.Render("  proceeding anyway due to --force"))
		}
	}

	linked := 0
	failed := 0

	for key, versions := range grouped {
		filesResp, err := client.GetModFiles(key.domain, int(key.modID))
		if err != nil {
			fmt.Println(warnStyle.Render(fmt.Sprintf(
				"  ⚠ failed to fetch file list for %s/mods/%d: %s",
				key.domain, key.modID, err,
			)))
			failed += len(versions)
			continue
		}

		for _, v := range versions {
			match, warnings, err := nexus.IdentifyNexusFile(
				v.OriginalName.String,
				v.ArchiveSize,
				v.Label,
				"", // no file version hint in auto mode
				filesResp.Files,
			)
			for _, w := range warnings {
				fmt.Println(warnStyle.Render(fmt.Sprintf("  ⚠ %s", w)))
			}
			if err != nil {
				fmt.Println(warnStyle.Render(fmt.Sprintf(
					"  ⚠ error identifying mod_file_version %d (%s / %s): %s",
					v.VersionID, v.ModPageName, v.Label, err,
				)))
				failed++
				continue
			}
			if match == nil {
				fmt.Println(warnStyle.Render(fmt.Sprintf(
					"  ⚠ could not identify nexus file for mod_file_version %d (%s / %s); use `mods nexus link --version-id %d --nexus-url %s` to resolve manually",
					v.VersionID, v.ModPageName, v.Label, v.VersionID,
					fmt.Sprintf("https://www.nexusmods.com/%s/mods/%d", key.domain, key.modID),
				)))
				failed++
				continue
			}

			if err := q.UpdateModFileVersionNexusFileID(ctx, dbq.UpdateModFileVersionNexusFileIDParams{
				ID:          v.VersionID,
				NexusFileID: sql.NullInt64{Int64: int64(match.File.FileID), Valid: true},
			}); err != nil {
				fmt.Println(warnStyle.Render(fmt.Sprintf(
					"  ⚠ failed to update nexus file id for mod_file_version %d: %s",
					v.VersionID, err,
				)))
				failed++
				continue
			}

			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"  linked mod_file_version %d (%s / %s) to nexus file_id %d (confidence: %s)",
				v.VersionID, v.ModPageName, v.Label, match.File.FileID, match.Confidence,
			)))
			linked++
		}
	}

	fmt.Printf("\n  linked: %d  failed/skipped: %d\n", linked, failed+len(skippable))
	return nil
}
