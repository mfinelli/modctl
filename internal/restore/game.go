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

package restore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
)

// idRemap holds old -> new ID mappings built during game-scoped import
type idRemap struct {
	targets         map[int64]int64
	modPages        map[int64]int64
	modFiles        map[int64]int64
	modFileVersions map[int64]int64
	remapConfigs    map[int64]int64
	profiles        map[int64]int64
	overrides       map[int64]int64
}

// Game imports a game-scoped bundle into an existing database.
func Game(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	bs blobstore.Store,
	bundle *Bundle,
	opts Options,
	modctlVersion string,
	logger *slog.Logger,
) (Result, error) {
	var res Result

	// Resolve game identity: opts.Game takes precedence (set when extracting
	// a single game from a full bundle), otherwise use the bundle manifest.
	var storeID, storeGameID, displayName string
	if opts.Game != "" {
		parts := strings.SplitN(opts.Game, ":", 2)
		if len(parts) != 2 {
			return res, fmt.Errorf("invalid --game value %q: expected store_id:store_game_id", opts.Game)
		}
		storeID, storeGameID = parts[0], parts[1]
		// displayName will be resolved from the bundle DB below
	} else if bundle.Manifest.Game != nil {
		storeID = bundle.Manifest.Game.StoreID
		storeGameID = bundle.Manifest.Game.StoreGameID
		displayName = bundle.Manifest.Game.DisplayName
	} else {
		return res, fmt.Errorf("no game specified: pass --game or use a game-scoped bundle")
	}

	// Validate schema version
	currentSchema, err := currentSchemaVersion(ctx, db)
	if err != nil {
		return res, fmt.Errorf("get current schema version: %w", err)
	}
	if bundle.Manifest.SchemaVersion > currentSchema {
		return res, fmt.Errorf(
			"bundle schema version %d is newer than current schema version %d; run migrations first",
			bundle.Manifest.SchemaVersion, currentSchema,
		)
	}

	bq := dbq.New(bundle.BundleDB)

	// Find the matching game install in the bundle DB
	bundleInstalls, err := bq.ExportGetGameInstalls(ctx)
	if err != nil {
		return res, fmt.Errorf("read game installs from bundle: %w", err)
	}

	var matchedInstall *dbq.GameInstall
	for _, gi := range bundleInstalls {
		if gi.StoreID == storeID && gi.StoreGameID == storeGameID {
			gi := gi
			matchedInstall = &gi
			break
		}
	}
	if matchedInstall == nil {
		return res, fmt.Errorf("game %s:%s not found in bundle", storeID, storeGameID)
	}
	oldGameInstallID := matchedInstall.ID

	// Resolve display name from bundle if not set from manifest
	if displayName == "" {
		displayName = matchedInstall.DisplayName
	}

	// Check if game already exists
	existing, err := q.ImportGetGameInstallByStoreKey(ctx, dbq.ImportGetGameInstallByStoreKeyParams{
		StoreID:     bundle.Manifest.Game.StoreID,
		StoreGameID: bundle.Manifest.Game.StoreGameID,
		InstanceID:  "default",
	})
	gameExists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return res, fmt.Errorf("check existing game install: %w", err)
	}

	if gameExists && !opts.Force {
		return res, fmt.Errorf(
			"game %q (%s:%s) already exists; use --force to overwrite",
			displayName, storeID, storeGameID,
		)
	}

	if opts.DryRun {
		return dryRunGame(ctx, bundle, bq, oldGameInstallID)
	}

	if gameExists && opts.Force {
		if err := q.ImportDeleteGameInstall(ctx, existing); err != nil {
			return res, fmt.Errorf("delete existing game install: %w", err)
		}
	}

	// Import blobs (archives only; game-scoped bundles never contain backup blobs)
	archiveCount, _, overrideCount, err := importBlobs(ctx, bundle, bs)
	if err != nil {
		return res, fmt.Errorf("import blobs: %w", err)
	}
	res.Archives = archiveCount
	res.Overrides = overrideCount

	// ID remapping tables
	remap := &idRemap{
		targets:         make(map[int64]int64),
		modPages:        make(map[int64]int64),
		modFiles:        make(map[int64]int64),
		modFileVersions: make(map[int64]int64),
		remapConfigs:    make(map[int64]int64),
		profiles:        make(map[int64]int64),
		overrides:       make(map[int64]int64),
	}

	// Insert store (reuse existing if present)
	if err := importStore(ctx, q, bq, bundle.Manifest.Game.StoreID); err != nil {
		return res, fmt.Errorf("import store: %w", err)
	}

	// Insert game install, capture new ID
	newGameInstallID, err := importGameInstall(ctx, q, bq, matchedInstall)
	if err != nil {
		return res, fmt.Errorf("import game install: %w", err)
	}

	// Targets
	if err := importTargets(ctx, q, bq, newGameInstallID, oldGameInstallID, remap); err != nil {
		return res, fmt.Errorf("import targets: %w", err)
	}

	// Blobs are already recorded by importBlobs via IngestFile +
	// EnsureBlobRecorded, so no ID remapping needed for blobs.

	// Mod pages
	if err := importModPages(ctx, q, bq, newGameInstallID, oldGameInstallID, remap); err != nil {
		return res, fmt.Errorf("import mod pages: %w", err)
	}
	res.ModPages = len(remap.modPages)

	// Mod files
	if err := importModFiles(ctx, q, bq, oldGameInstallID, remap); err != nil {
		return res, fmt.Errorf("import mod files: %w", err)
	}

	// Mod file versions
	if err := importModFileVersions(ctx, q, bq, oldGameInstallID, remap); err != nil {
		return res, fmt.Errorf("import mod file versions: %w", err)
	}

	// Remap configs and rules (must come before profile items)
	if err := importRemapConfigs(ctx, q, bq, oldGameInstallID, remap); err != nil {
		return res, fmt.Errorf("import remap configs: %w", err)
	}

	// Profiles
	if err := importProfiles(ctx, q, bq, newGameInstallID, oldGameInstallID, remap); err != nil {
		return res, fmt.Errorf("import profiles: %w", err)
	}
	res.Profiles = len(remap.profiles)

	// Profile items
	if err := importProfileItems(ctx, q, bq, oldGameInstallID, remap); err != nil {
		return res, fmt.Errorf("import profile items: %w", err)
	}

	// Profile path policies
	if err := importProfilePathPolicies(ctx, q, bq, oldGameInstallID, remap); err != nil {
		return res, fmt.Errorf("import profile path policies: %w", err)
	}

	// Mod incompatibilities
	if err := importModIncompatibilities(ctx, q, bq, oldGameInstallID, remap); err != nil {
		return res, fmt.Errorf("import mod incompatibilities: %w", err)
	}

	// Overrides and patch entries
	if err := importOverrides(ctx, q, bq, oldGameInstallID, remap); err != nil {
		return res, fmt.Errorf("import overrides: %w", err)
	}

	// Inventory entries (no ID remapping, keyed by archive_sha256 + position)
	if err := importInventory(ctx, q, bq, oldGameInstallID); err != nil {
		return res, fmt.Errorf("import inventory: %w", err)
	}

	// Queue missing inventory scans
	scanned, failed, err := scanMissingInventories(ctx, db, q, bs, oldGameInstallID, bq, opts.SkipInventory, logger)
	if err != nil {
		return res, fmt.Errorf("scan inventories: %w", err)
	}
	res.InventoryScanned = scanned
	res.InventoryFailed = failed

	return res, nil
}

