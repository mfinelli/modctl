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

package planner

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shaOf is the sha256 of content, as hex.
func shaOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

type remapSpec struct {
	ruleType  string
	intValue  int64
	textValue string
}

type entrySpec struct {
	rawPath   string
	entryType string // "file" if empty
}

// modSpec describes a mod in a profile.
type modSpec struct {
	name           string
	version        string
	priority       int64 // the next free one if 0
	disabled       bool
	paths          []string // the files in its archive
	extraEntries   []entrySpec
	notInventoried bool
	remap          []remapSpec
	skipBackup     []string
	writeOnce      []string
	targetID       int64 // the fixture's target if 0
}

type addedMod struct {
	versionID  int64
	itemID     int64
	archiveSha string
}

func (f planFixture) exec(t *testing.T, query string, args ...any) int64 {
	t.Helper()

	res, err := f.db.Exec(query, args...)
	require.NoError(t, err, query)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

// mod puts a mod in the profile of the fixture.
func (f planFixture) mod(t *testing.T, spec modSpec) addedMod {
	t.Helper()

	n := modSeq.Add(1)
	if spec.name == "" {
		spec.name = fmt.Sprintf("Mod %d", n)
	}
	if spec.priority == 0 {
		row := f.db.QueryRow(`SELECT COALESCE(MAX(priority), 0) + 1 FROM profile_items WHERE profile_id = ?`, f.profileID)
		require.NoError(t, row.Scan(&spec.priority))
	}
	if spec.targetID == 0 {
		spec.targetID = f.target.ID
	}

	archiveSha := fmt.Sprintf("%064x", n)
	f.exec(t, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, archiveSha)
	page := f.exec(t, `INSERT INTO mod_pages (game_install_id, name, source_kind) VALUES (?, ?, 'local')`, f.gameID, spec.name)
	file := f.exec(t, `INSERT INTO mod_files (mod_page_id, label) VALUES (?, 'Main')`, page)

	var scanned any = "2026-01-01T00:00:00.000Z"
	if spec.notInventoried {
		scanned = nil
	}
	var version any
	if spec.version != "" {
		version = spec.version
	}
	versionID := f.exec(t,
		`INSERT INTO mod_file_versions (mod_file_id, archive_sha256, version_string, inventory_scanned_at) VALUES (?, ?, ?, ?)`,
		file, archiveSha, version, scanned)

	position := 0
	for _, p := range spec.paths {
		f.exec(t, `INSERT INTO archive_inventory_entries (archive_sha256, raw_path, entry_type, size_bytes, position) VALUES (?, ?, 'file', 5, ?)`,
			archiveSha, p, position)
		position++
	}
	for _, e := range spec.extraEntries {
		typ := e.entryType
		if typ == "" {
			typ = "file"
		}
		f.exec(t, `INSERT INTO archive_inventory_entries (archive_sha256, raw_path, entry_type, size_bytes, position) VALUES (?, ?, ?, 5, ?)`,
			archiveSha, e.rawPath, typ, position)
		position++
	}

	var remapConfig any
	if len(spec.remap) > 0 {
		cfg := f.exec(t, `INSERT INTO remap_configs DEFAULT VALUES`)
		for i, r := range spec.remap {
			var intValue, textValue any
			if r.ruleType == "strip_components" {
				intValue = r.intValue
			} else {
				textValue = r.textValue
			}
			f.exec(t, `INSERT INTO remap_rules (remap_config_id, position, rule_type, int_value, text_value) VALUES (?, ?, ?, ?, ?)`,
				cfg, i, r.ruleType, intValue, textValue)
		}
		remapConfig = cfg
	}

	enabled := 1
	if spec.disabled {
		enabled = 0
	}
	itemID := f.exec(t,
		`INSERT INTO profile_items (profile_id, mod_file_version_id, target_id, priority, enabled, remap_config_id) VALUES (?, ?, ?, ?, ?, ?)`,
		f.profileID, versionID, spec.targetID, spec.priority, enabled, remapConfig)

	for _, p := range spec.skipBackup {
		f.exec(t, `INSERT INTO profile_item_skip_backup_patterns (profile_item_id, pattern) VALUES (?, ?)`, itemID, p)
	}
	for _, p := range spec.writeOnce {
		f.exec(t, `INSERT INTO profile_item_write_once_patterns (profile_item_id, pattern) VALUES (?, ?)`, itemID, p)
	}

	return addedMod{versionID: versionID, itemID: itemID, archiveSha: archiveSha}
}

// content puts a file with the given content under the target.
func (f planFixture) content(t *testing.T, relpath, content string) {
	t.Helper()

	abs := filepath.Join(f.root, relpath)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}

// installedAs records a file as installed by a mod version, with the hash of
// content (that is not necessarily what is on disk).
func (f planFixture) installedAs(t *testing.T, relpath string, mod addedMod, content string) {
	t.Helper()

	require.NoError(t, f.q.UpsertInstalledFile(context.Background(), dbq.UpsertInstalledFileParams{
		GameInstallID:         f.gameID,
		TargetID:              f.target.ID,
		Relpath:               relpath,
		ContentSha256:         shaOf(content),
		SizeBytes:             int64(len(content)),
		OwnerModFileVersionID: sql.NullInt64{Int64: mod.versionID, Valid: true},
	}))
}

func destPaths(ops []PlanOp) []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		out = append(out, op.DestPath)
	}
	return out
}

