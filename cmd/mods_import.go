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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/archivescanner"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/importer"
	"github.com/mfinelli/modctl/internal/nexus"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	modsImportGame          string
	modsImportName          string
	modsImportLabel         string
	modsImportFileVersion   string
	modsImportNexusUrl      string
	modsImportRm            bool
	modsImportListTimeout   int64
	modsImportPageID        int64
	modsImportSkipInventory bool
	modsImportSkipNexusLink bool
)

type prepareArchiveResult struct {
	PathToImport string
	Wrapped      bool
	WrappedFrom  string // e.g. "pdf" (without dot), or "" if unknown
	MemberName   string // tar member name (basename of input)
	Cleanup      func()
}

var modsImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import a mod archive into the blob store",
	Long: `Import a mod archive into modctl's content-addressed archive store.

This command copies the input file into modctl's archive store (deduplicated by
SHA-256) and records metadata in the database so it can be added to profiles
later.

By default, the input file is treated as an archive. modctl will validate the
file by listing its contents using bsdtar before importing it.

If the input file is not a supported archive format, modctl will wrap it into a
new .tar.gz archive containing the file, then import that archive. This ensures
that all stored archives can be inspected and extracted consistently later.

You can optionally attach Nexus metadata at import time using --nexus-url.

If --rm is provided, the original input file is deleted only after the archive
has been safely stored and the database has been updated successfully.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// TODO: extract these somewhere else
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

		err := internal.EnsureDBExists()
		if err != nil {
			return err
		}

		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()

		err = internal.MigrateDB(ctx, db)
		if err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		inputPath := args[0]
		archivesDir := viper.GetString("archives_dir")

		bs := blobstore.Store{
			ArchivesDir:  archivesDir,
			BackupsDir:   viper.GetString("backups_dir"),
			OverridesDir: viper.GetString("overrides_dir"),
		}

		// Optional nexus parse
		var gameDomain *string
		var modID *int64
		var canoncialNexusUrl *string
		if modsImportNexusUrl != "" {
			ref, err := nexus.ParseModURL(modsImportNexusUrl)
			if err != nil {
				return fmt.Errorf("parse --nexus-url: %w", err)
			}
			gameDomain = &ref.GameDomain
			modID = &ref.ModID
			canoncialNexusUrl = &ref.CanonicalUrl
		}

		// Safety checks for --rm up front.
		info, err := os.Lstat(inputPath)
		if err != nil {
			return fmt.Errorf("stat input: %w", err)
		}
		if modsImportRm {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("--rm refuses to operate on symlinks")
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("--rm requires a regular file input")
			}
			under, err := internal.IsUnderDir(inputPath, archivesDir)
			if err != nil {
				return fmt.Errorf("check --rm safety: %w", err)
			}
			if under {
				return fmt.Errorf("--rm refuses to remove files already inside the archive store")
			}
		}

		// Validate input as an archive using bsdtar -t, otherwise wrap into .tar.gz.
		listTimeout := time.Duration(modsImportListTimeout) * time.Second
		prep, err := prepareImportArchive(ctx, inputPath, listTimeout)
		if err != nil {
			return err
		}
		defer prep.Cleanup()

		if prep.Wrapped {
			fmt.Println(warnStyle.Render("  ⚠ input was not a supported archive; wrapped into .tar.gz for storage"))
		}

		q := dbq.New(db)

		// Resolve game install id: --game overrides active selection
		if modsImportGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			modsImportGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, modsImportGame)
		if err != nil {
			return err
		}

		opts := importer.ImportOptions{
			GameInstallID:    gi.ID,
			ArchivePath:      prep.PathToImport,
			OriginalBasename: filepath.Base(inputPath),
			PageID:           &modsImportPageID,
			NexusURL:         canoncialNexusUrl,
			NexusGameDomain:  gameDomain,
			NexusModID:       modID,
			Wrapped:          prep.Wrapped,
			WrappedFrom:      prep.WrappedFrom,
			MemberName:       prep.MemberName,
		}
		if modsImportName != "" {
			opts.ModName = &modsImportName
		}
		if modsImportLabel != "" {
			opts.FileLabel = &modsImportLabel
		}

		pageID, fileID, versionID, sha, size, err := importer.ImportArchive(ctx, db, q, bs, opts)
		if err != nil {
			return err
		}

		// Do an inventory scan for the imported archive
		if !modsImportSkipInventory {
			err := archivescanner.ScanOne(
				ctx,
				db,
				q,
				bs,
				archivescanner.Scanner{},
				sha,
				slog.Default(),
			)
			if err != nil {
				fmt.Println(warnStyle.Render("  ⚠ inventory scan failed - run 'mods scan-inventory' to retry"))
			} else {
				fmt.Println(subtleStyle.Render("  inventoried archive entries"))
			}
		}

		// Attempt Nexus linking if we have a URL, an API key, and --skip-nexus-link was not set
		if modsImportNexusUrl != "" && !modsImportSkipNexusLink {
			apiKey := viper.GetString("nexus.apikey")
			if apiKey == "" {
				fmt.Println(subtleStyle.Render("  nexus api key not configured, skipping link"))
			} else {
				client, err := nexusclient.New(ctx, apiKey, logger, rootCmd.Version)
				if err != nil {
					// Non-fatal, warn and continue
					fmt.Println(warnStyle.Render(fmt.Sprintf("  ⚠ failed to initialize nexus client: %s", err)))
				} else {
					defer client.Close()
					if err := attemptNexusLink(ctx, q, client, nexusLinkParams{
						pageID:           pageID,
						fileID:           fileID,
						versionID:        versionID,
						gameDomain:       *gameDomain,
						modID:            *modID,
						originalBasename: filepath.Base(inputPath),
						archiveSize:      size,
						nameProvided:     modsImportName != "",
						labelProvided:    modsImportLabel != "",
						label:            modsImportLabel,
						fileVersion:      modsImportFileVersion,
					}); err != nil {
						fmt.Println(warnStyle.Render(fmt.Sprintf("  ⚠ nexus link failed: %s", err)))
					}
				}
			}
		}

		// Delete original only after successful import + DB commit
		if modsImportRm {
			if err := os.Remove(inputPath); err != nil {
				// Import is done; keep this as a loud error because the user asked for --rm.
				return fmt.Errorf("import succeeded but failed to remove original file: %w", err)
			}
			fmt.Println(subtleStyle.Render("  removed original input file"))
		}

		fmt.Println("Imported:")
		fmt.Printf("  mod_page_id: %d\n", pageID)
		fmt.Printf("  mod_file_id: %d\n", fileID)
		fmt.Printf("  mod_file_version_id: %d\n", versionID)
		fmt.Printf("  sha256: %s\n", sha)
		fmt.Printf("  size_bytes: %d\n", size)

		return nil
	},
}

func init() {
	modsCmd.AddCommand(modsImportCmd)

	modsImportCmd.Flags().StringVarP(&modsImportGame, "game", "g", "",
		"Override the currently active game")
	modsImportCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	modsImportCmd.Flags().StringVar(&modsImportName, "name", "",
		"Name for the mod (defaults to archive filename)")
	modsImportCmd.Flags().StringVar(&modsImportLabel, "label", "",
		"Label for the mod file (defaults to 'Main File')")
	modsImportCmd.Flags().StringVar(&modsImportFileVersion, "file-version", "",
		"Set version for the linked file")
	modsImportCmd.Flags().StringVar(&modsImportNexusUrl, "nexus-url", "",
		"Nexus mod page URL (sets source_kind=nexus)")
	modsImportCmd.Flags().Int64Var(&modsImportPageID, "page-id", 0,
		"Attach the mod to an existing page")
	modsImportCmd.Flags().BoolVar(&modsImportRm, "rm", false,
		"Remove original archive after import")
	modsImportCmd.Flags().Int64VarP(&modsImportListTimeout, "list-timeout",
		"t", 60, "Set timeout in seconds to list the contents of the passed archive")

	modsImportCmd.Flags().BoolVar(&modsImportSkipInventory, "skip-inventory", false,
		"Skip scanning archive contents after import")
	modsImportCmd.Flags().BoolVar(&modsImportSkipNexusLink, "skip-nexus-link", false,
		"Skip linking to a specific nexus file")

	// name only makes sense when creating a new page
	modsImportCmd.MarkFlagsMutuallyExclusive("name", "page-id")
}

func prepareImportArchive(ctx context.Context, inputPath string, listTimeout time.Duration) (prepareArchiveResult, error) {
	// First, try to validate as an archive with bsdtar -t
	ctxT, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	if err := bsdtarListOK(ctxT, inputPath); err == nil {
		return prepareArchiveResult{PathToImport: inputPath, Wrapped: false, Cleanup: func() {}}, nil
	}

	// Not an archive (or bsdtar couldn't list it) -- wrap into tar.gz.
	tmpDir := viper.GetString("tmp_dir")
	wrapped, cleanup, err := wrapIntoTarGz(tmpDir, inputPath)
	if err != nil {
		return prepareArchiveResult{}, err
	}

	// Validate the wrapped archive too (should succeed unless we wrote bad tar.gz)
	ctxT2, cancel2 := context.WithTimeout(ctx, listTimeout)
	defer cancel2()
	if err := bsdtarListOK(ctxT2, wrapped); err != nil {
		cleanup()
		return prepareArchiveResult{}, fmt.Errorf("wrapped archive failed bsdtar validation: %w", err)
	}

	return prepareArchiveResult{PathToImport: wrapped, Wrapped: true, Cleanup: cleanup}, nil
}

func bsdtarListOK(ctx context.Context, archivePath string) error {
	// Keep output quiet on success; capture stderr for failure message.
	cmd := exec.CommandContext(ctx, viper.GetString("bsdtar"), "-t", "-f", archivePath)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("bsdtar -t failed: %s", msg)
		}
		return fmt.Errorf("bsdtar -t failed: %w", err)
	}
	return nil
}

// Note Mode: int64(info.Mode().Perm()) preserves permission bits but does
// _NOT_ Sticky/setuid bits and so Perm() drops them. this is our desired
// behavior
//
// This writes a tar member named as the input basename, with:
//   - uid/gid 0, uname/gname root/root
//   - original modtime from os.Stat
//   - original mode (including executable bit)
//   - content is raw bytes of the input file
func wrapIntoTarGz(tmpDir, srcPath string) (wrappedPath string, cleanup func(), err error) {
	info, err := os.Stat(srcPath)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("cannot wrap non-regular file: %s", srcPath)
	}

	base := filepath.Base(srcPath)
	if base == "" || base == "." || base == ".." {
		return "", nil, fmt.Errorf("invalid input filename: %q", base)
	}

	// Create temp file
	f, err := os.CreateTemp(tmpDir, "modctl-wrap-*.tar.gz")
	if err != nil {
		return "", nil, fmt.Errorf("create temp archive: %w", err)
	}
	tmpName := f.Name()

	cleanup = func() { _ = os.Remove(tmpName) }

	// Stream: gzip -> tar -> file contents
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Ensure we close in reverse order, capturing the first error.
	closeAll := func() error {
		var first error

		setFirst := func(err error) {
			if err != nil && first == nil {
				first = err
			}
		}

		setFirst(tw.Close())
		setFirst(gw.Close())
		setFirst(f.Sync())
		setFirst(f.Close())

		return first
	}

	src, err := os.Open(srcPath)
	if err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	hdr := &tar.Header{
		Name:    base,
		Mode:    int64(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: info.ModTime(),

		Uid:   0,
		Gid:   0,
		Uname: "root",
		Gname: "root",

		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		_ = closeAll()
		cleanup()
		return "", nil, fmt.Errorf("write tar header: %w", err)
	}

	if _, err := io.Copy(tw, src); err != nil {
		_ = closeAll()
		cleanup()
		return "", nil, fmt.Errorf("write tar body: %w", err)
	}

	if err := closeAll(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("finalize temp archive: %w", err)
	}

	return tmpName, cleanup, nil
}

type nexusLinkParams struct {
	pageID           int64
	fileID           int64
	versionID        int64
	gameDomain       string
	modID            int64
	originalBasename string
	archiveSize      int64
	nameProvided     bool
	labelProvided    bool
	label            string
	fileVersion      string
}

func attemptNexusLink(
	ctx context.Context,
	q *dbq.Queries,
	client *nexusclient.Client,
	p nexusLinkParams,
) error {
	// TODO: extract these somewhere else
	subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	// Always try to fetch mod info - useful for name even if file ID fails
	modInfo, err := client.GetModCached(p.gameDomain, int(p.modID))
	if err != nil {
		return fmt.Errorf("fetching nexus mod info: %w", err)
	}

	// Update mod page name if not explicitly provided
	if !p.nameProvided {
		if err := q.UpdateModPageName(ctx, dbq.UpdateModPageNameParams{
			ID:   p.pageID,
			Name: modInfo.Name,
		}); err != nil {
			return fmt.Errorf("updating mod page name: %w", err)
		}
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  updated mod page name: %s", modInfo.Name)))
	}

	// Fetch file list for identification
	filesResp, err := client.GetModFiles(p.gameDomain, int(p.modID))
	if err != nil {
		return fmt.Errorf("fetching nexus file list: %w", err)
	}

	match, warnings, err := nexus.IdentifyNexusFile(p.originalBasename, p.archiveSize, p.label, p.fileVersion, filesResp.Files)
	if err != nil {
		return fmt.Errorf("identifying nexus file: %w", err)
	}
	for _, warn := range warnings {
		fmt.Println(warnStyle.Render(fmt.Sprintf("  ⚠ %s", warn)))
	}
	if match == nil {
		fmt.Println(warnStyle.Render("  ⚠ could not identify nexus file id - run `mods nexus link` to resolve"))
		return nil
	}

	fmt.Println(subtleStyle.Render(fmt.Sprintf("  identified nexus file: %s v%s (file_id: %d, confidence: %s)",
		match.File.Name, match.File.Version, match.File.FileID, match.Confidence)))

	if err := q.UpdateModFileVersionNexusFileID(ctx, dbq.UpdateModFileVersionNexusFileIDParams{
		ID:          p.versionID,
		NexusFileID: sql.NullInt64{Int64: int64(match.File.FileID), Valid: true},
	}); err != nil {
		return fmt.Errorf("updating nexus file id: %w", err)
	}

	// Determine version string: CLI flag wins, otherwise use API value if non-empty
	versionToStore := p.fileVersion
	if versionToStore == "" {
		versionToStore = match.File.Version
	}
	if versionToStore != "" {
		if err := q.UpdateModFileVersionVersionString(ctx, dbq.UpdateModFileVersionVersionStringParams{
			ID:            p.versionID,
			VersionString: sql.NullString{String: versionToStore, Valid: true},
		}); err != nil {
			return fmt.Errorf("updating version string: %w", err)
		}
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  set version: %s", versionToStore)))
	}

	if !p.labelProvided {
		if err := q.UpdateModFileLabel(ctx, dbq.UpdateModFileLabelParams{
			ID:    p.fileID,
			Label: match.File.Name,
		}); err != nil {
			return fmt.Errorf("updating mod file label: %w", err)
		}
		fmt.Println(subtleStyle.Render(fmt.Sprintf("  updated mod file label: %s", match.File.Name)))
	}

	return nil
}
