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
	"github.com/mfinelli/modctl/internal/planner"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	unapplyGame      string
	unapplyDryRun    bool
	unapplyPrintOps  bool
	unapplyForce     bool
	unapplyAbort     bool
	unapplyPruneDirs bool
)

var unapplyCmd = &cobra.Command{
	Use:   "unapply",
	Short: "Remove all tool-managed files from a game install",
	Long: `Remove all tool-managed files from a game install.

Removes all files that were deployed by the last apply operation and
restores any pre-existing files that were backed up. The game directory
is returned to its pre-mod state.

Use --dry-run to preview the plan without making any changes.`,
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
		if unapplyGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			unapplyGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, unapplyGame)
		if err != nil {
			return err
		}

		// Check for incomplete previous operation
		lastOp, err := q.GetLastOperationForGameInstall(ctx, gi.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check last operation: %w", err)
		}
		if err == nil && lastOp.Status == "running" {
			if !unapplyAbort && !unapplyForce {
				return fmt.Errorf(
					"last apply/unapply operation (#%d, started %s) did not complete\n"+
						"  your game directory may be in a partially applied state\n"+
						"  options:\n"+
						"    --abort    mark the operation as failed and exit\n"+
						"    --force    mark the operation as failed and start a fresh unapply",
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
			if unapplyAbort {
				style.Println("Operation marked as failed. Run 'modctl apply' to reapply or 'modctl unapply' to clean up.")
				return nil
			}
		}

		// Resolve applied profile name for display if available
		appliedState, err := q.GetGameInstallAppliedState(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("get applied state: %w", err)
		}
		var appliedProfileName string
		if appliedState.AppliedProfileID.Valid {
			profile, err := q.GetProfileByID(ctx, appliedState.AppliedProfileID.Int64)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("get applied profile: %w", err)
			}
			if err == nil {
				appliedProfileName = profile.Name
			}
		}

		// Acquire per-game lock to prevent concurrent apply/unapply
		unlock, err := lock.GameInstall(viper.GetString("locks_dir"), gi.ID)
		if err != nil {
			return err
		}
		defer unlock()

		// Build the unapply plans
		targets, err := q.ListTargetsForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list targets: %w", err)
		}
		if len(targets) == 0 {
			return fmt.Errorf("no targets found for game install %d", gi.ID)
		}

		var plans []planner.Plan
		for _, target := range targets {
			plan, err := planner.BuildUnapplyPlan(ctx, q, gi.ID, target)
			if err != nil {
				return fmt.Errorf("build unapply plan for target %q: %w", target.Name, err)
			}
			plans = append(plans, plan)
		}

		totalOps := 0
		for _, plan := range plans {
			totalOps += len(plan.Ops)
		}
		if totalOps == 0 {
			style.Println(style.Subtle.Render(
				"  nothing to unapply: no tool-managed files found"))
			return nil
		}

		// Only targets that have something to undo are worth mentioning
		var activeTargets []string
		for _, plan := range plans {
			if len(plan.Ops) > 0 {
				activeTargets = append(activeTargets, plan.TargetName)
			}
		}

		// Dry-run output
		if unapplyDryRun {
			for _, plan := range plans {
				if len(plan.Ops) == 0 {
					continue
				}
				printUnapplyPlan(plan, gi.DisplayName, appliedProfileName)
			}
			return nil
		}

		// Real unapply
		header := fmt.Sprintf("Unapplying %s", gi.DisplayName)
		if appliedProfileName != "" {
			header += fmt.Sprintf("  %s", style.Subtle.Render("(last applied: \""+appliedProfileName+"\")"))
		}
		targetLabel := "target"
		if len(activeTargets) > 1 {
			targetLabel = "targets"
		}
		style.Println(style.Bold.Render(header) +
			"  " + style.Subtle.Render(fmt.Sprintf("(%s: %s)", targetLabel, strings.Join(activeTargets, ", "))))
		style.Println()

		bs := blobstore.Store{
			ArchivesDir: viper.GetString("archives_dir"),
			BackupsDir:  viper.GetString("backups_dir"),
			TmpDir:      viper.GetString("tmp_dir"),
		}

		ext := extractor.Extractor{
			BsdtarPath: viper.GetString("bsdtar"),
			BlobStore:  bs,
			StagingDir: viper.GetString("tmp_dir"),
		}

		// Create operation record
		op, err := q.CreateOperation(ctx, dbq.CreateOperationParams{
			GameInstallID: gi.ID,
			ProfileID:     sql.NullInt64{},
			OpType:        "unapply",
		})
		if err != nil {
			return fmt.Errorf("create operation: %w", err)
		}

		// Counters
		total := totalOps
		current := 0
		var (
			countRemove  int
			countRestore int
		)

		width := len(strconv.Itoa(total))
		fmtCounter := fmt.Sprintf("[%%%dd/%%%dd]", width, width)

		// Print an initial line so \r updates have something to overwrite
		if !unapplyPrintOps {
			style.Printf("  [%*d/%d] ...", width, 0, total)
		}

		// With several targets in one run, say which one each op belongs to
		multiTarget := len(activeTargets) > 1
		var currentTarget string

		printOp := func(symbol, path string) {
			current++
			line := fmt.Sprintf("  "+fmtCounter+" %s %s", current, total, symbol, path)
			if multiTarget {
				line += style.Subtle.Render("  " + currentTarget)
			}
			if unapplyPrintOps {
				style.Println(line)
			} else {
				style.Printf("\r%-*s", 80, line)
			}
		}

		markFailed := func(err error) error {
			_ = q.FinishOperation(ctx, dbq.FinishOperationParams{
				Status:  "failed",
				Message: sql.NullString{String: err.Error(), Valid: true},
				ID:      op.ID,
			})
			return err
		}

		var allWarnings []string

		for _, plan := range plans {
			currentTarget = plan.TargetName
			if multiTarget && unapplyPrintOps && len(plan.Ops) > 0 {
				style.Println(style.Subtle.Render(fmt.Sprintf("  target: %s", plan.TargetName)))
			}

			var removedPaths []string

			for _, planOp := range plan.Ops {
				switch planOp.Kind {
				case planner.PlanOpRemove:
					printOp(style.Removed.Render("-"), planOp.DestPath)
					if _, err := ext.RemoveFile(ctx, db, q, planOp, plan.TargetRoot, gi.ID, plan.TargetID, op.ID); err != nil {
						return markFailed(fmt.Errorf("remove %q: %w", planOp.DestPath, err))
					}
					countRemove++
					removedPaths = append(removedPaths, planOp.DestPath)

				case planner.PlanOpRestoreBackup:
					printOp(style.Restored.Render("↩"), planOp.DestPath)
					if _, err := ext.RestoreFile(ctx, db, q, planOp, plan.TargetRoot, gi.ID, plan.TargetID, op.ID); err != nil {
						return markFailed(fmt.Errorf("restore %q: %w", planOp.DestPath, err))
					}
					countRestore++

				default:
					plan.Warnings = append(plan.Warnings,
						fmt.Sprintf("unexpected op kind %q for %q during unapply - skipped", planOp.Kind, planOp.DestPath))
				}
			}

			if unapplyPruneDirs {
				pruneWarnings := extractor.PruneDirs(plan.TargetRoot, removedPaths)
				allWarnings = append(allWarnings, pruneWarnings...)
			}
			allWarnings = append(allWarnings, plan.Warnings...)
		}

		// Clear spinner line
		if !unapplyPrintOps {
			style.Print("\r" + strings.Repeat(" ", 80) + "\r")
		}

		// Mark operation successful and clear applied state in one transaction
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

		if err := qtx.ClearGameInstallAppliedState(ctx, gi.ID); err != nil {
			return markFailed(fmt.Errorf("clear applied state: %w", err))
		}

		if err := tx.Commit(); err != nil {
			return markFailed(fmt.Errorf("commit final transaction: %w", err))
		}

		// Summary
		elapsed := time.Since(mustParseTime(op.StartedAt))
		style.Println(style.Bold.Render(fmt.Sprintf("Unapply complete in %.1fs", elapsed.Seconds())))
		if countRemove > 0 {
			style.Printf("  removed:   %d\n", countRemove)
		}
		if countRestore > 0 {
			style.Printf("  restored:  %d\n", countRestore)
		}
		if len(allWarnings) > 0 {
			style.Println(style.Warning.Render(fmt.Sprintf("  warnings:  %d", len(allWarnings))))
			for _, w := range allWarnings {
				style.Println(style.Warning.Render("    ⚠  " + w))
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(unapplyCmd)

	unapplyCmd.Flags().StringVarP(&unapplyGame, "game", "g", "",
		"Override the currently active game")
	unapplyCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	unapplyCmd.Flags().BoolVar(&unapplyDryRun, "dry-run", false,
		"Preview the plan without making any changes")
	unapplyCmd.Flags().BoolVar(&unapplyPrintOps, "print-ops", false,
		"Print each file operation on its own line instead of using a progress indicator")
	unapplyCmd.Flags().BoolVar(&unapplyForce, "force", false,
		"Mark any incomplete operation as failed and start fresh")
	unapplyCmd.Flags().BoolVar(&unapplyAbort, "abort", false,
		"Mark any incomplete operation as failed and exit")
	unapplyCmd.Flags().BoolVar(&unapplyPruneDirs, "prune-dirs", false,
		"Remove empty directories left behind after file removals")
}

// printUnapplyPlan renders the dry-run unapply plan output
func printUnapplyPlan(
	plan planner.Plan,
	gameName string,
	appliedProfileName string,
) {
	header := fmt.Sprintf("Unapply plan for %s", gameName)
	if appliedProfileName != "" {
		header += "  " + style.Subtle.Render("(last applied: \""+appliedProfileName+"\")")
	}
	header = style.Bold.Render(header) + "  " + style.Subtle.Render(fmt.Sprintf("(target: %s)", plan.TargetName))
	style.Println(header)
	style.Println()

	var countRemove, countRestore int

	for _, op := range plan.Ops {
		switch op.Kind {
		case planner.PlanOpRemove:
			style.Printf("  %s %s\n", style.Removed.Render("-"), op.DestPath)
			countRemove++
		case planner.PlanOpRestoreBackup:
			style.Printf("  %s %s\n", style.Restored.Render("↩"), op.DestPath)
			countRestore++
		}
	}

	style.Println()

	parts := []string{}
	if countRemove > 0 {
		parts = append(parts, fmt.Sprintf("%d remove", countRemove))
	}
	if countRestore > 0 {
		parts = append(parts, fmt.Sprintf("%d restore", countRestore))
	}
	total := countRemove + countRestore
	style.Printf("  %d operations: %s\n", total, strings.Join(parts, ", "))

	if len(plan.Warnings) > 0 {
		style.Println()
		for _, w := range plan.Warnings {
			style.Println(style.Warning.Render("  ⚠  " + w))
		}
	}
}