func warningsContaining(plan Plan, substr string) []string {
	var out []string
	for _, w := range plan.Warnings {
		if strings.Contains(w, substr) {
			out = append(out, w)
		}
	}
	return out
}

func TestBuildApplyPlanWinners(t *testing.T) {
	t.Parallel()

	t.Run("each file of a mod is written", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{name: "Big Mod", version: "1.2", paths: []string{"a.dll", "data/b.esp"}})

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"a.dll", "data/b.esp"}, destPaths(plan.Ops), "in the order of the archive")
		for _, op := range plan.Ops {
			assert.Equal(t, PlanOpWrite, op.Kind)
			assert.False(t, op.NeedsBackup)
			require.NotNil(t, op.File)
		}
		require.Len(t, plan.Files, 2)
		winner := plan.Files[1].Winner()
		assert.Equal(t, mod.versionID, winner.ModFileVersionID)
		assert.Equal(t, mod.itemID, winner.ProfileItemID)
		assert.Equal(t, "Big Mod", winner.ModPageName)
		assert.Equal(t, "Main", winner.FileLabel)
		assert.Equal(t, "1.2", winner.VersionString)
		assert.Equal(t, mod.archiveSha, winner.Entry.ArchiveSha256)
		assert.Equal(t, "data/b.esp", winner.Entry.SourcePath)
		assert.Equal(t, "data/b.esp", winner.Entry.DestPath)
		assert.Empty(t, plan.Warnings)
		assert.Equal(t, f.profileID, plan.ProfileID)
	})

	t.Run("an empty profile writes nothing", func(t *testing.T) {
		t.Parallel()

		plan := newPlanFixture(t).buildApply(t, false)

		assert.Empty(t, plan.Ops)
		assert.Empty(t, plan.Files)
		assert.Empty(t, plan.PatchBaseArchives)
	})

	t.Run("the mod with the higher priority wins a path that both have", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		low := f.mod(t, modSpec{name: "Low", priority: 1, paths: []string{"shared.dll", "low-only.dll"}})
		high := f.mod(t, modSpec{name: "High", priority: 2, paths: []string{"shared.dll", "high-only.dll"}})

		plan := f.buildApply(t, false)

		// higher priority first, and then the order of each archive
		assert.Equal(t, []string{"shared.dll", "high-only.dll", "low-only.dll"}, destPaths(plan.Ops))

		shared := plan.Files[0]
		require.Equal(t, "shared.dll", shared.DestPath)
		require.Len(t, shared.Conflicts, 2)
		assert.True(t, shared.Conflicts[0].Won)
		assert.Equal(t, high.versionID, shared.Conflicts[0].ModFileVersionID)
		assert.Equal(t, int64(2), shared.Conflicts[0].Priority)
		assert.False(t, shared.Conflicts[1].Won)
		assert.Equal(t, low.versionID, shared.Conflicts[1].ModFileVersionID)
		assert.Equal(t, int64(1), shared.Conflicts[1].Priority)
		assert.Equal(t, high.itemID, shared.ProfileItemID, "the deploy rules are the winner's")

		assert.Len(t, plan.Files[1].Conflicts, 1, "a path that only one has has no conflict to speak of")
	})

	t.Run("a mod that is disabled is not part of the plan", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{paths: []string{"on.dll"}})
		f.mod(t, modSpec{paths: []string{"off.dll"}, disabled: true})

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"on.dll"}, destPaths(plan.Ops))
	})

	t.Run("a disabled mod does not take a path from the one that is enabled", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		enabled := f.mod(t, modSpec{priority: 1, paths: []string{"shared.dll"}})
		f.mod(t, modSpec{priority: 2, paths: []string{"shared.dll"}, disabled: true})

		plan := f.buildApply(t, false)

		require.Len(t, plan.Files, 1)
		assert.Equal(t, enabled.versionID, plan.Files[0].Winner().ModFileVersionID)
		assert.Len(t, plan.Files[0].Conflicts, 1)
	})

	t.Run("mods for another target are not part of the plan", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		other, err := f.q.UpsertDiscoveredTarget(context.Background(), dbq.UpsertDiscoveredTargetParams{
			GameInstallID: f.gameID,
			Name:          "proton_prefix",
			RootPath:      t.TempDir(),
		})
		require.NoError(t, err)
		f.mod(t, modSpec{paths: []string{"mine.dll"}})
		f.mod(t, modSpec{paths: []string{"theirs.dll"}, targetID: other.ID})

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"mine.dll"}, destPaths(plan.Ops))
	})

	t.Run("entries that are not files are left out", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{
			paths: []string{"real.dll"},
			extraEntries: []entrySpec{
				{rawPath: "somedir/", entryType: "dir"},
				{rawPath: "link.dll", entryType: "symlink"},
			},
		})

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"real.dll"}, destPaths(plan.Ops))
	})

	t.Run("an archive that has not been inventoried is an error that says which", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{name: "Fresh Mod", paths: []string{"a.dll"}, notInventoried: true})

		plan, err := BuildApplyPlan(context.Background(), f.q, f.gameID, f.profileID, f.target, false)

		var unscanned *UninventoriedArchiveError
		require.True(t, errors.As(err, &unscanned), "got %v", err)
		assert.Equal(t, mod.versionID, unscanned.ModFileVersionID)
		assert.Equal(t, "Fresh Mod", unscanned.ModPageName)
		assert.Equal(t, "Main", unscanned.FileLabel)
		assert.Equal(t, mod.archiveSha, unscanned.ArchiveSha256)
		assert.Equal(t, Plan{}, plan)
	})
}

