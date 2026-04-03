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
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/extractor"
	"github.com/mfinelli/modctl/internal/planner"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		boldStyle := lipgloss.NewStyle().Bold(true)
		addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		removeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		hunkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

		ctx := cmd.Context()
		relpath := filepath.Clean(args[0])

		if filepath.IsAbs(relpath) {
			return fmt.Errorf("path must be relative, got %q", relpath)
		}

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

		if profilesPreviewGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesPreviewGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesPreviewGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesPreviewProfile)
		if err != nil {
			return err
		}

		targetName := profilesPreviewTarget
		if targetName == "" {
			targetName = "game_dir"
		}

		target, err := q.GetTargetByName(ctx, dbq.GetTargetByNameParams{
			GameInstallID: gi.ID,
			Name:          targetName,
		})
		if err != nil {
			return fmt.Errorf("resolve target %q: %w", targetName, err)
		}

		// Build apply plan to find the winner for this path
		plan, err := planner.BuildApplyPlan(ctx, q, gi.ID, p.ID, target, false)
		if err != nil {
			var uninvErr *planner.UninventoriedArchiveError
			if errors.As(err, &uninvErr) {
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
			ArchivesDir:  viper.GetString("archives_dir"),
			BackupsDir:   viper.GetString("backups_dir"),
			OverridesDir: viper.GetString("overrides_dir"),
			TmpDir:       viper.GetString("tmp_dir"),
		}

		ex := extractor.Extractor{
			BsdtarPath: viper.GetString("bsdtar"),
			BlobStore:  bs,
			StagingDir: viper.GetString("tmp_dir"),
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
		if _, exists := diskStat(absPath); exists {
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

		fmt.Println(boldStyle.Render(fmt.Sprintf(
			"Apply preview for %q in profile %q:", relpath, p.Name,
		)))
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  winner: %s", modInfo)))
		fmt.Println()

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
			fmt.Println(subtleStyle.Render("  no differences (on-disk file matches what apply would write)"))
			return nil
		}

		for _, line := range strings.Split(diff, "\n") {
			switch {
			case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
				fmt.Println(boldStyle.Render(line))
			case strings.HasPrefix(line, "@@"):
				fmt.Println(hunkStyle.Render(line))
			case strings.HasPrefix(line, "+"):
				fmt.Println(addStyle.Render(line))
			case strings.HasPrefix(line, "-"):
				fmt.Println(removeStyle.Render(line))
			default:
				fmt.Println(subtleStyle.Render(line))
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
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesPreviewCmd.Flags().StringVarP(&profilesPreviewProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesPreviewCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
	profilesPreviewCmd.Flags().StringVarP(&profilesPreviewTarget, "target", "t", "",
		"Install target (default: game_dir)")
	profilesPreviewCmd.RegisterFlagCompletionFunc("target",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.TargetNames(cmd, toComplete)
		})

	profilesPreviewCmd.Flags().BoolVar(&profilesPreviewForce, "force", false,
		"Diff binary files without refusing")
}
