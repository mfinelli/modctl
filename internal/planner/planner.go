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

// Package planner computes the desired file state for a profile apply or
// unapply. It reads the filesystem to check file existence and ownership
// but has no write side effects.
package planner

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/remap"
)

// PlanOpKind describes what a single file operation will do.
type PlanOpKind string

const (
	PlanOpWrite         PlanOpKind = "write"
	PlanOpOverwrite     PlanOpKind = "overwrite"
	PlanOpRemove        PlanOpKind = "remove"
	PlanOpRestoreBackup PlanOpKind = "restore_backup"
	PlanOpNoop          PlanOpKind = "noop"
)

// RemappedEntry is a single archive entry after remap rules have been applied.
type RemappedEntry struct {
	ArchiveSha256 string
	Position      int64
	SourcePath    string
	DestPath      string
	SizeBytes     int64
}

// Conflict records a single mod's claim on a destination path.
type Conflict struct {
	ModFileVersionID int64
	ProfileItemID    int64
	Entry            RemappedEntry
	Priority         int64
	Won              bool
	// Display fields for dry-run output and status reporting.
	ModPageName   string
	FileLabel     string
	VersionString string // empty if not set
}

// PlanFile is the resolved state for a single destination path. The winner
// is the Conflict where Won == true. All other entries in Conflicts lost.
type PlanFile struct {
	DestPath      string
	ProfileItemID int64
	Conflicts     []Conflict // sorted priority desc; exactly one has Won=true
}

// Winner returns the winning Conflict for this path.
func (pf *PlanFile) Winner() Conflict {
	for _, c := range pf.Conflicts {
		if c.Won {
			return c
		}
	}
	panic("PlanFile has no winner")
}

// PlanOp is a single file operation the apply step will execute.
type PlanOp struct {
	Kind     PlanOpKind
	DestPath string
	// File is set for Write, Overwrite ops.
	File *PlanFile
	// BackupSha256 is set for RestoreBackup ops.
	BackupSha256 string
	// NeedsBackup is true when a pre-existing non-tool-owned file must be
	// backed up before being overwritten.
	NeedsBackup bool

	// OverrideID is set when this op's content comes from an override.
	OverrideID sql.NullInt64
	// OverrideType is set when OverrideID is valid.
	// One of: "full_file", "ini_patch", "yaml_patch", "json_patch"
	OverrideType string
	// OverrideBlobSha256 is set for full_file override ops.
	OverrideBlobSha256 sql.NullString
	// OverrideBaseArchiveSha256 is set for patch override ops: the archive
	// that must be staged to provide the base file.
	OverrideBaseArchiveSha256 sql.NullString
	// OverrideBaseRawPath is the path inside the base archive for patch ops.
	OverrideBaseRawPath sql.NullString
}

// Plan is the full computed desired state for a profile apply or unapply.
// It has no disk side effects and can be used directly for dry-run output.
type Plan struct {
	GameInstallID int64
	ProfileID     int64 // 0 for unapply plans
	TargetID      int64
	TargetRoot    string
	Files         []PlanFile // one per destination path, apply plans only
	Ops           []PlanOp
	Warnings      []string

	// PatchBaseArchives is the set of archive sha256s that must be staged
	// for patch override base files, even if no mod write op requires them.
	PatchBaseArchives []string
}

// UninventoriedArchiveError is returned when a profile item references an
// archive that has not yet been scanned.
type UninventoriedArchiveError struct {
	ModFileVersionID int64
	ModPageName      string
	FileLabel        string
	ArchiveSha256    string
}

func (e *UninventoriedArchiveError) Error() string {
	return fmt.Sprintf(
		"archive for %q / %q (mod_file_version_id %d, archive %.16s...) has not been inventoried; run 'mods scan-inventory'",
		e.ModPageName, e.FileLabel, e.ModFileVersionID, e.ArchiveSha256,
	)
}

