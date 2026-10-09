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
	modsInfoGame          string
	modsInfoShowInventory bool
)

var modsInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show detailed information about a mod page",
	Long: `Show detailed information about a mod page including all files, versions,
Nexus link state, cached update info, and profile membership.

Nexus data is read from the local cache only. Run 'mods nexus check-updates'
to refresh cached data.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.ModPageIDs(cmd, toComplete)
	},
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

		q := dbq.New(db)

		// Resolve game install id: --game overrides active selection
		if modsInfoGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			modsInfoGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, modsInfoGame)
		if err != nil {
			return err
		}

		mp, err := internal.ResolveModPageArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		return runModsInfo(ctx, q, mp.ID, gi.ID, modsInfoShowInventory)
	},
}

func init() {
	modsCmd.AddCommand(modsInfoCmd)

	modsInfoCmd.Flags().StringVarP(&modsInfoGame, "game", "g", "",
		"Override the currently active game")
	modsInfoCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	modsInfoCmd.Flags().BoolVar(&modsInfoShowInventory, "show-inventory", false,
		"Show archive inventory entries for each version")
}

type nexusFileCache struct {
	Version               string
	FetchedAt             time.Time
	HasUpdate             bool
	LatestVersion         string
	UpdateAlreadyImported bool // true when superseded but head is imported
}

type profileMembership struct {
	ProfileName string
	Enabled     bool
	Priority    int64
}

type versionInventory struct {
	Entries     []dbq.GetInventoryEntriesForArchiveRow
	ParseErrors []dbq.GetInventoryParseErrorsForArchiveRow
	Scanned     bool
}

func runModsInfo(
	ctx context.Context,
	q *dbq.Queries,
	modPageID int64,
	gameInstallID int64,
	showInventory bool,
) error {
	// fetch mod page
	mp, err := q.GetModPageByID(ctx, dbq.GetModPageByIDParams{
		ID:            modPageID,
		GameInstallID: gameInstallID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("mod page %d not found or does not belong to the active game install", modPageID)
	}
	if err != nil {
		return fmt.Errorf("fetching mod page: %w", err)
	}

	// fetch files and versions
	fileVersions, err := q.GetModFilesWithVersions(ctx, modPageID)
	if err != nil {
		return fmt.Errorf("fetching mod files: %w", err)
	}

	// fetch profile membership per version
	versionProfiles := make(map[int64][]profileMembership)
	for _, fv := range fileVersions {
		rows, err := q.GetModFileVersionProfiles(ctx, dbq.GetModFileVersionProfilesParams{
			ModFileVersionID: fv.ModFileVersionID,
			GameInstallID:    gameInstallID,
		})
		if err != nil {
			return fmt.Errorf("fetching profile membership for version %d: %w", fv.ModFileVersionID, err)
		}
		var memberships []profileMembership
		for _, r := range rows {
			memberships = append(memberships, profileMembership{
				ProfileName: r.ProfileName,
				Enabled:     util.SqliteIntToBool(r.Enabled),
				Priority:    r.Priority,
			})
		}
		versionProfiles[fv.ModFileVersionID] = memberships
	}

	// fetch nexus cache data if applicable
	var nexusModInfo *dbc.NexusModInfo
	nexusFileInfos := make(map[int64]*nexusFileCache)
	// next map and superseded set are built here and passed to renderModInfo
	next := make(map[int64]int64)
	superseded := make(map[int64]struct{})

	// set of all nexus_file_ids we have imported for this mod page
	importedNexusFileIDs := make(map[int64]struct{})
	for _, fv := range fileVersions {
		if fv.NexusFileID.Valid {
			importedNexusFileIDs[fv.NexusFileID.Int64] = struct{}{}
		}
	}
	if mp.SourceKind == "nexus" && mp.NexusGameDomain.Valid && mp.NexusModID.Valid {
		cacheReader, err := nexusclient.NewCacheReader(ctx, logger)
		if err != nil {
			logger.Warn("failed to open nexus cache", "error", err)
		} else {
			defer cacheReader.Close()

			// fetch mod level cache
			modInfo, err := cacheReader.GetNexusModInfo(mp.NexusGameDomain.String, mp.NexusModID.Int64)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				logger.Warn("failed to fetch nexus mod info from cache", "error", err)
			} else if err == nil {
				nexusModInfo = modInfo
			}

			// fetch update chain once
			chain, err := cacheReader.GetNexusFileUpdateChain(
				mp.NexusGameDomain.String,
				mp.NexusModID.Int64,
			)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				logger.Warn("failed to fetch nexus file update chain", "error", err)
			}

			// build next and superseded from the chain
			for _, row := range chain {
				next[row.OldFileID] = row.NewFileID
				superseded[row.OldFileID] = struct{}{}
			}

			// fetch per-file cache info
			for _, fv := range fileVersions {
				if !fv.NexusFileID.Valid {
					continue
				}
				row, err := cacheReader.GetNexusFileInfo(
					mp.NexusGameDomain.String,
					mp.NexusModID.Int64,
					fv.NexusFileID.Int64,
				)
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if err != nil {
					logger.Warn("failed to fetch nexus file info from cache",
						"mod_file_version_id", fv.ModFileVersionID,
						"error", err,
					)
					continue
				}

				fetchedAt, err := time.Parse(time.RFC3339, row.FetchedAt)
				if err != nil {
					continue
				}

				// only walk the chain for non-superseded versions
				_, isSuperseded := superseded[fv.NexusFileID.Int64]
				info := &nexusFileCache{
					Version:   row.Version.String,
					FetchedAt: fetchedAt,
				}

				if !isSuperseded {
					latestFileID := internal.WalkUpdateChain(fv.NexusFileID.Int64, next)
					info.HasUpdate = latestFileID != fv.NexusFileID.Int64
					if info.HasUpdate {
						latestRow, err := cacheReader.GetNexusFileInfo(
							mp.NexusGameDomain.String,
							mp.NexusModID.Int64,
							latestFileID,
						)
						if err == nil && latestRow.Version.Valid {
							info.LatestVersion = latestRow.Version.String
						}
					}
				} else {
					// superseded: check if the head is already imported
					latestFileID := internal.WalkUpdateChain(fv.NexusFileID.Int64, next)
					_, headImported := importedNexusFileIDs[latestFileID]
					info.UpdateAlreadyImported = headImported
					if !headImported {
						// need to surface the latest version string for the update prompt
						latestRow, err := cacheReader.GetNexusFileInfo(
							mp.NexusGameDomain.String,
							mp.NexusModID.Int64,
							latestFileID,
						)
						if err == nil && latestRow.Version.Valid {
							info.LatestVersion = latestRow.Version.String
						}
					}
				}

				nexusFileInfos[fv.ModFileVersionID] = info
			}
		}
	}

	inventories := make(map[int64]versionInventory) // keyed by mod_file_version_id

	if showInventory {
		for _, fv := range fileVersions {
			entries, err := q.GetInventoryEntriesForArchive(ctx, fv.ArchiveSha256)
			if err != nil {
				return fmt.Errorf("fetching inventory for version %d: %w", fv.ModFileVersionID, err)
			}
			parseErrors, err := q.GetInventoryParseErrorsForArchive(ctx, fv.ArchiveSha256)
			if err != nil {
				return fmt.Errorf("fetching inventory parse errors for version %d: %w", fv.ModFileVersionID, err)
			}
			inventories[fv.ModFileVersionID] = versionInventory{
				Entries:     entries,
				ParseErrors: parseErrors,
				Scanned:     len(entries) > 0 || len(parseErrors) > 0,
			}
		}
	}

	style.Println(renderModInfo(mp, fileVersions, versionProfiles, nexusModInfo, nexusFileInfos, superseded, inventories, showInventory))
	return nil
}

func renderModInfo(
	mp dbq.GetModPageByIDRow,
	fileVersions []dbq.GetModFilesWithVersionsRow,
	versionProfiles map[int64][]profileMembership,
	nexusModInfo *dbc.NexusModInfo,
	nexusFileInfos map[int64]*nexusFileCache,
	superseded map[int64]struct{},
	inventories map[int64]versionInventory,
	showInventory bool,
) string {
	kv := style.KV{Indent: 2, Width: 16}
	kvIndented := style.KV{Indent: 6, Width: 16}

	var b strings.Builder

	// header card
	headerContent := style.Title.Render(mp.Name)
	headerContent += "\n" + style.Subtle.Render(fmt.Sprintf("mod_page_id: %d", mp.ID))
	headerContent += "\n" + style.Subtle.Render(fmt.Sprintf("source: %s", mp.SourceKind))
	if mp.SourceUrl.Valid {
		headerContent += "\n" + style.Subtle.Render(fmt.Sprintf("url: %s", mp.SourceUrl.String))
	}
	if mp.SourceRef.Valid {
		headerContent += "\n" + style.Subtle.Render(fmt.Sprintf("ref: %s", mp.SourceRef.String))
	}
	if mp.Notes.Valid && strings.TrimSpace(mp.Notes.String) != "" {
		headerContent += "\n" + style.Subtle.Render(internal.FormatNotesLines(mp.Notes.String))
	}
	headerContent += "\n" + style.Subtle.Render(fmt.Sprintf(
		"added: %s", style.Age(mustParseTime(mp.CreatedAt)),
	))
	b.WriteString(style.Card.Render(headerContent))
	b.WriteString("\n")

	// nexus section
	if mp.SourceKind == "nexus" && mp.NexusGameDomain.Valid && mp.NexusModID.Valid {
		b.WriteString(style.Section.Render("Nexus") + "\n")
		kv.Write(&b, "mod page:", fmt.Sprintf(
			"https://www.nexusmods.com/%s/mods/%d",
			mp.NexusGameDomain.String, mp.NexusModID.Int64,
		))
		if nexusModInfo != nil {
			if nexusModInfo.Author.Valid {
				kv.Write(&b, "author:", nexusModInfo.Author.String)
			}
			if nexusModInfo.Summary.Valid {
				kv.Write(&b, "summary:", nexusModInfo.Summary.String)
			}
			fetchedAt, err := time.Parse(time.RFC3339, nexusModInfo.FetchedAt)
			if err == nil {
				kv.Write(&b, "info fetched:", style.Subtle.Render(style.Age(fetchedAt)))
			}
			if !nexusModInfo.IsAvailable.Valid || nexusModInfo.IsAvailable.Int64 == 0 {
				kv.Write(&b, "status:", style.Warning.Render("⚠ mod unavailable on Nexus"))
			}
		} else {
			kv.Write(&b, "cached info:", style.Subtle.Render("(run 'mods nexus check-updates' to fetch)"))
		}
	}

	// files section - group by mod_file
	b.WriteString(style.Section.Render("Files") + "\n")

	if len(fileVersions) == 0 {
		b.WriteString(style.Subtle.Render("  (none)") + "\n")
	} else {
		// group rows by mod_file_id
		type modFileGroup struct {
			fileID    int64
			label     string
			isPrimary bool
			versions  []dbq.GetModFilesWithVersionsRow
		}
		var groups []modFileGroup
		groupIndex := make(map[int64]int)

		for _, fv := range fileVersions {
			if _, ok := groupIndex[fv.ModFileID]; !ok {
				groupIndex[fv.ModFileID] = len(groups)
				groups = append(groups, modFileGroup{
					fileID:    fv.ModFileID,
					label:     fv.FileLabel,
					isPrimary: util.SqliteIntToBool(fv.IsPrimary),
				})
			}
			idx := groupIndex[fv.ModFileID]
			groups[idx].versions = append(groups[idx].versions, fv)
		}

		for _, g := range groups {
			// file header line
			fileHeader := fmt.Sprintf("  %s", g.label)
			if g.isPrimary {
				fileHeader += "  " + style.PrimaryTag.Render("(primary)")
			}
			kv.Write(&b, fmt.Sprintf("  [file %d]", g.fileID), fileHeader)

			for _, v := range g.versions {
				b.WriteString(fmt.Sprintf("    version %d", v.ModFileVersionID) + "\n")

				if v.VersionString.Valid {
					kvIndented.Write(&b, "  version:", v.VersionString.String)
				} else {
					kvIndented.Write(&b, "  version:", style.Subtle.Render("(none)"))
				}

				kvIndented.Write(&b, "  archive:", style.ShortSha(v.ArchiveSha256))
				kvIndented.Write(&b, "  size:", style.Bytes(v.SizeBytes))

				if v.OriginalName.Valid {
					kvIndented.Write(&b, "  filename:", style.Subtle.Render(v.OriginalName.String))
				}

				// nexus file link state
				if v.NexusFileID.Valid {
					if info, ok := nexusFileInfos[v.ModFileVersionID]; ok {
						_, isSuperseded := superseded[v.NexusFileID.Int64]
						if isSuperseded && info.UpdateAlreadyImported {
							kvIndented.Write(&b, "  nexus version:",
								style.Subtle.Render(fmt.Sprintf("%s (superseded)", info.Version)))
						} else if isSuperseded || info.HasUpdate {
							kvIndented.Write(&b, "  nexus version:",
								style.UpdateAvailable.Render(fmt.Sprintf("%s ↑ update available → %s",
									info.Version, info.LatestVersion)))
						} else {
							kvIndented.Write(&b, "  nexus version:",
								fmt.Sprintf("%s ✓ %s",
									info.Version,
									style.Subtle.Render(fmt.Sprintf("(last fetched %s)", style.Age(info.FetchedAt))),
								))
						}
					} else {
						kvIndented.Write(&b, "  nexus version:",
							style.Subtle.Render("(run 'mods nexus check-updates' to fetch)"))
					}
				} else if mp.SourceKind == "nexus" {
					kvIndented.Write(&b, "  nexus link:",
						style.Warning.Render("⚠ unlinked (run 'mods nexus link' to resolve)"))
				}

				// profile membership
				memberships := versionProfiles[v.ModFileVersionID]
				if len(memberships) == 0 {
					kvIndented.Write(&b, "  profiles:", style.Subtle.Render("(not in any profile)"))
				} else {
					for _, m := range memberships {
						tag := style.Active.Render("enabled")
						if !m.Enabled {
							tag = style.Inactive.Render("disabled")
						}
						kvIndented.Write(&b, "  profiles:",
							fmt.Sprintf("%s [priority %d] %s",
								m.ProfileName, m.Priority, tag,
							))
					}
				}

				if showInventory {
					inv := inventories[v.ModFileVersionID]
					if !inv.Scanned {
						kvIndented.Write(&b, "  inventory:", style.Subtle.Render("(not scanned; run 'mods scan-inventory')"))
					} else {
						kvIndented.Write(&b, "  inventory:", fmt.Sprintf("%d file(s)", len(inv.Entries)))
						for _, e := range inv.Entries {
							path := ""
							if e.RawPath.Valid {
								path = e.RawPath.String
							}
							size := ""
							if e.SizeBytes.Valid {
								size = style.Subtle.Render(fmt.Sprintf("  %s", style.Bytes(e.SizeBytes.Int64)))
							}
							b.WriteString(fmt.Sprintf("      %s%s\n", path, size))
						}
						if len(inv.ParseErrors) > 0 {
							b.WriteString(style.Warning.Render(fmt.Sprintf(
								"      ⚠ %d parse error(s):\n", len(inv.ParseErrors),
							)))
							for _, pe := range inv.ParseErrors {
								b.WriteString(style.Warning.Render(fmt.Sprintf(
									"        position %d: %s\n", pe.Position, pe.ParseError.String,
								)))
							}
						}
					}
				}

				b.WriteString("\n")
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// mustParseTime parses a timestamp string from the DB
func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// created_at is always set by the DB with a fixed format so this
		// should never happen in practice
		return time.Time{}
	}
	return t
}