func dryRunGame(ctx context.Context, bundle *Bundle, bq *dbq.Queries, oldGameInstallID int64) (Result, error) {
	var res Result

	archiveBlobs, err := bq.ExportGetArchiveBlobsForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return res, err
	}
	modPages, err := bq.ExportGetModPagesForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return res, err
	}
	profiles, err := bq.GetProfilesForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return res, err
	}
	overrides, err := bq.ExportGetOverridesForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return res, err
	}

	res.Archives = len(archiveBlobs)
	res.ModPages = len(modPages)
	res.Profiles = len(profiles)
	res.Overrides = len(overrides)
	return res, nil
}

func importStore(ctx context.Context, dst, src *dbq.Queries, storeID string) error {
	// Reuse existing store row if present
	_, err := dst.ImportGetStoreByID(ctx, storeID)
	if err == nil {
		return nil // already exists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check store: %w", err)
	}

	row, err := src.GetStoreById(ctx, storeID)
	if err != nil {
		return fmt.Errorf("get store from bundle: %w", err)
	}
	return dst.ExportInsertStore(ctx, dbq.ExportInsertStoreParams(row))
}

func importGameInstall(ctx context.Context, dst, src *dbq.Queries, gi *dbq.GameInstall) (int64, error) {
	newID, err := dst.ImportInsertGameInstall(ctx, dbq.ImportInsertGameInstallParams{
		StoreID:         gi.StoreID,
		StoreGameID:     gi.StoreGameID,
		DisplayName:     gi.DisplayName,
		InstanceID:      gi.InstanceID,
		CanonicalGameID: gi.CanonicalGameID,
		InstallRoot:     gi.InstallRoot,
		Metadata:        gi.Metadata,
		LastSeenAt:      gi.LastSeenAt,
		IsPresent:       gi.IsPresent,
		CreatedAt:       gi.CreatedAt,
		UpdatedAt:       gi.UpdatedAt,
	})
	return newID, err
}