// BuildApplyPlan computes the desired file state for applying profileID to
// gameInstallID. It reads the filesystem to check file existence and
// ownership but does not modify anything.
func BuildApplyPlan(ctx context.Context, q *dbq.Queries, gameInstallID, profileID int64, skipRecheck bool) (Plan, error) {
	target, err := q.GetTargetByName(ctx, dbq.GetTargetByNameParams{
		GameInstallID: gameInstallID,
		Name:          "game_dir",
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return Plan{}, fmt.Errorf("no game_dir target found for game install %d", gameInstallID)
		}
		return Plan{}, fmt.Errorf("resolve target: %w", err)
	}

	plan := Plan{
		GameInstallID: gameInstallID,
		ProfileID:     profileID,
		TargetID:      target.ID,
		TargetRoot:    target.RootPath,
	}

	// Load enabled profile items sorted by priority desc.
	items, err := q.GetProfileItemForPlanning(ctx, profileID)
	if err != nil {
		return Plan{}, fmt.Errorf("load profile items: %w", err)
	}

	// winner map: destPath -> index into plan.Files
	winners := make(map[string]int)

	for _, item := range items {
		if !item.InventoryScannedAt.Valid {
			label, labelErr := q.GetModFileVersionLabel(ctx, item.ModFileVersionID)
			if labelErr != nil {
				return Plan{}, &UninventoriedArchiveError{
					ModFileVersionID: item.ModFileVersionID,
					ArchiveSha256:    item.ArchiveSha256,
				}
			}
			return Plan{}, &UninventoriedArchiveError{
				ModFileVersionID: item.ModFileVersionID,
				ModPageName:      label.ModPageName,
				FileLabel:        label.FileLabel,
				ArchiveSha256:    item.ArchiveSha256,
			}
		}

		var rules []dbq.RemapRule
		if item.RemapConfigID.Valid {
			rules, err = q.GetRemapRulesForConfig(ctx, item.RemapConfigID.Int64)
			if err != nil {
				return Plan{}, fmt.Errorf("load remap rules for item %d: %w", item.ItemID, err)
			}
		}

		entries, err := q.GetInventoryEntriesForArchive(ctx, item.ArchiveSha256)
		if err != nil {
			return Plan{}, fmt.Errorf("load inventory for archive %s: %w", item.ArchiveSha256, err)
		}

		for _, entry := range entries {
			if !entry.RawPath.Valid {
				continue
			}
			rawPath := entry.RawPath.String

			result, err := remap.Apply(rules, rawPath)
			if err != nil {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("remap error for %q in archive %.16s: %v", rawPath, item.ArchiveSha256, err))
				continue
			}
			if result.Skip {
				continue
			}

			destPath := result.Path

			if err := validateDestPath(destPath); err != nil {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("rejected %q -> %q: %v", rawPath, destPath, err))
				continue
			}

			re := RemappedEntry{
				ArchiveSha256: item.ArchiveSha256,
				Position:      entry.Position,
				SourcePath:    rawPath,
				DestPath:      destPath,
				SizeBytes:     entry.SizeBytes.Int64,
			}

			conflict := Conflict{
				ModFileVersionID: item.ModFileVersionID,
				ProfileItemID:    item.ItemID,
				Entry:            re,
				Priority:         item.Priority,
				ModPageName:      item.ModPageName,
				FileLabel:        item.FileLabel,
				VersionString:    item.VersionString.String,
			}

			if idx, exists := winners[destPath]; exists {
				plan.Files[idx].Conflicts = append(plan.Files[idx].Conflicts, conflict)
			} else {
				conflict.Won = true
				pf := PlanFile{
					DestPath:      destPath,
					ProfileItemID: item.ItemID,
					Conflicts:     []Conflict{conflict},
				}
				winners[destPath] = len(plan.Files)
				plan.Files = append(plan.Files, pf)
			}
		}
	}

	// Load overrides for this profile and overlay them on top of the mod
	// winner set.
	overrides, err := q.ListOverridesForApply(ctx, profileID)
	if err != nil {
		return Plan{}, fmt.Errorf("load overrides: %w", err)
	}

	// Track which archives are already going to be staged from mod ops so
	// we can avoid adding duplicates to PatchBaseArchives.
	stagedArchives := make(map[string]bool)
	for _, pf := range plan.Files {
		stagedArchives[pf.Winner().Entry.ArchiveSha256] = true
	}

	// overrideByPath holds the override row keyed by relpath for use when
	// building ops below.
	type overrideInfo struct {
		id           int64
		overrideType string
		blobSha256   sql.NullString
		baseArchive  sql.NullString
		baseRawPath  sql.NullString
	}
	overrideMap := make(map[string]overrideInfo, len(overrides))

	for _, o := range overrides {
		oi := overrideInfo{
			id:           o.ID,
			overrideType: o.OverrideType,
			blobSha256:   o.BlobSha256,
			baseArchive:  o.SourceArchiveSha256,
			baseRawPath:  o.SourceRawPath,
		}
		overrideMap[o.Relpath] = oi

		if _, exists := winners[o.Relpath]; !exists {
			// net-new override path (synthesize a PlanFile with no conflicts)
			pf := PlanFile{
				DestPath:  o.Relpath,
				Conflicts: []Conflict{},
			}
			winners[o.Relpath] = len(plan.Files)
			plan.Files = append(plan.Files, pf)
		}

		// For patch overrides, ensure the base archive will be staged.
		if o.OverrideType != "full_file" && o.SourceArchiveSha256.Valid {
			if !stagedArchives[o.SourceArchiveSha256.String] {
				plan.PatchBaseArchives = append(plan.PatchBaseArchives, o.SourceArchiveSha256.String)
				stagedArchives[o.SourceArchiveSha256.String] = true
			}
		}
	}

	// Load currently installed files for reconciliation
	installedFiles, err := q.GetInstalledFilesForTarget(ctx, dbq.GetInstalledFilesForTargetParams{
		GameInstallID: gameInstallID,
		TargetID:      target.ID,
	})
	if err != nil {
		return Plan{}, fmt.Errorf("load installed files: %w", err)
	}

	installed := make(map[string]dbq.InstalledFile, len(installedFiles))
	for _, f := range installedFiles {
		installed[f.Relpath] = f
	}

	// Build op for each winner path.
	for i := range plan.Files {
		pf := &plan.Files[i]
		absPath := filepath.Join(target.RootPath, pf.DestPath)

		oi, hasOverride := overrideMap[pf.DestPath]

		op := PlanOp{
			DestPath: pf.DestPath,
		}

		// Only set File for non-net-new paths (paths with a mod winner)
		if len(pf.Conflicts) > 0 {
			op.File = pf
		}

		if hasOverride {
			op.OverrideID = sql.NullInt64{Int64: oi.id, Valid: true}
			op.OverrideType = oi.overrideType
			op.OverrideBlobSha256 = oi.blobSha256
			op.OverrideBaseArchiveSha256 = oi.baseArchive
			op.OverrideBaseRawPath = oi.baseRawPath
		}

		existingInstall, isInstalled := installed[pf.DestPath]
		_, existsOnDisk := diskStat(absPath)

		switch {
		case isInstalled && existsOnDisk:
			if hasOverride && op.OverrideType != "full_file" {
				// Patch overrides are never noop'd
				op.Kind = PlanOpOverwrite
			} else if hasOverride && !skipRecheck {
				// Full-file override noop check
				onDiskHash, err := hashFile(absPath)
				if err != nil {
					plan.Warnings = append(plan.Warnings,
						fmt.Sprintf("recheck: could not hash %q: %v", pf.DestPath, err))
					// Fall through to plain overwrite if we can't hash
					op.Kind = PlanOpOverwrite
				} else if onDiskHash == existingInstall.ContentSha256 &&
					// File is already correct - noop.
					existingInstall.OwnerOverrideID.Int64 == oi.id {
					op.Kind = PlanOpNoop
				} else {
					op.Kind = PlanOpOverwrite
				}
			} else if hasOverride && skipRecheck {
				// --no-recheck with override: always reapply
				op.Kind = PlanOpOverwrite
			} else if !skipRecheck {
				// Normal mod-owned file recheck
				onDiskHash, err := hashFile(absPath)
				if err != nil {
					plan.Warnings = append(plan.Warnings,
						fmt.Sprintf("recheck: could not hash %q: %v", pf.DestPath, err))
					op.Kind = PlanOpOverwrite
				} else if onDiskHash == existingInstall.ContentSha256 &&
					existingInstall.OwnerModFileVersionID.Int64 == pf.Winner().ModFileVersionID {
					op.Kind = PlanOpNoop
				} else if onDiskHash != existingInstall.ContentSha256 &&
					existingInstall.OwnerModFileVersionID.Int64 == pf.Winner().ModFileVersionID {
					// Same owner but content drifted externally - back up new content
					op.Kind = PlanOpOverwrite
					op.NeedsBackup = true
					plan.Warnings = append(plan.Warnings,
						fmt.Sprintf("drift: %q was modified externally (game update?), will back up current content before overwriting",
							pf.DestPath))
				} else {
					// Different winner - plain overwrite, no backup needed since
					// tool owns the file
					op.Kind = PlanOpOverwrite
				}
			} else {
				// Tool owns it and it's on disk - normal overwrite, no backup
				op.Kind = PlanOpOverwrite
			}

		case isInstalled && !existsOnDisk:
			// Tool thought it owned it but it's gone - drift warning, treat
			// as a fresh write
			op.Kind = PlanOpWrite
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("drift: %q was installed but is missing from disk",
					pf.DestPath))

		case !isInstalled && existsOnDisk:
			// Pre-existing file not owned by the tool - backup before writing
			op.Kind = PlanOpWrite
			op.NeedsBackup = true

		default:
			// Not installed, not on disk - clean write
			op.Kind = PlanOpWrite
		}

		plan.Ops = append(plan.Ops, op)
	}

	// For each currently installed path not in the new winner set, determine
	// whether to remove, restore a backup, or promote a loser
	for relpath, installedFile := range installed {
		if _, stillWanted := winners[relpath]; stillWanted {
			continue
		}

		// Check if the current owner is being displaced by a loser promotion.
		// This happens when the installed file's owner is no longer in the
		// profile but another mod that lost the conflict is still present.
		//
		// Find if any profile item still in the plan provides this path.
		// We need to check plan.Files for any PlanFile at this path that has
		// a loser which is still in the profile - but since this path has no
		// winner in the new plan, we need to check if any conflict entries
		// exist at all for this path from the new plan items.
		//
		// Since winners map only contains paths with at least one enabled mod,
		// a path not in winners means no enabled mod provides it anymore.
		// So we always remove or restore.
		absPath := filepath.Join(target.RootPath, relpath)
		_, existsOnDisk := diskStat(absPath)

		if !existsOnDisk {
			// Already gone - just clean up the DB record, no disk op needed.
			// We still emit a Remove so apply cleans up installed_files
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("drift: %q was installed by mod_file_version %d but is already missing from disk",
					relpath, installedFile.OwnerModFileVersionID.Int64))
			plan.Ops = append(plan.Ops, PlanOp{
				Kind:     PlanOpRemove,
				DestPath: relpath,
			})
			continue
		}

		// Check if a backup exists to restore
		backup, err := q.GetBackupForPath(ctx, dbq.GetBackupForPathParams{
			GameInstallID: gameInstallID,
			TargetID:      target.ID,
			Relpath:       relpath,
		})
		if err != nil && err != sql.ErrNoRows {
			return Plan{}, fmt.Errorf("check backup for %q: %w", relpath, err)
		}

		if err == nil {
			plan.Ops = append(plan.Ops, PlanOp{
				Kind:         PlanOpRestoreBackup,
				DestPath:     relpath,
				BackupSha256: backup.BackupBlobSha256,
			})
		} else {
			plan.Ops = append(plan.Ops, PlanOp{
				Kind:     PlanOpRemove,
				DestPath: relpath,
			})
		}
	}

	return plan, nil
}

