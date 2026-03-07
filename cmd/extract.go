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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/archivescanner"
	"github.com/mfinelli/modctl/internal/restore"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	extractGame      string
	extractMod       string
	extractFile      string
	extractVersion   string
	extractOutputDir string
	extractOverwrite bool
)

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract mod archives from a modctl export bundle",
	Long: `Extract mod archives from a modctl export bundle.

Without --mod, lists all mods in the bundle grouped by game and mod page.

With --mod, extracts the matching mod archive(s) to the output directory.
The --file and --version flags narrow the selection further; if omitted and
only one option exists it is selected automatically, otherwise the command
lists the available options and exits.

For full bundles, --game is required when extracting (--mod). It has no
effect on game-scoped bundles.

Extracted files are named using the original filename if available, otherwise
the archive format is detected and the file is named by its sha256 prefix.

Examples:
  modctl extract ./backup.tar.zst
  modctl extract ./backup.tar.zst --game steam:1091500 --mod "Unofficial Skyrim Patch"
  modctl extract ./backup.tar.zst --mod "Cyber Engine Tweaks" --file "Main File" --version "1.2.3"
  modctl extract ./game-export.tar.zst --mod "Appearance Menu Mod" --output-dir ~/mods/`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO extract....
		boldStyle := lipgloss.NewStyle().Bold(true)
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
		errStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		bundlePath := args[0]

		bundle, err := restore.OpenAndValidate(ctx, bundlePath)
		if err != nil {
			return fmt.Errorf("invalid bundle: %w", err)
		}
		defer bundle.Close()

		bq := dbq.New(bundle.BundleDB)
		isFull := bundle.Manifest.ExportKind == "full"

		// warn if --game passed on game-scoped bundle
		if !isFull && extractGame != "" {
			fmt.Println(warnStyle.Render("  ⚠ --game has no effect on a game-scoped bundle, ignoring"))
		}

		// list mode
		if extractMod == "" {
			return runExtractList(ctx, bq, bundle, isFull, boldStyle, subtleStyle)
		}

		// extraction mode
		if isFull && extractGame == "" {
			return fmt.Errorf("--game is required when extracting from a full bundle")
		}

		// resolve output dir
		outDir := extractOutputDir
		if outDir == "" {
			outDir = "."
		}
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}

		return runExtract(
			ctx, bq, bundle, isFull,
			boldStyle, subtleStyle, okStyle, warnStyle, errStyle,
			outDir,
		)
	},
}

func init() {
	rootCmd.AddCommand(extractCmd)

	// note: no completion here because we operate on the bundled db
	extractCmd.Flags().StringVarP(&extractGame, "game", "g", "",
		"Game install selector (required for extraction from full bundles)")
	extractCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		})

	extractCmd.Flags().StringVar(&extractMod, "mod", "",
		"Mod page name to extract (exact match)")
	extractCmd.Flags().StringVar(&extractFile, "file", "",
		"Mod file label to extract (exact match)")
	extractCmd.Flags().StringVar(&extractVersion, "version", "",
		"Mod file version to extract (exact match)")
	extractCmd.Flags().StringVarP(&extractOutputDir, "output-dir", "o", "",
		"Directory to write extracted files (default: current directory)")
	extractCmd.Flags().BoolVar(&extractOverwrite, "overwrite", false,
		"Overwrite existing files in the output directory")
}

// runExtractList prints all mods in the bundle grouped by game and mod page.
func runExtractList(
	ctx context.Context,
	bq *dbq.Queries,
	bundle *restore.Bundle,
	isFull bool,
	bold, subtle lipgloss.Style,
) error {
	games, err := bq.ExportGetGameInstalls(ctx)
	if err != nil {
		return fmt.Errorf("list games: %w", err)
	}

	for _, gi := range games {
		header := fmt.Sprintf("Mods in bundle (game: %s  %s:%s)",
			gi.DisplayName, gi.StoreID, gi.StoreGameID)
		fmt.Println(bold.Render(header))
		fmt.Println()

		pages, err := bq.ExportGetModPagesForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list mod pages: %w", err)
		}

		if len(pages) == 0 {
			fmt.Println(subtle.Render("  (no mods)"))
			fmt.Println()
			continue
		}

		for _, page := range pages {
			fmt.Printf("  Mod Page: %s\n", page.Name)

			files, err := bq.ExportGetModFilesForGameInstall(ctx, gi.ID)
			if err != nil {
				return fmt.Errorf("list mod files: %w", err)
			}

			for _, f := range files {
				if f.ModPageID != page.ID {
					continue
				}
				fmt.Printf("    File: %s\n", f.Label)

				versions, err := bq.ExportGetModFileVersionsForGameInstall(ctx, gi.ID)
				if err != nil {
					return fmt.Errorf("list mod file versions: %w", err)
				}

				for _, v := range versions {
					if v.ModFileID != f.ID {
						continue
					}
					verStr := subtle.Render("(no version)")
					if v.VersionString.Valid {
						verStr = v.VersionString.String
					}
					sha := v.ArchiveSha256[:16] + "..."
					line := fmt.Sprintf("      %s  %s", verStr, subtle.Render(sha))

					// nexus info
					if v.NexusFileID.Valid {
						line += "  " + subtle.Render(fmt.Sprintf("(nexus file_id=%d)", v.NexusFileID.Int64))
					}
					fmt.Println(line)
				}
			}
			fmt.Println()
		}
	}

	return nil
}