func TestBuildApplyPlanRemap(t *testing.T) {
	t.Parallel()

	t.Run("rules change the path a file is written to", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{
			paths: []string{"ModFolder/Data/a.esp", "ModFolder/Data/b.esp"},
			remap: []remapSpec{
				{ruleType: "strip_components", intValue: 1},
				{ruleType: "dest_prefix", textValue: "GameData"},
			},
		})

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"GameData/Data/a.esp", "GameData/Data/b.esp"}, destPaths(plan.Ops))
		entry := plan.Files[0].Winner().Entry
		assert.Equal(t, "ModFolder/Data/a.esp", entry.SourcePath, "what is read is still the original")
		assert.Equal(t, "GameData/Data/a.esp", entry.DestPath)
	})

	t.Run("a file that the rules skip is not written", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{
			paths: []string{"keep.dll", "docs/readme.txt"},
			remap: []remapSpec{{ruleType: "exclude_glob", textValue: "docs/*"}},
		})

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"keep.dll"}, destPaths(plan.Ops))
		assert.Empty(t, plan.Warnings, "skipping is what the rule is for")
	})

	t.Run("rules are what make two mods conflict or not", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{priority: 1, paths: []string{"same.dll"}, remap: []remapSpec{{ruleType: "dest_prefix", textValue: "one"}}})
		f.mod(t, modSpec{priority: 2, paths: []string{"same.dll"}, remap: []remapSpec{{ruleType: "dest_prefix", textValue: "two"}}})

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"two/same.dll", "one/same.dll"}, destPaths(plan.Ops))
		for _, pf := range plan.Files {
			assert.Len(t, pf.Conflicts, 1)
		}
	})

	t.Run("a path that would leave the game directory is rejected with a warning", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{
			paths: []string{"fine.dll", "escape.dll"},
			remap: []remapSpec{{ruleType: "dest_prefix", textValue: "../outside"}},
		})
		f.mod(t, modSpec{paths: []string{"ok.dll"}})

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"ok.dll"}, destPaths(plan.Ops))
		rejected := warningsContaining(plan, "rejected")
		require.Len(t, rejected, 2)
		assert.Contains(t, rejected[0], "path traversal not allowed")
	})

	t.Run("an absolute path is rejected with a warning", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{paths: []string{"/etc/passwd", "ok.dll"}})

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"ok.dll"}, destPaths(plan.Ops))
		rejected := warningsContaining(plan, "rejected")
		require.Len(t, rejected, 1)
		assert.Contains(t, rejected[0], "absolute path not allowed")
	})

	t.Run("a rule that can't be applied is a warning, and the rest goes on", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{
			priority: 1,
			paths:    []string{"broken.dll"},
			remap:    []remapSpec{{ruleType: "include_glob", textValue: "["}},
		})
		f.mod(t, modSpec{priority: 2, paths: []string{"fine.dll"}})

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{"fine.dll"}, destPaths(plan.Ops))
		require.Len(t, warningsContaining(plan, "remap error"), 1)
	})
}

