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
	"github.com/mfinelli/modctl/internal/patchapply"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	profilesOverridesPatchPreviewGame    string
	profilesOverridesPatchPreviewProfile string
)

var profilesOverridesPatchPreviewCmd = &cobra.Command{
	Use:   "preview <path>",
	Short: "Preview the result of applying patch entries to a file",
	Long: `Preview the result of applying the current patch entries to the base file.

Extracts the base file from the current winning mod's archive, applies all
patch entries in memory, and displays a unified diff of the changes.

If no mod provides this path the patch is applied against an empty document
and the full result is shown.

Note: requires archive extraction which may be slow for large archives.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract these somewhere
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		boldStyle := lipgloss.NewStyle().Bold(true)
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
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

		if profilesOverridesPatchPreviewGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOverridesPatchPreviewGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, profilesOverridesPatchPreviewGame)
		if err != nil {
			return err
		}

		p, err := argresolver.ResolveProfileArg(ctx, q, &gi, profilesOverridesPatchPreviewProfile)
		if err != nil {
			return err
		}

		target, err := q.GetTargetByName(ctx, dbq.GetTargetByNameParams{
			GameInstallID: gi.ID,
			Name:          "game_dir",
		})
		if err != nil {
			return fmt.Errorf("resolve game_dir target: %w", err)
		}

		override, err := q.GetOverride(ctx, dbq.GetOverrideParams{
			ProfileID: p.ID,
			TargetID:  target.ID,
			Relpath:   relpath,
		})
		if err != nil {
			return fmt.Errorf("no patch override found for %q in profile %q", relpath, p.Name)
		}

		if override.OverrideType == "full_file" {
			return fmt.Errorf(
				"override for %q is a full-file override; use 'profiles overrides edit' to view it",
				relpath,
			)
		}

		entries, err := q.ListOverridePatchEntries(ctx, override.ID)
		if err != nil {
			return fmt.Errorf("list patch entries: %w", err)
		}

		if len(entries) == 0 {
			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"  no patch entries for %q in profile %q", relpath, p.Name,
			)))
			return nil
		}

		// convert DB entries to patchapply entries
		patchEntries := make([]patchapply.Entry, len(entries))
		for i, e := range entries {
			patchEntries[i] = patchapply.Entry{
				PatchType:  e.PatchType,
				EntryKey:   e.EntryKey,
				EntryValue: e.EntryValue.String,
			}
			if e.EntrySection.Valid {
				patchEntries[i].EntrySection = e.EntrySection.String
			}
		}

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

		// fetch base file content — extract from winning mod if available
		var baseContent []byte
		var baseLabel string
		noBase := false

		winner, winnerErr := q.GetCurrentWinnerForPath(ctx, dbq.GetCurrentWinnerForPathParams{
			ProfileID: p.ID,
			RawPath:   sql.NullString{String: relpath, Valid: true},
		})
		if winnerErr != nil {
			// no mod provides this path - apply against empty document
			noBase = true
			baseLabel = "(no base file)"
		} else {
			stagingDir, err := ex.ExtractArchive(ctx, winner.ArchiveSha256)
			if err != nil {
				return fmt.Errorf("extract archive: %w", err)
			}
			defer os.RemoveAll(stagingDir)

			if !winner.RawPath.Valid {
				return fmt.Errorf("inventory entry for %q has no path", relpath)
			}
			stagedFilePath := filepath.Join(stagingDir, winner.RawPath.String)
			baseContent, err = os.ReadFile(stagedFilePath)
			if err != nil {
				return fmt.Errorf("read base file from staging: %w", err)
			}
			baseLabel = relpath + " (base)"
		}

		// apply patches
		result, err := patchapply.Apply(patchEntries, baseContent)
		if err != nil {
			return fmt.Errorf("apply patch entries: %w", err)
		}

		// header
		fmt.Println(boldStyle.Render(fmt.Sprintf(
			"Patch preview for %q in profile %q (%s, %d entries):",
			relpath, p.Name, formatOverrideType(override.OverrideType), len(entries),
		)))
		fmt.Println()

		if noBase {
			fmt.Println(warnStyle.Render("  ⚠ no mod in this profile provides this path — showing result against empty document"))
			fmt.Println()
		}

		if result.Skipped > 0 {
			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"  %d entry(ies) had no effect (key not found)", result.Skipped,
			)))
			fmt.Println()
		}

		// generate and display unified diff
		diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(string(baseContent)),
			B:        difflib.SplitLines(string(result.Output)),
			FromFile: baseLabel,
			ToFile:   relpath + " (patched)",
			Context:  3,
		})
		if err != nil {
			return fmt.Errorf("generate diff: %w", err)
		}

		if diff == "" {
			fmt.Println(subtleStyle.Render("  no changes (patch entries produce identical output)"))
			return nil
		}

		// colorize diff output line by line
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
	profilesOverridesPatchCmd.AddCommand(profilesOverridesPatchPreviewCmd)

	profilesOverridesPatchPreviewCmd.Flags().StringVarP(&profilesOverridesPatchPreviewGame, "game", "g", "",
		"Override the currently active game")
	profilesOverridesPatchPreviewCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesOverridesPatchPreviewCmd.Flags().StringVarP(&profilesOverridesPatchPreviewProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesOverridesPatchPreviewCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})
}