// runExtract extracts the selected mod archive(s) to the output directory.
func runExtract(
	ctx context.Context,
	bq *dbq.Queries,
	bundle *restore.Bundle,
	isFull bool,
	bold, subtle, ok, warn, errSty lipgloss.Style,
	outDir string,
) error {
	games, err := bq.ExportGetGameInstalls(ctx)
	if err != nil {
		return fmt.Errorf("list games: %w", err)
	}

	// filter to selected game if full bundle
	var targetGames []dbq.GameInstall
	for _, gi := range games {
		if isFull {
			sel := fmt.Sprintf("%s:%s", gi.StoreID, gi.StoreGameID)
			if sel != extractGame && gi.DisplayName != extractGame {
				continue
			}
		}
		targetGames = append(targetGames, gi)
	}

	if isFull && len(targetGames) == 0 {
		return fmt.Errorf("no game matching %q found in bundle", extractGame)
	}

	var extracted int

	for _, gi := range targetGames {
		pages, err := bq.ExportGetModPagesForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list mod pages: %w", err)
		}

		// find matching mod page (exact name match)
		var matchedPage *dbq.ModPage
		for i := range pages {
			if pages[i].Name == extractMod {
				matchedPage = &pages[i]
				break
			}
		}
		if matchedPage == nil {
			fmt.Println(errSty.Render(fmt.Sprintf(
				"no mod page matching %q found", extractMod,
			)))
			fmt.Println(subtle.Render("available mod pages:"))
			for _, p := range pages {
				fmt.Println(subtle.Render("  " + p.Name))
			}
			return fmt.Errorf("mod page not found")
		}

		// get mod files for this page
		allFiles, err := bq.ExportGetModFilesForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list mod files: %w", err)
		}
		var pageFiles []dbq.ModFile
		for _, f := range allFiles {
			if f.ModPageID == matchedPage.ID {
				pageFiles = append(pageFiles, f)
			}
		}

		// resolve file
		var matchedFile *dbq.ModFile
		if extractFile != "" {
			for i := range pageFiles {
				if pageFiles[i].Label == extractFile {
					matchedFile = &pageFiles[i]
					break
				}
			}
			if matchedFile == nil {
				fmt.Println(errSty.Render(fmt.Sprintf(
					"no file matching %q found for mod %q", extractFile, extractMod,
				)))
				fmt.Println(subtle.Render("available files:"))
				for _, f := range pageFiles {
					fmt.Println(subtle.Render("  " + f.Label))
				}
				return fmt.Errorf("mod file not found")
			}
		} else if len(pageFiles) == 1 {
			matchedFile = &pageFiles[0]
		} else {
			fmt.Println(errSty.Render(fmt.Sprintf(
				"multiple files found for mod %q, use --file to select one", extractMod,
			)))
			for _, f := range pageFiles {
				fmt.Println(subtle.Render("  " + f.Label))
			}
			return fmt.Errorf("ambiguous mod file selection")
		}

		// get versions for this file
		allVersions, err := bq.ExportGetModFileVersionsForGameInstall(ctx, gi.ID)
		if err != nil {
			return fmt.Errorf("list mod file versions: %w", err)
		}
		var fileVersions []dbq.ModFileVersion
		for _, v := range allVersions {
			if v.ModFileID == matchedFile.ID {
				fileVersions = append(fileVersions, v)
			}
		}

		// resolve version
		var matchedVersion *dbq.ModFileVersion
		if extractVersion != "" {
			for i := range fileVersions {
				if fileVersions[i].VersionString.Valid &&
					fileVersions[i].VersionString.String == extractVersion {
					matchedVersion = &fileVersions[i]
					break
				}
			}
			if matchedVersion == nil {
				fmt.Println(errSty.Render(fmt.Sprintf(
					"no version matching %q found for mod %q file %q",
					extractVersion, extractMod, matchedFile.Label,
				)))
				fmt.Println(subtle.Render("available versions:"))
				for _, v := range fileVersions {
					if v.VersionString.Valid {
						fmt.Println(subtle.Render("  " + v.VersionString.String))
					} else {
						fmt.Println(subtle.Render("  (no version) " + v.ArchiveSha256[:16] + "..."))
					}
				}
				return fmt.Errorf("mod file version not found")
			}
		} else if len(fileVersions) == 1 {
			matchedVersion = &fileVersions[0]
		} else {
			fmt.Println(errSty.Render(fmt.Sprintf(
				"multiple versions found for mod %q file %q, use --version to select one",
				extractMod, matchedFile.Label,
			)))
			for _, v := range fileVersions {
				if v.VersionString.Valid {
					fmt.Println(subtle.Render("  " + v.VersionString.String))
				} else {
					fmt.Println(subtle.Render("  (no version) " + v.ArchiveSha256[:16] + "..."))
				}
			}
			return fmt.Errorf("ambiguous version selection")
		}

		// extract the blob
		if err := extractBlob(
			ctx,
			bundle,
			matchedVersion,
			matchedPage,
			matchedFile,
			outDir,
			bold, subtle, ok, warn,
		); err != nil {
			return err
		}
		extracted++
	}

	if extracted == 0 {
		return fmt.Errorf("nothing extracted")
	}

	return nil
}