func TestBuildApplyPlanReconciliation(t *testing.T) {
	t.Parallel()

	t.Run("a file that is not there is a clean write", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{paths: []string{"a.dll"}})

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "a.dll")
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.False(t, op.NeedsBackup)
		assert.Empty(t, plan.Warnings)
	})

	t.Run("a file that is there and isn't ours is backed up before it is written", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.content(t, "a.dll", "the game's own")
		f.mod(t, modSpec{paths: []string{"a.dll"}})

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "a.dll")
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.True(t, op.NeedsBackup)
		assert.Empty(t, plan.Warnings)
	})

	t.Run("not when a skip-backup rule says not to", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.content(t, "settings.ini", "the game's own")
		f.mod(t, modSpec{paths: []string{"settings.ini", "other.ini"}, skipBackup: []string{"settings.ini"}})
		f.content(t, "other.ini", "the game's own")

		plan := f.buildApply(t, false)

		skipped := opFor(t, plan, "settings.ini")
		assert.Equal(t, PlanOpWrite, skipped.Kind)
		assert.False(t, skipped.NeedsBackup)
		assert.True(t, skipped.SkipBackup)
		assert.True(t, opFor(t, plan, "other.ini").NeedsBackup, "the pattern is for that file only")
		assert.False(t, opFor(t, plan, "other.ini").SkipBackup)
	})

	t.Run("a file of ours that is the same as it was left is not touched", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"a.dll"}})
		f.content(t, "a.dll", "mod content")
		f.installedAs(t, "a.dll", mod, "mod content")

		plan := f.buildApply(t, false)

		assert.Equal(t, PlanOpNoop, opFor(t, plan, "a.dll").Kind)
		assert.Empty(t, plan.Warnings)
	})

	t.Run("a file of ours that has changed is backed up and overwritten, with a warning", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"a.dll"}})
		f.content(t, "a.dll", "changed by a game update")
		f.installedAs(t, "a.dll", mod, "mod content")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "a.dll")
		assert.Equal(t, PlanOpOverwrite, op.Kind)
		assert.True(t, op.NeedsBackup)
		drift := warningsContaining(plan, "drift")
		require.Len(t, drift, 1)
		assert.Contains(t, drift[0], "will back up current content")
	})

	t.Run("and without the backup when a skip-backup rule says so", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"a.dll"}, skipBackup: []string{"a.dll"}})
		f.content(t, "a.dll", "changed by a game update")
		f.installedAs(t, "a.dll", mod, "mod content")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "a.dll")
		assert.Equal(t, PlanOpOverwrite, op.Kind)
		assert.False(t, op.NeedsBackup)
		drift := warningsContaining(plan, "drift")
		require.Len(t, drift, 1)
		assert.Contains(t, drift[0], "skip-backup rule active")
	})

	t.Run("a file that another mod installed is overwritten, and not backed up", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		old := f.mod(t, modSpec{priority: 1, paths: []string{"other.dll"}})
		current := f.mod(t, modSpec{priority: 2, paths: []string{"a.dll"}})
		_ = current
		f.content(t, "a.dll", "installed by the old one")
		f.installedAs(t, "a.dll", old, "installed by the old one")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "a.dll")
		assert.Equal(t, PlanOpOverwrite, op.Kind)
		assert.False(t, op.NeedsBackup)
		assert.Empty(t, warningsContaining(plan, "drift"))
	})

	t.Run("a file of ours that is gone is written again, with a warning", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"a.dll"}})
		f.installedAs(t, "a.dll", mod, "mod content")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "a.dll")
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.False(t, op.NeedsBackup)
		missing := warningsContaining(plan, "missing from disk")
		require.Len(t, missing, 1)
		assert.Contains(t, missing[0], "drift")
	})

	t.Run("a file that can't be hashed is overwritten, with a warning", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"a.dll"}})
		// a directory can be looked at but not read, whoever runs the test
		require.NoError(t, os.MkdirAll(filepath.Join(f.root, "a.dll"), 0o755))
		f.installedAs(t, "a.dll", mod, "mod content")

		plan := f.buildApply(t, false)

		assert.Equal(t, PlanOpOverwrite, opFor(t, plan, "a.dll").Kind)
		require.Len(t, warningsContaining(plan, "could not hash"), 1)
	})

	t.Run("without the recheck a file of ours is overwritten whatever it holds", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"same.dll", "changed.dll"}})
		f.content(t, "same.dll", "mod content")
		f.installedAs(t, "same.dll", mod, "mod content")
		f.content(t, "changed.dll", "changed by a game update")
		f.installedAs(t, "changed.dll", mod, "mod content")

		plan := f.buildApply(t, true)

		for _, name := range []string{"same.dll", "changed.dll"} {
			op := opFor(t, plan, name)
			assert.Equal(t, PlanOpOverwrite, op.Kind, name)
			assert.False(t, op.NeedsBackup, name)
		}
		assert.Empty(t, plan.Warnings, "nothing was looked at")
	})

	t.Run("without the recheck a file that isn't ours is still backed up", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.content(t, "a.dll", "the game's own")
		f.mod(t, modSpec{paths: []string{"a.dll"}})

		plan := f.buildApply(t, true)

		op := opFor(t, plan, "a.dll")
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.True(t, op.NeedsBackup)
	})

	t.Run("a path in the way that can't be looked at stops the plan", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.content(t, "data", "a file where a directory should be")
		f.mod(t, modSpec{paths: []string{"data/mod.dll"}})

		plan, err := BuildApplyPlan(context.Background(), f.q, f.gameID, f.profileID, f.target, false)

		require.Error(t, err)
		assert.Contains(t, err.Error(), `stat "data/mod.dll"`)
		assert.Equal(t, Plan{}, plan)
	})

	t.Run("a canceled context is an error and not a plan of warnings", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"a.dll"}})
		f.content(t, "a.dll", "mod content")
		f.installedAs(t, "a.dll", mod, "mod content")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := BuildApplyPlan(ctx, f.q, f.gameID, f.profileID, f.target, false)

		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("what the profile no longer has goes while the rest is written", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{paths: []string{"new.dll"}})
		f.content(t, "old.dll", "left from a mod that is gone")
		f.installed(t, "old.dll")

		plan := f.buildApply(t, false)

		assert.Equal(t, PlanOpWrite, opFor(t, plan, "new.dll").Kind)
		assert.Equal(t, PlanOpRemove, opFor(t, plan, "old.dll").Kind)
		assert.Equal(t, []string{"new.dll", "old.dll"}, destPaths(plan.Ops), "writes first, then removals")
	})
}

