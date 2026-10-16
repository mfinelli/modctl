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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/extractor"
	"github.com/mfinelli/modctl/internal/fsutil"
	"github.com/mfinelli/modctl/internal/planner"
	"github.com/mfinelli/modctl/internal/style"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"
)

var (
	profilesPreviewGame    string
	profilesPreviewProfile string
	profilesPreviewTarget  string
	profilesPreviewForce   bool
)

var profilesPreviewCmd = &cobra.Command{
	Use:   "preview <path>",
	Short: "Show a diff between the on-disk file and what apply would write",
	Long: `Show a unified diff between the current on-disk file and what the active
profile would write at that path if apply were run.

The path is relative to the target root (default: game_dir). Use --target
to preview a path in a different install target.

The diff shows what apply would change:
  - lines removed from the on-disk file are shown in red
  - lines that apply would write are shown in green

Errors if no mod in the active profile provides this path. Note that this
command requires archive extraction which may be slow for large archives.

Binary files are detected automatically and refused unless --force is passed.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		relpath, err := argresolver.Relpath(args[0])
		if err != nil {
			return err
		}

		db, err := openDB(ctx)
		if err != nil {
			return err
		}
		defer db.Close()

		q := dbq.New(db)

		gi, err := gameInstall(ctx, q, profilesPreviewGame)
		if err != nil {
			return err
		}

		p, err := argresolver.Profile(ctx, q, gi, profilesPreviewProfile)
		if err != nil {
			return err
		}

		targets, err := q.ListTargetsForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list targets: %w", err)
		}
		target, err := internal.ResolveTarget(targets, profilesPreviewTarget)
		if err != nil {
			return err
		}

		// Build apply plan to find the winner for this path
		plan, err := planner.BuildApplyPlan(ctx, q, gi.ID, p.ID, target, false)
		if err != nil {
			if uninvErr, ok := errors.AsType[*planner.UninventoriedArchiveError](err); ok {
				return fmt.Errorf("%w\nrun 'modctl mods scan-inventory' to fix", uninvErr)
			}
			return fmt.Errorf("build apply plan: %w", err)
		}

		// Find the op for this specific path
		var winnerOp *planner.PlanOp
		for i := range plan.Ops {
			if plan.Ops[i].DestPath == relpath {
				op := plan.Ops[i]
				winnerOp = &op
				break
			}
		}

		if winnerOp == nil || winnerOp.File == nil || len(winnerOp.File.Conflicts) == 0 {
			return fmt.Errorf("no mod in the active profile provides %q", relpath)
		}

		winner := winnerOp.File.Winner()

		bs := blobstore.Store{
			ArchivesDir:  cfg.ArchivesDir,
			BackupsDir:   cfg.BackupsDir,
			OverridesDir: cfg.OverridesDir,
			TmpDir:       cfg.TmpDir,
		}

		ex := extractor.Extractor{
			BsdtarPath: cfg.Bsdtar,
			BlobStore:  bs,
			StagingDir: cfg.TmpDir,
		}

		stagingDir, err := ex.ExtractArchive(ctx, winner.Entry.ArchiveSha256)
		if err != nil {
			return fmt.Errorf("extract archive: %w", err)
		}
		defer os.RemoveAll(stagingDir)

		stagedPath := filepath.Join(stagingDir, winner.Entry.SourcePath)
		incomingData, err := os.ReadFile(stagedPath)
		if err != nil {
			return fmt.Errorf("read staged file: %w", err)
		}

		if internal.IsBinaryContent(incomingData) && !profilesPreviewForce {
			return fmt.Errorf(
				"incoming file for %q appears to be binary; pass --force to diff anyway",
				relpath,
			)
		}

		absPath := filepath.Join(target.RootPath, relpath)
		var onDiskData []byte
		exists, err := fsutil.Exists(absPath)
		if err != nil {
			return fmt.Errorf("stat on-disk file: %w", err)
		}
		if exists {
			onDiskData, err = os.ReadFile(absPath)
			if err != nil {
				return fmt.Errorf("read on-disk file: %w", err)
			}
			if internal.IsBinaryContent(onDiskData) && !profilesPreviewForce {
				return fmt.Errorf(
					"on-disk file %q appears to be binary; pass --force to diff anyway",
					relpath,
				)
			}
		}

		modInfo := winner.ModPageName
		if winner.VersionString != "" {
			modInfo += " " + winner.VersionString
		}

		style.Println(style.Bold.Render(fmt.Sprintf(
			"Apply preview for %q in profile %q:", relpath, p.Name,
		)))
		style.Println(style.Subtle.Render(fmt.Sprintf("  winner: %s", modInfo)))
		style.Println()

		diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(string(onDiskData)),
			B:        difflib.SplitLines(string(incomingData)),
			FromFile: relpath + " (on disk)",
			ToFile:   relpath + " (incoming)",
			Context:  3,
		})
		if err != nil {
			return fmt.Errorf("generate diff: %w", err)
		}

		if diff == "" {
			style.Println(style.Subtle.Render("  no differences (on-disk file matches what apply would write)"))
			return nil
		}

		for line := range strings.SplitSeq(diff, "\n") {
			switch {
			case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
				style.Println(style.Bold.Render(line))
			case strings.HasPrefix(line, "@@"):
				style.Println(style.Hunk.Render(line))
			case strings.HasPrefix(line, "+"):
				style.Println(style.Added.Render(line))
			case strings.HasPrefix(line, "-"):
				style.Println(style.Removed.Render(line))
			default:
				style.Println(style.Subtle.Render(line))
			}
		}

		return nil
	},
}

func init() {
	profilesCmd.AddCommand(profilesPreviewCmd)

	profilesPreviewCmd.Flags().StringVarP(&profilesPreviewGame, "game", "g", "",
		"Override the currently active game")
	profilesPreviewCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().GameInstallSelectors(cmd, toComplete)
		})
	profilesPreviewCmd.Flags().StringVarP(&profilesPreviewProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesPreviewCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().ProfileNames(cmd, toComplete)
		})
	profilesPreviewCmd.Flags().StringVarP(&profilesPreviewTarget, "target", "t", "",
		"Install target (default: game_dir)")
	profilesPreviewCmd.RegisterFlagCompletionFunc("target",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completer().TargetNames(cmd, toComplete)
		})

	profilesPreviewCmd.Flags().BoolVar(&profilesPreviewForce, "force", false,
		"Diff binary files without refusing")
}