func importTargets(ctx context.Context, dst, src *dbq.Queries, newGameInstallID, oldGameInstallID int64, remap *idRemap) error {
	targets, err := src.ListTargetsForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get targets: %w", err)
	}
	for _, t := range targets {
		newID, err := dst.ImportInsertTarget(ctx, dbq.ImportInsertTargetParams{
			GameInstallID: newGameInstallID,
			Name:          t.Name,
			RootPath:      t.RootPath,
			Origin:        t.Origin,
			Metadata:      t.Metadata,
			CreatedAt:     t.CreatedAt,
			UpdatedAt:     t.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert target %q: %w", t.Name, err)
		}
		remap.targets[t.ID] = newID
	}
	return nil
}

func importModPages(ctx context.Context, dst, src *dbq.Queries, newGameInstallID, oldGameInstallID int64, remap *idRemap) error {
	pages, err := src.ExportGetModPagesForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get mod pages: %w", err)
	}
	for _, p := range pages {
		newID, err := dst.ImportInsertModPage(ctx, dbq.ImportInsertModPageParams{
			GameInstallID:   newGameInstallID,
			Name:            p.Name,
			SourceKind:      p.SourceKind,
			SourceUrl:       p.SourceUrl,
			SourceRef:       p.SourceRef,
			NexusGameDomain: p.NexusGameDomain,
			NexusModID:      p.NexusModID,
			Notes:           p.Notes,
			Metadata:        p.Metadata,
			CreatedAt:       p.CreatedAt,
			UpdatedAt:       p.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert mod page %q: %w", p.Name, err)
		}
		remap.modPages[p.ID] = newID
	}
	return nil
}

func importModFiles(ctx context.Context, dst, src *dbq.Queries, oldGameInstallID int64, remap *idRemap) error {
	files, err := src.ExportGetModFilesForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get mod files: %w", err)
	}
	for _, f := range files {
		newModPageID, ok := remap.modPages[f.ModPageID]
		if !ok {
			return fmt.Errorf("mod file %d references unknown mod page %d", f.ID, f.ModPageID)
		}
		newID, err := dst.ImportInsertModFile(ctx, dbq.ImportInsertModFileParams{
			ModPageID: newModPageID,
			Label:     f.Label,
			IsPrimary: f.IsPrimary,
			SourceUrl: f.SourceUrl,
			Metadata:  f.Metadata,
			CreatedAt: f.CreatedAt,
			UpdatedAt: f.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert mod file %q: %w", f.Label, err)
		}
		remap.modFiles[f.ID] = newID
	}
	return nil
}

func importModFileVersions(ctx context.Context, dst, src *dbq.Queries, oldGameInstallID int64, remap *idRemap) error {
	versions, err := src.ExportGetModFileVersionsForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get mod file versions: %w", err)
	}
	for _, v := range versions {
		newModFileID, ok := remap.modFiles[v.ModFileID]
		if !ok {
			return fmt.Errorf("mod file version %d references unknown mod file %d", v.ID, v.ModFileID)
		}
		newID, err := dst.ImportInsertModFileVersion(ctx, dbq.ImportInsertModFileVersionParams{
			ModFileID:          newModFileID,
			ArchiveSha256:      v.ArchiveSha256,
			OriginalName:       v.OriginalName,
			VersionString:      v.VersionString,
			NexusFileID:        v.NexusFileID,
			UploadedAt:         v.UploadedAt,
			InventoryScannedAt: v.InventoryScannedAt,
			UpstreamNotes:      v.UpstreamNotes,
			Notes:              v.Notes,
			Metadata:           v.Metadata,
			CreatedAt:          v.CreatedAt,
			UpdatedAt:          v.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert mod file version: %w", err)
		}
		remap.modFileVersions[v.ID] = newID
	}
	return nil
}