func TestBuildApplyPlanWriteOnce(t *testing.T) {
	t.Parallel()

	t.Run("a file that is there is left as it is", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"settings.ini"}, writeOnce: []string{"settings.ini"}})
		f.content(t, "settings.ini", "as deployed")
		f.installedAs(t, "settings.ini", mod, "as deployed")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "settings.ini")
		assert.Equal(t, PlanOpNoop, op.Kind)
		assert.True(t, op.WriteOnce)
		assert.Empty(t, plan.Warnings)
	})

	t.Run("even if it was changed, which is mentioned", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"settings.ini"}, writeOnce: []string{"settings.ini"}})
		f.content(t, "settings.ini", "changed by the game")
		f.installedAs(t, "settings.ini", mod, "as deployed")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "settings.ini")
		assert.Equal(t, PlanOpNoop, op.Kind)
		assert.False(t, op.NeedsBackup)
		modified := warningsContaining(plan, "write-once")
		require.Len(t, modified, 1)
		assert.Contains(t, modified[0], "modified since last deploy")
	})

	t.Run("without the recheck it is not even looked at", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"settings.ini"}, writeOnce: []string{"settings.ini"}})
		f.content(t, "settings.ini", "changed by the game")
		f.installedAs(t, "settings.ini", mod, "as deployed")

		plan := f.buildApply(t, true)

		assert.Equal(t, PlanOpNoop, opFor(t, plan, "settings.ini").Kind)
		assert.Empty(t, plan.Warnings)
	})

	t.Run("a file that is gone is deployed again", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"settings.ini"}, writeOnce: []string{"settings.ini"}})
		f.installedAs(t, "settings.ini", mod, "as deployed")

		plan := f.buildApply(t, false)

		assert.Equal(t, PlanOpWrite, opFor(t, plan, "settings.ini").Kind)
		require.Len(t, warningsContaining(plan, "missing from disk"), 1)
	})

	t.Run("a file that was there first is backed up and written, once", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{paths: []string{"settings.ini"}, writeOnce: []string{"settings.ini"}})
		f.content(t, "settings.ini", "the game's own")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "settings.ini")
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.True(t, op.NeedsBackup)
		assert.True(t, op.WriteOnce)
	})

	t.Run("the pattern is for that file only", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"settings.ini", "mod.dll"}, writeOnce: []string{"settings.ini"}})
		for _, name := range []string{"settings.ini", "mod.dll"} {
			f.content(t, name, "changed")
			f.installedAs(t, name, mod, "as deployed")
		}

		plan := f.buildApply(t, false)

		assert.Equal(t, PlanOpNoop, opFor(t, plan, "settings.ini").Kind)
		assert.Equal(t, PlanOpOverwrite, opFor(t, plan, "mod.dll").Kind)
		assert.False(t, opFor(t, plan, "mod.dll").WriteOnce)
	})
}

