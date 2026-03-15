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
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	modsRemoveGame        string
	modsRemoveFileVersion string
	modsRemoveForce       bool
)

var modsRemoveCmd = &cobra.Command{
	Use:   "remove <mod-page>",
	Short: "Remove a mod or mod file version from the database",
	Long: `Remove a mod or mod file version from the database.

Without --file-version, removes the entire mod page and all files and
versions under it.

With --file-version, removes only that specific version. If removing the
version leaves the parent file empty, the file is also removed. If that
leaves the mod page empty, the page is also removed.

Blobs (archive files) are not removed immediately. Run 'modctl gc' to
reclaim disk space after removing mods.

If any mod file version to be removed is currently referenced by a profile,
the command will refuse unless --force is passed. With --force, the profile
items are removed automatically via cascade.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completion.ModPageIDs(cmd, toComplete)
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

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
		if modsRemoveGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			modsRemoveGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, modsRemoveGame)
		if err != nil {
			return err
		}

		page, err := internal.ResolveModPageArg(ctx, q, gi, args[0])
		if err != nil {
			return err
		}

		// Version-scoped removal
		if modsRemoveFileVersion != "" {
			mfv, err := internal.ResolveModFileVersionArg(ctx, q, gi, modsRemoveFileVersion)
			if err != nil {
				return fmt.Errorf("resolve mod file version: %w", err)
			}

			ver, err := q.GetModFileVersionWithParentIDs(ctx, mfv.ID)
			if err != nil {
				return fmt.Errorf("get mod file version: %w", err)
			}

			// Chain validation: version must belong to the resolved page
			if ver.ModPageID != page.ID {
				return fmt.Errorf(
					"mod file version %d belongs to mod page %q (id=%d), not %q (id=%d)",
					mfv.ID,
					ver.ModPageName, ver.ModPageID,
					page.Name, page.ID,
				)
			}

			// Check profile references
			profiles, err := q.GetModFileVersionProfiles(ctx, dbq.GetModFileVersionProfilesParams{
				ModFileVersionID: mfv.ID,
				GameInstallID:    gi.ID,
			})
			if err != nil {
				return fmt.Errorf("check profile references: %w", err)
			}
			if len(profiles) > 0 && !modsRemoveForce {
				var b strings.Builder
				fmt.Fprintf(&b, "mod file version %d is referenced by %d profile(s):\n\n",
					mfv.ID, len(profiles))
				for _, p := range profiles {
					enabled := "disabled"
					if p.Enabled != 0 {
						enabled = "enabled"
					}
					fmt.Fprintf(&b, "  %-30s [priority %d, %s]\n",
						p.ProfileName, p.Priority, enabled)
				}
				fmt.Fprintf(&b, "\npass --force to remove anyway (profile items will be deleted)")
				return fmt.Errorf("%s", b.String())
			}

			if len(profiles) > 0 {
				for _, p := range profiles {
					fmt.Println(warnStyle.Render(fmt.Sprintf(
						"  ⚠ removing from profile %q (priority %d)", p.ProfileName, p.Priority)))
				}
			}

			// Delete the version
			if err := q.DeleteModFileVersion(ctx, mfv.ID); err != nil {
				return fmt.Errorf("delete mod file version: %w", err)
			}
			fmt.Println(subtleStyle.Render(fmt.Sprintf(
				"  removed version %d (%s / %s)", mfv.ID, ver.ModPageName, ver.FileLabel)))

			// Cascade up: remove empty parent file
			versionCount, err := q.CountModFileVersionsForFile(ctx, ver.ModFileID)
			if err != nil {
				return fmt.Errorf("count remaining versions: %w", err)
			}
			if versionCount == 0 {
				if err := q.DeleteModFile(ctx, ver.ModFileID); err != nil {
					return fmt.Errorf("delete empty mod file: %w", err)
				}
				fmt.Println(subtleStyle.Render(fmt.Sprintf(
					"  removed empty file %q", ver.FileLabel)))

				// Cascade up: remove empty parent page
				fileCount, err := q.CountModFilesForPage(ctx, ver.ModPageID)
				if err != nil {
					return fmt.Errorf("count remaining files: %w", err)
				}
				if fileCount == 0 {
					if err := q.DeleteModPage(ctx, ver.ModPageID); err != nil {
						return fmt.Errorf("delete empty mod page: %w", err)
					}
					fmt.Println(subtleStyle.Render(fmt.Sprintf(
						"  removed empty mod page %q", ver.ModPageName)))
				}
			}

			fmt.Println("Removed mod file version " + strconv.FormatInt(mfv.ID, 10) +
				subtleStyle.Render("  (run 'modctl gc' to reclaim disk space)"))
			return nil
		}

		// Page-scoped removal: check all versions under this page
		profiles, err := q.GetProfilesReferencingModPage(ctx, page.ID)
		if err != nil {
			return fmt.Errorf("check profile references: %w", err)
		}
		if len(profiles) > 0 && !modsRemoveForce {
			var b strings.Builder
			fmt.Fprintf(&b, "mod page %q (id=%d) has versions referenced by %d profile(s):\n\n",
				page.Name, page.ID, len(profiles))
			for _, p := range profiles {
				fmt.Fprintf(&b, "  %s\n", p.ProfileName)
			}
			fmt.Fprintf(&b, "\npass --force to remove anyway (profile items will be deleted)")
			return fmt.Errorf("%s", b.String())
		}

		if len(profiles) > 0 {
			for _, p := range profiles {
				fmt.Println(warnStyle.Render(fmt.Sprintf(
					"  ⚠ removing mod from profile %q", p.ProfileName)))
			}
		}

		// FK CASCADE handles mod_files and mod_file_versions
		if err := q.DeleteModPage(ctx, page.ID); err != nil {
			return fmt.Errorf("delete mod page: %w", err)
		}

		fmt.Printf("Removed mod page %q (id=%d)", page.Name, page.ID)
		fmt.Println(subtleStyle.Render("  (run 'modctl gc' to reclaim disk space)"))

		return nil
	},
}

func init() {
	modsCmd.AddCommand(modsRemoveCmd)

	modsRemoveCmd.Flags().StringVarP(&modsRemoveGame, "game", "g", "",
		"Override the currently active game")
	modsRemoveCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	modsRemoveCmd.Flags().StringVar(&modsRemoveFileVersion, "file-version", "",
		"Remove a specific mod file version instead of the entire mod page")
	modsRemoveCmd.RegisterFlagCompletionFunc("file-version",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			modPageArg := ""
			if len(args) > 0 {
				modPageArg = strings.TrimSpace(args[0])
			}
			return completion.ModFileVersionIDsForPage(cmd, modPageArg, toComplete)
		})

	modsRemoveCmd.Flags().BoolVar(&modsRemoveForce, "force", false,
		"Remove even if referenced by profiles (profile items will be deleted)")
}
