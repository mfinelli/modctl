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
	"strings"
	"time"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/extractor"
	"github.com/mfinelli/modctl/internal/lock"
	"github.com/mfinelli/modctl/internal/patchapply"
	"github.com/mfinelli/modctl/internal/planner"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	applyGame          string
	applyProfile       string
	applyDryRun        bool
	applySkipRecheck   bool
	applyKeepStaging   bool
	applyVerbose       bool
	applyForce         bool
	applyAbort         bool
	applyPruneDirs     bool
	applyShowConflicts bool
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a profile to a game install",
	Long: `Apply a profile to a game install.

Computes the desired file state for the active (or specified) profile and
reconciles it with the current state on disk. Files are extracted from mod
archives to a staging directory and then moved into the game directory.

Pre-existing files that would be overwritten are backed up automatically
and can be restored with 'modctl unapply'.

By default, files that are already correctly deployed are skipped (noop)
and externally modified files are detected and backed up before being
overwritten. Use --no-recheck to skip hash checks for faster applies.

Use --dry-run to preview the plan without making any changes. Add the
--show-conflicts flag to get details file conflict information.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if err := internal.EnsureDBExists(); err != nil {
			return err
		}
		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()
		if err := internal.MigrateDB(ctx, db); err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		q := dbq.New(db)

		// Resolve game install
		if applyGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			applyGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, applyGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, applyProfile)
		if err != nil {
			return err
		}

		// Check for incomplete previous operation
		lastOp, err := q.GetLastOperationForGameInstall(ctx, gi.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check last operation: %w", err)
		}
		if err == nil && lastOp.Status == "running" {
			if !applyAbort && !applyForce {
				return fmt.Errorf(
					"last apply/unapply operation (#%d, started %s) did not complete\n"+
						"  your game directory may be in a partially applied state\n"+
						"  options:\n"+
						"    --abort    mark the operation as failed and exit\n"+
						"    --force    mark the operation as failed and start a fresh apply",
					lastOp.ID, lastOp.StartedAt,
				)
			}
			if err := q.FinishOperation(ctx, dbq.FinishOperationParams{
				Status:  "failed",
				Message: sql.NullString{String: "marked failed by user", Valid: true},
				ID:      lastOp.ID,
			}); err != nil {
				return fmt.Errorf("mark last operation failed: %w", err)
			}
			if applyAbort {
				style.Println("Operation marked as failed. Run 'modctl apply' to reapply or 'modctl unapply' to clean up.")
				return nil
			}
		}

		// Acquire per-game lock to prevent concurrent apply/unapply
		unlock, err := lock.GameInstall(viper.GetString("locks_dir"), gi.ID)
		if err != nil {
			return err
		}
		defer unlock()

		allTargets, err := q.ListTargetsForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list targets: %w", err)
		}
		if len(allTargets) == 0 {
			return fmt.Errorf("no targets found for game install %d", gi.ID)
		}

		// Disabled targets are skipped entirely
		targets := internal.EnabledTargets(allTargets)
		if len(targets) == 0 {
			return fmt.Errorf("no enabled targets for game install %d; run `modctl games targets enable <name>`", gi.ID)
		}

		var plans []planner.Plan
		for _, target := range targets {
			plan, err := planner.BuildApplyPlan(ctx, q, gi.ID, p.ID, target, applySkipRecheck)
			if err != nil {
				var uninvErr *planner.UninventoriedArchiveError
				if errors.As(err, &uninvErr) {
					return fmt.Errorf("%w\nrun 'modctl mods scan-inventory' to fix", uninvErr)
				}
				return fmt.Errorf("build plan for target %q: %w", target.Name, err)
			}
			plans = append(plans, plan)
		}

		// Dry-run output
		if applyDryRun {
			for _, plan := range plans {
				printApplyPlan(plan, p.Name, gi.DisplayName, applyShowConflicts)
			}
			return nil
		}

		// Real apply
		targetNames := make([]string, len(plans))
		for i, plan := range plans {
			targetNames[i] = plan.TargetName
		}
		targetLabel := "target"
		if len(plans) > 1 {
			targetLabel = "targets"
		}
		style.Println(style.Bold.Render(fmt.Sprintf("Applying %q → %s", p.Name, gi.DisplayName)) +
			"  " + style.Subtle.Render(fmt.Sprintf("(%s: %s)", targetLabel, strings.Join(targetNames, ", "))))
		style.Println()

		bs := blobstore.Store{
			ArchivesDir:  viper.GetString("archives_dir"),
			BackupsDir:   viper.GetString("backups_dir"),
			OverridesDir: viper.GetString("overrides_dir"),
			TmpDir:       viper.GetString("tmp_dir"),
		}

		ext := extractor.Extractor{
			BsdtarPath: viper.GetString("bsdtar"),
			BlobStore:  bs,
			StagingDir: viper.GetString("tmp_dir"),
		}

		// Clear staging before starting.
		if err := ext.ClearStaging(ctx); err != nil {
			return fmt.Errorf("clear staging: %w", err)
		}

		// Create operation record
		op, err := q.CreateOperation(ctx, dbq.CreateOperationParams{
			GameInstallID: gi.ID,
			ProfileID:     sql.NullInt64{Int64: p.ID, Valid: true},
			OpType:        "apply",
		})
		if err != nil {
			return fmt.Errorf("create operation: %w", err)
		}

		// Group write/overwrite ops by archive sha256 for extraction
		type archiveGroup struct {
			sha256 string
			ops    []planner.PlanOp
		}

		// Counters for summary
		total := 0
		for _, plan := range plans {
			total += len(plan.Ops)
		}

		current := 0
		var (
			countWrite     int
			countOverwrite int
			countRemove    int
			countRestore   int
			countBackedUp  int
		)

		// markFailed marks the operation as failed and returns the error
		markFailed := func(err error) error {
			_ = q.FinishOperation(ctx, dbq.FinishOperationParams{
				Status:  "failed",
				Message: sql.NullString{String: err.Error(), Valid: true},
				ID:      op.ID,
			})
			return err
		}

		// Helper to print progress
		width := len(strconv.Itoa(total))
		fmtCounter := fmt.Sprintf("[%%%dd/%%%dd]", width, width)

		// Print an initial line so \r updates have something to overwrite
		if !applyVerbose {
			style.Printf("  [%*d/%d] ...", width, 0, total)
		}

		// With several targets in one run, say which one each op belongs to
		multiTarget := len(plans) > 1
		var currentTarget string

		printOp := func(symbol, path, detail string) {
			current++
			if multiTarget {
				if detail != "" {
					detail = currentTarget + "  " + detail
				} else {
					detail = currentTarget
				}
			}
			line := fmt.Sprintf("  "+fmtCounter+" %s %s", current, total, symbol, path)
			if detail != "" {
				line += style.Subtle.Render("  " + detail)
			}
			if applyVerbose {
				style.Println(line)
			} else {
				style.Printf("\r%-*s", 80, line)
			}
		}

		for _, plan := range plans {
			currentTarget = plan.TargetName
			if multiTarget && applyVerbose {
				style.Println(style.Subtle.Render(fmt.Sprintf("  target: %s", plan.TargetName)))
			}

			archiveMap := make(map[string]*archiveGroup)
			var archiveOrder []string
			var overrideOps []planner.PlanOp
			var removeOps []planner.PlanOp
			var restoreOps []planner.PlanOp

			for _, planOp := range plan.Ops {
				switch planOp.Kind {
				case planner.PlanOpWrite, planner.PlanOpOverwrite:
					if planOp.OverrideID.Valid {
						overrideOps = append(overrideOps, planOp)
						// For patch overrides, the base archive is handled via
						// PatchBaseArchives below, not grouped here
					} else {
						sha := planOp.File.Winner().Entry.ArchiveSha256
						if _, ok := archiveMap[sha]; !ok {
							archiveMap[sha] = &archiveGroup{sha256: sha}
							archiveOrder = append(archiveOrder, sha)
						}
						archiveMap[sha].ops = append(archiveMap[sha].ops, planOp)
					}
				case planner.PlanOpRemove:
					removeOps = append(removeOps, planOp)
				case planner.PlanOpRestoreBackup:
					restoreOps = append(restoreOps, planOp)
				case planner.PlanOpNoop:
					logger.Debug("skipping noop op", "path", planOp.DestPath)
				}
			}

			// Add patch base archives to the staging set if not already present
			// No ops added; archive is staged for patch base file access only
			for _, sha := range plan.PatchBaseArchives {
				if _, ok := archiveMap[sha]; !ok {
					archiveMap[sha] = &archiveGroup{sha256: sha}
					archiveOrder = append(archiveOrder, sha)
				}
			}

			// Extract and deploy archives
			for _, sha := range archiveOrder {
				group := archiveMap[sha]
				stagingPath, err := ext.ExtractArchive(ctx, sha)
				if err != nil {
					return markFailed(fmt.Errorf("extract archive %.16s: %w", sha, err))
				}

				for _, planOp := range group.ops {
					symbol := style.Added.Render("+")
					detail := ""
					if planOp.Kind == planner.PlanOpOverwrite {
						symbol = style.Changed.Render("~")
					}
					if planOp.NeedsBackup {
						detail = "(backing up original)"
					}
					printOp(symbol, planOp.DestPath, detail)

					result, err := ext.DeployFile(ctx, db, q, planOp, stagingPath, plan.TargetRoot, gi.ID, plan.TargetID, p.ID, op.ID)
					if err != nil {
						if applyVerbose {
							style.Println(style.Warning.Render(fmt.Sprintf("    ✗ %v", err)))
						}
						return markFailed(fmt.Errorf("deploy %q: %w", planOp.DestPath, err))
					}

					if planOp.Kind == planner.PlanOpOverwrite {
						countOverwrite++
					} else {
						countWrite++
					}
					if result.WasBackedUp {
						countBackedUp++
					}
				}
			}

			// Deploy override ops
			for _, planOp := range overrideOps {
				symbol := style.Added.Render("+")
				detail := "(override)"
				if planOp.Kind == planner.PlanOpOverwrite {
					symbol = style.Changed.Render("~")
				}
				if planOp.NeedsBackup {
					detail = "(override, backing up original)"
				}
				printOp(symbol, planOp.DestPath, detail)

				// Load patch entries if needed
				var patchEntries []patchapply.Entry
				if planOp.OverrideType != "full_file" {
					dbEntries, err := q.ListOverridePatchEntries(ctx, planOp.OverrideID.Int64)
					if err != nil {
						return markFailed(fmt.Errorf("load patch entries for %q: %w", planOp.DestPath, err))
					}
					for _, e := range dbEntries {
						patchEntries = append(patchEntries, patchapply.Entry{
							PatchType:    e.PatchType,
							EntrySection: e.EntrySection.String,
							EntryKey:     e.EntryKey,
							EntryValue:   e.EntryValue.String,
						})
					}
				}

				result, err := ext.DeployOverrideFile(
					ctx, db, q, planOp,
					ext.StagingPathFor(planOp.OverrideBaseArchiveSha256.String),
					plan.TargetRoot, gi.ID, plan.TargetID, p.ID, op.ID,
					patchEntries,
				)
				if err != nil {
					return markFailed(fmt.Errorf("deploy override %q: %w", planOp.DestPath, err))
				}

				if planOp.Kind == planner.PlanOpOverwrite {
					countOverwrite++
				} else {
					countWrite++
				}
				if result.WasBackedUp {
					countBackedUp++
				}
			}

			var removedPaths []string

			// Remove ops
			for _, planOp := range removeOps {
				printOp(style.Removed.Render("-"), planOp.DestPath, "")
				if _, err := ext.RemoveFile(ctx, db, q, planOp, plan.TargetRoot, gi.ID, plan.TargetID, op.ID); err != nil {
					return markFailed(fmt.Errorf("remove %q: %w", planOp.DestPath, err))
				}
				countRemove++
				removedPaths = append(removedPaths, planOp.DestPath)
			}

			// Restore ops
			for _, planOp := range restoreOps {
				printOp(style.Restored.Render("↩"), planOp.DestPath, "")
				if _, err := ext.RestoreFile(ctx, db, q, planOp, plan.TargetRoot, gi.ID, plan.TargetID, op.ID); err != nil {
					return markFailed(fmt.Errorf("restore %q: %w", planOp.DestPath, err))
				}
				countRestore++
			}

			// Prune empty directories if requested
			if applyPruneDirs {
				pruneWarnings := extractor.PruneDirs(plan.TargetRoot, removedPaths)
				plan.Warnings = append(plan.Warnings, pruneWarnings...)
			}
		}

		// Clear the spinner line before printing summary
		if !applyVerbose {
			style.Print("\r" + strings.Repeat(" ", 80) + "\r")
		}

		// Mark operation successful and update applied state in one transaction
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return markFailed(fmt.Errorf("begin final transaction: %w", err))
		}
		defer tx.Rollback()

		qtx := q.WithTx(tx)

		if err := qtx.FinishOperation(ctx, dbq.FinishOperationParams{
			Status:  "success",
			Message: sql.NullString{},
			ID:      op.ID,
		}); err != nil {
			return markFailed(fmt.Errorf("finish operation: %w", err))
		}

		if err := qtx.UpdateGameInstallAppliedState(ctx, dbq.UpdateGameInstallAppliedStateParams{
			AppliedProfileID:   sql.NullInt64{Int64: p.ID, Valid: true},
			AppliedOperationID: sql.NullInt64{Int64: op.ID, Valid: true},
			ID:                 gi.ID,
		}); err != nil {
			return markFailed(fmt.Errorf("update applied state: %w", err))
		}

		if err := tx.Commit(); err != nil {
			return markFailed(fmt.Errorf("commit final transaction: %w", err))
		}

		// Cleanup staging unless --keep-staging
		if !applyKeepStaging {
			if err := ext.CleanupStaging(ctx); err != nil {
				// Non-fatal - warn but don't fail the apply
				style.Println(style.Warning.Render(fmt.Sprintf("  warning: cleanup staging: %v", err)))
			}
		} else {
			style.Println(style.Subtle.Render(fmt.Sprintf("  staging kept at: %s", ext.StagingPathFor(""))))
		}

		// Summary
		elapsed := time.Since(mustParseTime(op.StartedAt))
		style.Println(style.Bold.Render(fmt.Sprintf("Apply complete in %.1fs", elapsed.Seconds())))
		if countWrite > 0 {
			style.Printf("  written:     %d\n", countWrite)
		}
		if countOverwrite > 0 {
			style.Printf("  overwritten: %d\n", countOverwrite)
		}
		if countRemove > 0 {
			style.Printf("  removed:     %d\n", countRemove)
		}
		if countRestore > 0 {
			style.Printf("  restored:    %d\n", countRestore)
		}
		if countBackedUp > 0 {
			style.Printf("  backed up:   %d\n", countBackedUp)
		}
		var allWarnings []string
		for _, plan := range plans {
			allWarnings = append(allWarnings, plan.Warnings...)
		}
		if len(allWarnings) > 0 {
			style.Println(style.Warning.Render(fmt.Sprintf("  warnings:    %d", len(allWarnings))))
			for _, w := range allWarnings {
				style.Println(style.Warning.Render("    ⚠  " + w))
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(applyCmd)

	applyCmd.Flags().StringVarP(&applyGame, "game", "g", "",
		"Override the currently active game")
	applyCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	applyCmd.Flags().StringVarP(&applyProfile, "profile", "p", "",
		"Override the currently active profile")
	applyCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	applyCmd.Flags().BoolVar(&applyDryRun, "dry-run", false,
		"Preview the plan without making any changes")
	applyCmd.Flags().BoolVar(&applySkipRecheck, "no-recheck", false,
		"Skip on-disk hash checks during apply (faster but will not detect or back up externally modified files)")
	applyCmd.Flags().BoolVar(&applyKeepStaging, "keep-staging", false,
		"Keep staging directory after apply (useful for debugging)")
	applyCmd.Flags().BoolVar(&applyVerbose, "print-ops", false,
		"Print each operation on a new line instead of using a progress indicator")
	applyCmd.Flags().BoolVar(&applyForce, "force", false,
		"Mark any incomplete operation as failed and start fresh")
	applyCmd.Flags().BoolVar(&applyAbort, "abort", false,
		"Mark any incomplete operation as failed and exit")
	applyCmd.Flags().BoolVar(&applyPruneDirs, "prune-dirs", false,
		"Remove empty directories left behind after file removals")
	applyCmd.Flags().BoolVar(&applyShowConflicts, "show-conflicts", false,
		"Show losing mods for each conflicted path (implies --dry-run)")
}

// printApplyPlan renders the dry-run plan output
func printApplyPlan(
	plan planner.Plan,
	profileName string,
	gameName string,
	showConflicts bool,
) {
	style.Println(style.Bold.Render(fmt.Sprintf("Apply plan for %q → %s", profileName, gameName)) +
		"  " + style.Subtle.Render(fmt.Sprintf("(target: %s)", plan.TargetName)))
	style.Println()

	var (
		countWrite     int
		countOverwrite int
		countRemove    int
		countRestore   int
		countBackup    int
		countConflict  int
	)

	for _, op := range plan.Ops {
		switch op.Kind {
		case planner.PlanOpWrite:
			symbol := style.Added.Render("+")
			detail := ""
			if op.NeedsBackup {
				detail = style.Subtle.Render("(backup needed)")
				countBackup++
			} else if op.SkipBackup {
				detail = style.Subtle.Render("(skip-backup)")
			}
			modInfo := ""
			if op.File != nil {
				winner := op.File.Winner()
				modInfo = formatModInfo(winner)
			} else if op.OverrideID.Valid {
				modInfo = style.Subtle.Render("(override)")
			}
			style.Printf("  %s %-50s %s %s\n", symbol, op.DestPath, modInfo, detail)
			countWrite++
			if op.File != nil && len(op.File.Conflicts) > 1 {
				countConflict++
				if showConflicts {
					printConflictLosers(op.File)
				}
			}

		case planner.PlanOpOverwrite:
			symbol := style.Changed.Render("~")
			detail := ""
			if op.NeedsBackup {
				detail = style.Subtle.Render("(backup needed)")
				countBackup++
			} else if op.SkipBackup {
				detail = style.Subtle.Render("(skip-backup)")
			}
			modInfo := ""
			if op.File != nil {
				winner := op.File.Winner()
				modInfo = formatModInfo(winner)
			} else if op.OverrideID.Valid {
				modInfo = style.Subtle.Render("(override)")
			}
			style.Printf("  %s %-50s %s %s\n", symbol, op.DestPath, modInfo, detail)
			countOverwrite++
			if op.File != nil && len(op.File.Conflicts) > 1 {
				countConflict++
				if showConflicts {
					printConflictLosers(op.File)
				}
			}

		case planner.PlanOpRemove:
			style.Printf("  %s %s\n", style.Removed.Render("-"), op.DestPath)
			countRemove++

		case planner.PlanOpRestoreBackup:
			style.Printf("  %s %s\n", style.Restored.Render("↩"), op.DestPath)
			countRestore++
		}
	}

	style.Println()

	// Summary line
	parts := []string{}
	if countWrite > 0 {
		parts = append(parts, fmt.Sprintf("%d write", countWrite))
	}
	if countOverwrite > 0 {
		parts = append(parts, fmt.Sprintf("%d overwrite", countOverwrite))
	}
	if countRemove > 0 {
		parts = append(parts, fmt.Sprintf("%d remove", countRemove))
	}
	if countRestore > 0 {
		parts = append(parts, fmt.Sprintf("%d restore", countRestore))
	}
	total := countWrite + countOverwrite + countRemove + countRestore
	style.Printf("  %d operations: %s\n", total, strings.Join(parts, ", "))

	if countConflict > 0 {
		style.Printf("  %d conflict(s) resolved\n", countConflict)
	}
	if countBackup > 0 {
		style.Printf("  %d file(s) will be backed up\n", countBackup)
	}
	if len(plan.Warnings) > 0 {
		style.Println()
		for _, w := range plan.Warnings {
			style.Println(style.Warning.Render("  ⚠  " + w))
		}
	}
}

// formatModInfo returns a subtle string showing mod page name and version
// for dry-run output.
func formatModInfo(c planner.Conflict) string {
	s := c.ModPageName
	if c.VersionString != "" {
		s += " " + c.VersionString
	}
	return style.Subtle.Render(s)
}

// printConflictLosers prints the losing mods for a conflicted path, indented
// and muted, sorted by priority descending (same order as Conflicts slice).
func printConflictLosers(pf *planner.PlanFile) {
	for _, c := range pf.Conflicts {
		if c.Won {
			continue
		}
		style.Println(style.Subtle.Render(fmt.Sprintf("      ✗ %s", formatModInfoRaw(c))))
	}
}

// formatModInfoRaw returns the unstyled mod name + version string.
func formatModInfoRaw(c planner.Conflict) string {
	if c.VersionString != "" {
		return c.ModPageName + " " + c.VersionString
	}
	return c.ModPageName
}
