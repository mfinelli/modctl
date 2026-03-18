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

	"github.com/charmbracelet/lipgloss"
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
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	applyGame        string
	applyProfile     string
	applyDryRun      bool
	applySkipRecheck bool
	applyKeepStaging bool
	applyVerbose     bool
	applyForce       bool
	applyAbort       bool
	applyPruneDirs   bool
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
				fmt.Println("Operation marked as failed. Run 'modctl apply' to reapply or 'modctl unapply' to clean up.")
				return nil
			}
		}

		// Acquire per-game lock to prevent concurrent apply/unapply
		unlock, err := lock.GameInstall(viper.GetString("locks_dir"), gi.ID)
		if err != nil {
			return err
		}
		defer unlock()

		plan, err := planner.BuildApplyPlan(ctx, q, gi.ID, p.ID, applySkipRecheck)
		if err != nil {
			var uninvErr *planner.UninventoriedArchiveError
			if errors.As(err, &uninvErr) {
				return fmt.Errorf("%w\nrun 'modctl mods scan-inventory' to fix", uninvErr)
			}
			return fmt.Errorf("build plan: %w", err)
		}

		// TODO extract these styles somewhere
		boldStyle := lipgloss.NewStyle().Bold(true)
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		cyanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))

		// Dry-run output
		if applyDryRun {
			printApplyPlan(plan, p.Name, gi.DisplayName, boldStyle, subtleStyle, warnStyle, greenStyle, redStyle, yellowStyle, cyanStyle)
			return nil
		}

		// Real apply
		fmt.Println(boldStyle.Render(fmt.Sprintf("Applying %q → %s", p.Name, gi.DisplayName)))
		fmt.Println()

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
					// PatchBaseArchives below — not grouped here.
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

		// Add patch base archives to the staging set if not already present.
		for _, sha := range plan.PatchBaseArchives {
			if _, ok := archiveMap[sha]; !ok {
				archiveMap[sha] = &archiveGroup{sha256: sha}
				archiveOrder = append(archiveOrder, sha)
			}
			// No ops added — archive is staged for patch base file access only.
		}

		// Counters for summary
		total := len(plan.Ops)
		current := 0
		var (
			countWrite     int
			countOverwrite int
			countRemove    int
			countRestore   int
			countBackedUp  int
			countFailed    int
		)

		// Helper to print progress
		width := len(strconv.Itoa(total))
		fmtCounter := fmt.Sprintf("[%%%dd/%%%dd]", width, width)

		// Print an initial line so \r updates have something to overwrite
		if !applyVerbose {
			fmt.Printf("  [%*d/%d] ...", width, 0, total)
		}

		printOp := func(symbol, path, detail string) {
			current++
			line := fmt.Sprintf("  "+fmtCounter+" %s %s", current, total, symbol, path)
			if detail != "" {
				line += subtleStyle.Render("  " + detail)
			}
			if applyVerbose {
				fmt.Println(line)
			} else {
				fmt.Printf("\r%-*s", 80, line)
			}
		}

		// markFailed marks the operation as failed and returns the error
		markFailed := func(err error) error {
			_ = q.FinishOperation(ctx, dbq.FinishOperationParams{
				Status:  "failed",
				Message: sql.NullString{String: err.Error(), Valid: true},
				ID:      op.ID,
			})
			return err
		}

		// Extract and deploy archives
		for _, sha := range archiveOrder {
			group := archiveMap[sha]
			stagingPath, err := ext.ExtractArchive(ctx, sha)
			if err != nil {
				return markFailed(fmt.Errorf("extract archive %.16s: %w", sha, err))
			}

			for _, planOp := range group.ops {
				symbol := greenStyle.Render("+")
				detail := ""
				if planOp.Kind == planner.PlanOpOverwrite {
					symbol = yellowStyle.Render("~")
				}
				if planOp.NeedsBackup {
					detail = "(backing up original)"
				}
				printOp(symbol, planOp.DestPath, detail)

				result, err := ext.DeployFile(ctx, db, q, planOp, stagingPath, plan.TargetRoot, gi.ID, plan.TargetID, p.ID, op.ID)
				if err != nil {
					countFailed++
					if applyVerbose {
						fmt.Println(warnStyle.Render(fmt.Sprintf("    ✗ %v", err)))
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
			symbol := greenStyle.Render("+")
			detail := "(override)"
			if planOp.Kind == planner.PlanOpOverwrite {
				symbol = yellowStyle.Render("~")
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
					countFailed++
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
				countFailed++
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
			printOp(redStyle.Render("-"), planOp.DestPath, "")
			if _, err := ext.RemoveFile(ctx, db, q, planOp, plan.TargetRoot, gi.ID, plan.TargetID, op.ID); err != nil {
				countFailed++
				return markFailed(fmt.Errorf("remove %q: %w", planOp.DestPath, err))
			}
			countRemove++
			removedPaths = append(removedPaths, planOp.DestPath)
		}

		// Restore ops
		for _, planOp := range restoreOps {
			printOp(cyanStyle.Render("↩"), planOp.DestPath, "")
			if _, err := ext.RestoreFile(ctx, db, q, planOp, plan.TargetRoot, gi.ID, plan.TargetID, op.ID); err != nil {
				countFailed++
				return markFailed(fmt.Errorf("restore %q: %w", planOp.DestPath, err))
			}
			countRestore++
		}

		// Prune empty directories if requested
		if applyPruneDirs {
			pruneWarnings := extractor.PruneDirs(plan.TargetRoot, removedPaths)
			plan.Warnings = append(plan.Warnings, pruneWarnings...)
		}

		// Clear the spinner line before printing summary
		if !applyVerbose {
			fmt.Print("\r" + strings.Repeat(" ", 80) + "\r")
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
				fmt.Println(warnStyle.Render(fmt.Sprintf("  warning: cleanup staging: %v", err)))
			}
		} else {
			fmt.Println(subtleStyle.Render(fmt.Sprintf("  staging kept at: %s", ext.StagingPathFor(""))))
		}

		// Summary
		elapsed := time.Since(mustParseTime(op.StartedAt))
		fmt.Println(boldStyle.Render(fmt.Sprintf("Apply complete in %.1fs", elapsed.Seconds())))
		if countWrite > 0 {
			fmt.Printf("  written:     %d\n", countWrite)
		}
		if countOverwrite > 0 {
			fmt.Printf("  overwritten: %d\n", countOverwrite)
		}
		if countRemove > 0 {
			fmt.Printf("  removed:     %d\n", countRemove)
		}
		if countRestore > 0 {
			fmt.Printf("  restored:    %d\n", countRestore)
		}
		if countBackedUp > 0 {
			fmt.Printf("  backed up:   %d\n", countBackedUp)
		}
		if len(plan.Warnings) > 0 {
			fmt.Println(warnStyle.Render(fmt.Sprintf("  warnings:    %d", len(plan.Warnings))))
			for _, w := range plan.Warnings {
				fmt.Println(warnStyle.Render("    ⚠  " + w))
			}
		}
		if countFailed > 0 {
			fmt.Println(warnStyle.Render(fmt.Sprintf("  failed:      %d", countFailed)))
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
}

// printApplyPlan renders the dry-run plan output
func printApplyPlan(
	plan planner.Plan,
	profileName string,
	gameName string,
	bold, subtle, warn, green, red, yellow, cyan lipgloss.Style,
) {
	fmt.Println(bold.Render(fmt.Sprintf("Apply plan for %q → %s", profileName, gameName)))
	fmt.Println()

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
			symbol := green.Render("+")
			detail := ""
			if op.NeedsBackup {
				detail = subtle.Render("(backup needed)")
				countBackup++
			}
			winner := op.File.Winner()
			modInfo := formatModInfo(winner)
			fmt.Printf("  %s %-50s %s %s\n", symbol, op.DestPath, modInfo, detail)
			countWrite++
			if len(op.File.Conflicts) > 1 {
				countConflict++
			}

		case planner.PlanOpOverwrite:
			symbol := yellow.Render("~")
			detail := ""
			if op.NeedsBackup {
				detail = subtle.Render("(backup needed)")
				countBackup++
			}
			winner := op.File.Winner()
			modInfo := formatModInfo(winner)
			fmt.Printf("  %s %-50s %s %s\n", symbol, op.DestPath, modInfo, detail)
			countOverwrite++
			if len(op.File.Conflicts) > 1 {
				countConflict++
			}

		case planner.PlanOpRemove:
			fmt.Printf("  %s %s\n", red.Render("-"), op.DestPath)
			countRemove++

		case planner.PlanOpRestoreBackup:
			fmt.Printf("  %s %s\n", cyan.Render("↩"), op.DestPath)
			countRestore++
		}
	}

	fmt.Println()

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
	fmt.Printf("  %d operations: %s\n", total, strings.Join(parts, ", "))

	if countConflict > 0 {
		fmt.Printf("  %d conflict(s) resolved\n", countConflict)
	}
	if countBackup > 0 {
		fmt.Printf("  %d file(s) will be backed up\n", countBackup)
	}
	if len(plan.Warnings) > 0 {
		fmt.Println()
		for _, w := range plan.Warnings {
			fmt.Println(warn.Render("  ⚠  " + w))
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
	return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(s)
}