// BuildUnapplyPlan computes the operations needed to remove all tool-managed
// files for a game install. It does not require the profile to still exist.
func BuildUnapplyPlan(ctx context.Context, q *dbq.Queries, gameInstallID int64) (Plan, error) {
	target, err := q.GetTargetByName(ctx, dbq.GetTargetByNameParams{
		GameInstallID: gameInstallID,
		Name:          "game_dir",
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return Plan{}, fmt.Errorf("no game_dir target found for game install %d", gameInstallID)
		}
		return Plan{}, fmt.Errorf("resolve target: %w", err)
	}

	plan := Plan{
		GameInstallID: gameInstallID,
		TargetID:      target.ID,
		TargetRoot:    target.RootPath,
	}

	installedFiles, err := q.GetInstalledFilesForTarget(ctx, dbq.GetInstalledFilesForTargetParams{
		GameInstallID: gameInstallID,
		TargetID:      target.ID,
	})
	if err != nil {
		return Plan{}, fmt.Errorf("load installed files: %w", err)
	}

	for _, f := range installedFiles {
		absPath := filepath.Join(target.RootPath, f.Relpath)
		_, existsOnDisk := diskStat(absPath)

		if !existsOnDisk {
			// Already gone - emit remove to clean up DB record, add warning.
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("drift: %q was installed but is already missing from disk", f.Relpath))
			plan.Ops = append(plan.Ops, PlanOp{
				Kind:     PlanOpRemove,
				DestPath: f.Relpath,
			})
			continue
		}

		backup, err := q.GetBackupForPath(ctx, dbq.GetBackupForPathParams{
			GameInstallID: gameInstallID,
			TargetID:      target.ID,
			Relpath:       f.Relpath,
		})
		if err != nil && err != sql.ErrNoRows {
			return Plan{}, fmt.Errorf("check backup for %q: %w", f.Relpath, err)
		}

		op := PlanOp{DestPath: f.Relpath}
		if err == nil {
			op.Kind = PlanOpRestoreBackup
			op.BackupSha256 = backup.BackupBlobSha256
		} else {
			op.Kind = PlanOpRemove
		}
		plan.Ops = append(plan.Ops, op)
	}

	return plan, nil
}

// diskStat checks whether a path exists on disk.
// Returns (info, true) if it exists, (nil, false) if it does not.
func diskStat(path string) (os.FileInfo, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	return info, true
}