func importRemapConfigs(ctx context.Context, dst, src *dbq.Queries, oldGameInstallID int64, remap *idRemap) error {
	configs, err := src.ExportGetRemapConfigsForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get remap configs: %w", err)
	}
	for _, cfg := range configs {
		newID, err := dst.ImportInsertRemapConfig(ctx, dbq.ImportInsertRemapConfigParams{
			CreatedAt: cfg.CreatedAt,
			UpdatedAt: cfg.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert remap config %d: %w", cfg.ID, err)
		}
		remap.remapConfigs[cfg.ID] = newID

		rules, err := src.ExportGetRemapRulesForConfig(ctx, cfg.ID)
		if err != nil {
			return fmt.Errorf("get remap rules for config %d: %w", cfg.ID, err)
		}
		for _, rule := range rules {
			if err := dst.ImportInsertRemapRule(ctx, dbq.ImportInsertRemapRuleParams{
				RemapConfigID: newID,
				Position:      rule.Position,
				RuleType:      rule.RuleType,
				IntValue:      rule.IntValue,
				TextValue:     rule.TextValue,
				JsonValue:     rule.JsonValue,
				CreatedAt:     rule.CreatedAt,
				UpdatedAt:     rule.UpdatedAt,
			}); err != nil {
				return fmt.Errorf("insert remap rule: %w", err)
			}
		}
	}
	return nil
}

func importProfiles(ctx context.Context, dst, src *dbq.Queries, newGameInstallID, oldGameInstallID int64, remap *idRemap) error {
	profiles, err := src.GetProfilesForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get profiles: %w", err)
	}
	for _, p := range profiles {
		newID, err := dst.ImportInsertProfile(ctx, dbq.ImportInsertProfileParams{
			GameInstallID: newGameInstallID,
			Name:          p.Name,
			Description:   p.Description,
			IsActive:      p.IsActive,
			CreatedAt:     p.CreatedAt,
			UpdatedAt:     p.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert profile %q: %w", p.Name, err)
		}
		remap.profiles[p.ID] = newID
	}
	return nil
}

func importProfileItems(ctx context.Context, dst, src *dbq.Queries, oldGameInstallID int64, remap *idRemap) error {
	items, err := src.ExportGetProfileItemsForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get profile items: %w", err)
	}
	for _, item := range items {
		newProfileID, ok := remap.profiles[item.ProfileID]
		if !ok {
			return fmt.Errorf("profile item references unknown profile %d", item.ProfileID)
		}
		newMFVID, ok := remap.modFileVersions[item.ModFileVersionID]
		if !ok {
			return fmt.Errorf("profile item references unknown mod file version %d", item.ModFileVersionID)
		}
		var newRemapConfigID sql.NullInt64
		if item.RemapConfigID.Valid {
			newID, ok := remap.remapConfigs[item.RemapConfigID.Int64]
			if !ok {
				return fmt.Errorf("profile item references unknown remap config %d", item.RemapConfigID.Int64)
			}
			newRemapConfigID = sql.NullInt64{Int64: newID, Valid: true}
		}
		if _, err := dst.ImportInsertProfileItem(ctx, dbq.ImportInsertProfileItemParams{
			ProfileID:        newProfileID,
			Policy:           item.Policy,
			ModFileVersionID: newMFVID,
			Enabled:          item.Enabled,
			Priority:         item.Priority,
			RemapConfigID:    newRemapConfigID,
			TargetID:         item.TargetID,
			Notes:            item.Notes,
			CreatedAt:        item.CreatedAt,
			UpdatedAt:        item.UpdatedAt,
		}); err != nil {
			return fmt.Errorf("insert profile item: %w", err)
		}
	}
	return nil
}

func importProfilePathPolicies(ctx context.Context, dst, src *dbq.Queries, oldGameInstallID int64, remap *idRemap) error {
	policies, err := src.ExportGetProfilePathPoliciesForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get profile path policies: %w", err)
	}
	for _, p := range policies {
		newProfileID, ok := remap.profiles[p.ProfileID]
		if !ok {
			return fmt.Errorf("profile path policy references unknown profile %d", p.ProfileID)
		}
		if _, err := dst.ImportInsertProfilePathPolicy(ctx, dbq.ImportInsertProfilePathPolicyParams{
			ProfileID:   newProfileID,
			TargetName:  p.TargetName,
			PathPattern: p.PathPattern,
			Policy:      p.Policy,
			Metadata:    p.Metadata,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		}); err != nil {
			return fmt.Errorf("insert profile path policy: %w", err)
		}
	}
	return nil
}