// extractBlob copies a single blob from the bundle temp dir to the output dir.
func extractBlob(
	ctx context.Context,
	bundle *restore.Bundle,
	version *dbq.ModFileVersion,
	page *dbq.ModPage,
	file *dbq.ModFile,
	outDir string,
	bold, subtle, ok, warn lipgloss.Style,
) error {
	sha := version.ArchiveSha256
	fan := sha[:2]
	blobPath := filepath.Join(bundle.BundleDir, "archives", fan, sha)

	// verify blob hash before extracting
	actual, err := hashFile(blobPath)
	if err != nil {
		return fmt.Errorf("hash blob %s: %w", sha[:16], err)
	}
	if actual != sha {
		return fmt.Errorf(
			"blob integrity check failed: expected %s got %s",
			sha, actual,
		)
	}

	// determine output filename
	outName, err := resolveOutputName(ctx, version, blobPath)
	if err != nil {
		return fmt.Errorf("resolve output filename: %w", err)
	}

	outPath := filepath.Join(outDir, outName)

	// check for existing file
	if _, err := os.Stat(outPath); err == nil {
		if !extractOverwrite {
			fmt.Println(warn.Render(fmt.Sprintf(
				"  ⚠ skipping %s (already exists, use --overwrite to replace)",
				outName,
			)))
			return nil
		}
	}

	// copy blob to output
	if err := copyFile(blobPath, outPath); err != nil {
		return fmt.Errorf("copy blob to output: %w", err)
	}

	fmt.Println(ok.Render(fmt.Sprintf("  ✓ extracted: %s", outName)))

	// print nexus info if available
	if version.NexusFileID.Valid {
		nexusLine := fmt.Sprintf("    nexus file_id=%d", version.NexusFileID.Int64)
		// get mod page nexus info
		if page.NexusModID.Valid && page.NexusGameDomain.Valid {
			nexusLine += fmt.Sprintf("  mod_id=%d  game=%s",
				page.NexusModID.Int64, page.NexusGameDomain.String)
			nexusLine += fmt.Sprintf("\n    url: https://www.nexusmods.com/%s/mods/%d",
				page.NexusGameDomain.String, page.NexusModID.Int64)
		}
		fmt.Println(subtle.Render(nexusLine))
	}

	return nil
}

// resolveOutputName determines the output filename for a blob.
// Uses original_name if available, otherwise detects the archive format
// via bsdtar and names it by sha256 prefix + extension.
func resolveOutputName(
	ctx context.Context,
	version *dbq.ModFileVersion,
	blobPath string,
) (string, error) {
	if version.OriginalName.Valid && version.OriginalName.String != "" {
		return version.OriginalName.String, nil
	}

	// detect format via bsdtar trailer
	ext, err := detectArchiveExt(ctx, blobPath)
	if err != nil {
		// fallback: just use sha prefix with no extension
		return version.ArchiveSha256[:16], nil
	}

	return version.ArchiveSha256[:16] + ext, nil
}

// detectArchiveExt runs bsdtar -tvvf and parses the summary trailer to
// determine the archive format and compression, returning a file extension.
func detectArchiveExt(ctx context.Context, blobPath string) (string, error) {
	format, compression, err := archivescanner.DetectFormat(
		ctx, viper.GetString("bsdtar"), blobPath,
	)
	if err != nil {
		return "", err
	}
	return archiveFormatToExt(format, compression), nil
}

// archiveFormatToExt maps bsdtar format/compression strings to file extensions.
func archiveFormatToExt(format, compression string) string {
	format = strings.ToLower(format)
	compression = strings.ToLower(compression)

	switch {
	case strings.Contains(format, "zip"):
		return ".zip"
	case strings.Contains(format, "rar"):
		return ".rar"
	case strings.Contains(format, "7-zip"):
		return ".7z"
	case strings.Contains(format, "ustar") || strings.Contains(format, "tar"):
		switch compression {
		case "gzip":
			return ".tar.gz"
		case "bzip2":
			return ".tar.bz2"
		case "xz":
			return ".tar.xz"
		case "zstd":
			return ".tar.zst"
		default:
			return ".tar"
		}
	default:
		return ".tar.gz" // we wrap unknowns in tar.gz on import
	}
}

// TODO: is this the ...sixth copy ?!
// hashFile hashes a file and returns the hex sha256.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// TODO: this is at least the second version
// copyFile copies src to dst atomically.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}

	success := false
	defer func() {
		out.Close()
		if !success {
			os.Remove(dst)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}

	success = true
	return nil
}