// fullFileOverride adds an override of a whole file for relpath, and returns
// its id and the sha256 of the blob that has its content.
func (f planFixture) fullFileOverride(t *testing.T, relpath string) (id int64, blobSha string) {
	t.Helper()

	n := modSeq.Add(1)
	blobSha = fmt.Sprintf("%064x", n)
	f.exec(t, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'override', 1)`, blobSha)
	id = f.exec(t, `INSERT INTO overrides (profile_id, target_id, relpath, blob_sha256, override_type) VALUES (?, ?, ?, ?, 'full_file')`,
		f.profileID, f.target.ID, relpath, blobSha)
	return id, blobSha
}

// patchOverride adds an override that patches the file at baseRawPath in the
// archive with the sha256 baseArchive.
func (f planFixture) patchOverride(t *testing.T, relpath, overrideType, baseArchive, baseRawPath string) int64 {
	t.Helper()

	return f.exec(t, `INSERT INTO overrides (profile_id, target_id, relpath, override_type, source_archive_sha256, source_raw_path) VALUES (?, ?, ?, ?, ?, ?)`,
		f.profileID, f.target.ID, relpath, overrideType, baseArchive, baseRawPath)
}

// archive adds an archive to the store that no mod uses.
func (f planFixture) archive(t *testing.T) string {
	t.Helper()

	sha := fmt.Sprintf("%064x", modSeq.Add(1))
	f.exec(t, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'archive', 1)`, sha)
	return sha
}