func importModIncompatibilities(ctx context.Context, dst, src *dbq.Queries, oldGameInstallID int64, remap *idRemap) error {
	incompatibilities, err := src.ExportGetModIncompatibilitiesForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get mod incompatibilities: %w", err)
	}
	for _, inc := range incompatibilities {
		newIDA, ok := remap.modPages[inc.ModPageIDA]
		if !ok {
			return fmt.Errorf("incompatibility references unknown mod page %d", inc.ModPageIDA)
		}
		newIDB, ok := remap.modPages[inc.ModPageIDB]
		if !ok {
			return fmt.Errorf("incompatibility references unknown mod page %d", inc.ModPageIDB)
		}
		if err := dst.ImportInsertModIncompatibility(ctx, dbq.ImportInsertModIncompatibilityParams{
			ModPageIDA: newIDA,
			ModPageIDB: newIDB,
			Reason:     inc.Reason,
			Source:     inc.Source,
			CreatedAt:  inc.CreatedAt,
			UpdatedAt:  inc.UpdatedAt,
		}); err != nil {
			return fmt.Errorf("insert mod incompatibility: %w", err)
		}
	}
	return nil
}

func importInventory(ctx context.Context, dst, src *dbq.Queries, oldGameInstallID int64) error {
	// inventory entries are keyed by (archive_sha256, position) so no ID
	// remapping needed - just skip entries that already exist
	entries, err := src.ExportGetInventoryForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get inventory entries: %w", err)
	}
	for _, e := range entries {
		err := dst.ImportInsertInventoryEntry(ctx, dbq.ImportInsertInventoryEntryParams{
			ArchiveSha256: e.ArchiveSha256,
			RawPath:       e.RawPath,
			EntryType:     e.EntryType,
			SizeBytes:     e.SizeBytes,
			LinkTarget:    e.LinkTarget,
			ContentSha256: e.ContentSha256,
			Position:      e.Position,
			ParseError:    e.ParseError,
			CreatedAt:     e.CreatedAt,
		})
		if err != nil {
			// skip duplicate entries (same archive already imported)
			if isSQLiteUniqueConstraint(err) {
				continue
			}
			return fmt.Errorf("insert inventory entry: %w", err)
		}
	}
	return nil
}

func importOverrides(ctx context.Context, dst, src *dbq.Queries, oldGameInstallID int64, remap *idRemap) error {
	overrides, err := src.ExportGetOverridesForGameInstall(ctx, oldGameInstallID)
	if err != nil {
		return fmt.Errorf("get overrides: %w", err)
	}
	for _, o := range overrides {
		newProfileID, ok := remap.profiles[o.ProfileID]
		if !ok {
			return fmt.Errorf("override references unknown profile %d", o.ProfileID)
		}
		newTargetID, ok := remap.targets[o.TargetID]
		if !ok {
			return fmt.Errorf("override references unknown target %d", o.TargetID)
		}
		newID, err := dst.ImportInsertOverride(ctx, dbq.ImportInsertOverrideParams{
			ProfileID:           newProfileID,
			TargetID:            newTargetID,
			Relpath:             o.Relpath,
			BlobSha256:          o.BlobSha256,
			OverrideType:        o.OverrideType,
			SourceArchiveSha256: o.SourceArchiveSha256,
			SourceRawPath:       o.SourceRawPath,
			SourceContentSha256: o.SourceContentSha256,
			Notes:               o.Notes,
			CreatedAt:           o.CreatedAt,
			UpdatedAt:           o.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert override for %q: %w", o.Relpath, err)
		}
		remap.overrides[o.ID] = newID

		// import patch entries for this override
		entries, err := src.ExportGetPatchEntriesForOverride(ctx, o.ID)
		if err != nil {
			return fmt.Errorf("get patch entries for override %d: %w", o.ID, err)
		}
		for _, e := range entries {
			if err := dst.ImportInsertPatchEntry(ctx, dbq.ImportInsertPatchEntryParams{
				OverrideID:   newID,
				Position:     e.Position,
				PatchType:    e.PatchType,
				EntrySection: e.EntrySection,
				EntryKey:     e.EntryKey,
				EntryValue:   e.EntryValue,
			}); err != nil {
				return fmt.Errorf("insert patch entry: %w", err)
			}
		}
	}
	return nil
}