func (f planFixture) installedByOverride(t *testing.T, relpath string, overrideID int64, content string) {
	t.Helper()

	require.NoError(t, f.q.UpsertInstalledFile(context.Background(), dbq.UpsertInstalledFileParams{
		GameInstallID:   f.gameID,
		TargetID:        f.target.ID,
		Relpath:         relpath,
		ContentSha256:   shaOf(content),
		SizeBytes:       int64(len(content)),
		OwnerOverrideID: sql.NullInt64{Int64: overrideID, Valid: true},
	}))
}

func TestBuildApplyPlanOverrides(t *testing.T) {
	t.Parallel()

	t.Run("an override of a file that a mod has is written instead of it", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"settings.ini"}})
		id, blob := f.fullFileOverride(t, "settings.ini")

		plan := f.buildApply(t, false)

		require.Len(t, plan.Ops, 1)
		op := plan.Ops[0]
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.Equal(t, sql.NullInt64{Int64: id, Valid: true}, op.OverrideID)
		assert.Equal(t, "full_file", op.OverrideType)
		assert.Equal(t, sql.NullString{String: blob, Valid: true}, op.OverrideBlobSha256)
		require.NotNil(t, op.File, "the mod's claim is still there")
		assert.Equal(t, mod.versionID, op.File.Winner().ModFileVersionID)
	})

	t.Run("an override of a file that no mod has is a path of its own", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		id, blob := f.fullFileOverride(t, "extra/user.cfg")

		plan := f.buildApply(t, false)

		require.Len(t, plan.Ops, 1)
		op := plan.Ops[0]
		assert.Equal(t, "extra/user.cfg", op.DestPath)
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.Nil(t, op.File, "no mod to take it from")
		assert.Equal(t, sql.NullInt64{Int64: id, Valid: true}, op.OverrideID)
		assert.Equal(t, sql.NullString{String: blob, Valid: true}, op.OverrideBlobSha256)
		require.Len(t, plan.Files, 1)
		assert.Empty(t, plan.Files[0].Conflicts)
	})

	t.Run("a patch says which file of which archive it is a patch of", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		base := f.archive(t)
		id := f.patchOverride(t, "settings.ini", "ini_patch", base, "Mod/settings.ini")

		plan := f.buildApply(t, false)

		require.Len(t, plan.Ops, 1)
		op := plan.Ops[0]
		assert.Equal(t, PlanOpWrite, op.Kind)
		assert.Equal(t, sql.NullInt64{Int64: id, Valid: true}, op.OverrideID)
		assert.Equal(t, "ini_patch", op.OverrideType)
		assert.Equal(t, sql.NullString{String: base, Valid: true}, op.OverrideBaseArchiveSha256)
		assert.Equal(t, sql.NullString{String: "Mod/settings.ini", Valid: true}, op.OverrideBaseRawPath)
		assert.False(t, op.OverrideBlobSha256.Valid)
		assert.Equal(t, []string{base}, plan.PatchBaseArchives, "it has to be unpacked for it")
	})

	t.Run("an archive that a mod is written from is not asked for again", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"settings.ini"}})
		f.patchOverride(t, "settings.ini", "ini_patch", mod.archiveSha, "settings.ini")

		plan := f.buildApply(t, false)

		assert.Empty(t, plan.PatchBaseArchives)
	})

	t.Run("an archive that two patches are for is asked for once", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		base := f.archive(t)
		f.patchOverride(t, "a.ini", "ini_patch", base, "a.ini")
		f.patchOverride(t, "b.json", "json_patch", base, "b.json")

		plan := f.buildApply(t, false)

		assert.Equal(t, []string{base}, plan.PatchBaseArchives)
		assert.Len(t, plan.Ops, 2)
	})

	t.Run("a full file override is not an archive to unpack", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{paths: []string{"settings.ini"}})
		f.fullFileOverride(t, "settings.ini")

		assert.Empty(t, f.buildApply(t, false).PatchBaseArchives)
	})

	t.Run("deploy rules are for the mod and not for its override", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		f.mod(t, modSpec{
			paths:      []string{"settings.ini"},
			skipBackup: []string{"settings.ini"},
			writeOnce:  []string{"settings.ini"},
		})
		f.content(t, "settings.ini", "the game's own")
		f.fullFileOverride(t, "settings.ini")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "settings.ini")
		assert.False(t, op.SkipBackup)
		assert.False(t, op.WriteOnce)
		assert.True(t, op.NeedsBackup, "what is there is backed up, which the skip-backup rule of the mod would have said not to")
	})

	t.Run("a file that the override installed is left alone while it is the same", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		id, _ := f.fullFileOverride(t, "user.cfg")
		f.content(t, "user.cfg", "my settings")
		f.installedByOverride(t, "user.cfg", id, "my settings")

		plan := f.buildApply(t, false)

		assert.Equal(t, PlanOpNoop, opFor(t, plan, "user.cfg").Kind)
		assert.Empty(t, plan.Warnings)
	})

	t.Run("and written again when it has changed", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		id, _ := f.fullFileOverride(t, "user.cfg")
		f.content(t, "user.cfg", "changed by the game")
		f.installedByOverride(t, "user.cfg", id, "my settings")

		assert.Equal(t, PlanOpOverwrite, opFor(t, f.buildApply(t, false), "user.cfg").Kind)
	})

	t.Run("or when it is one that the mod installed", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		mod := f.mod(t, modSpec{paths: []string{"settings.ini"}})
		f.fullFileOverride(t, "settings.ini")
		f.content(t, "settings.ini", "mod content")
		f.installedAs(t, "settings.ini", mod, "mod content")

		plan := f.buildApply(t, false)

		op := opFor(t, plan, "settings.ini")
		assert.Equal(t, PlanOpOverwrite, op.Kind, "same content, but it is not the override that put it there")
		assert.False(t, op.NeedsBackup)
	})

	t.Run("without the recheck it is always written again", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		id, _ := f.fullFileOverride(t, "user.cfg")
		f.content(t, "user.cfg", "my settings")
		f.installedByOverride(t, "user.cfg", id, "my settings")

		assert.Equal(t, PlanOpOverwrite, opFor(t, f.buildApply(t, true), "user.cfg").Kind)
	})

	t.Run("a patch is never skipped, since what it is applied to may have changed", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		base := f.archive(t)
		id := f.patchOverride(t, "settings.ini", "ini_patch", base, "settings.ini")
		f.content(t, "settings.ini", "patched")
		f.installedByOverride(t, "settings.ini", id, "patched")

		assert.Equal(t, PlanOpOverwrite, opFor(t, f.buildApply(t, false), "settings.ini").Kind)
	})

	t.Run("a file that the override installed and is gone is written again, with a warning", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		id, _ := f.fullFileOverride(t, "user.cfg")
		f.installedByOverride(t, "user.cfg", id, "my settings")

		plan := f.buildApply(t, false)

		assert.Equal(t, PlanOpWrite, opFor(t, plan, "user.cfg").Kind)
		require.Len(t, warningsContaining(plan, "missing from disk"), 1)
	})

	t.Run("overrides of another target are left out", func(t *testing.T) {
		t.Parallel()

		f := newPlanFixture(t)
		other, err := f.q.UpsertDiscoveredTarget(context.Background(), dbq.UpsertDiscoveredTargetParams{
			GameInstallID: f.gameID,
			Name:          "proton_prefix",
			RootPath:      t.TempDir(),
		})
		require.NoError(t, err)
		blob := fmt.Sprintf("%064x", modSeq.Add(1))
		f.exec(t, `INSERT INTO blobs (sha256, kind, size_bytes) VALUES (?, 'override', 1)`, blob)
		f.exec(t, `INSERT INTO overrides (profile_id, target_id, relpath, blob_sha256, override_type) VALUES (?, ?, 'theirs.cfg', ?, 'full_file')`,
			f.profileID, other.ID, blob)

		assert.Empty(t, f.buildApply(t, false).Ops)
	})
}
