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

package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/andygrunwald/vdf"
	"github.com/mfinelli/modctl/dbq"
)

func ScanStores(ctx context.Context, db *sql.DB) error {
	q := dbq.New(db)
	stores, err := q.ListEnabledStores(ctx)
	if err != nil {
		return err
	}

	for _, store := range stores {
		switch store.Implementation {
		case "steam":
			if err := refreshSteam(ctx, db, q); err != nil {
				return err
			}
		default:
			// TODO: make this pretty (WARN)
			fmt.Printf("Implementation %s isn't currently implemented\n",
				store.Implementation)
		}
	}

	return nil
}

func refreshSteam(ctx context.Context, db *sql.DB, q *dbq.Queries) error {
	libs, didScan, warns, err := discoverSteamLibraries()
	for _, w := range warns {
		// TODO make this pretty
		fmt.Printf("WARNING: %s", w)
	}
	if err != nil {
		return fmt.Errorf("error scanning for steam libraries: %w", err)
	}
	if !didScan {
		// discovery did not meaningfully run -> do NOT mark installs missing
		return nil
	}

	instanceByLib := assignSteamInstanceIDs(libs)
	installs, warns, err := discoverSteamInstalls(libs, instanceByLib)
	for _, w := range warns {
		// TODO make this pretty
		fmt.Printf("WARNING: %s", w)
	}
	if err != nil {
		return fmt.Errorf("error enumerating steam installs: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("error starting transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	if err := qtx.MarkStoreInstallsNotPresent(ctx, "steam"); err != nil {
		return fmt.Errorf("error marking steam installs not present: %w", err)
	}

	for _, di := range installs {
		id, err := qtx.UpsertGameInstall(ctx, di)
		if err != nil {
			return fmt.Errorf("upsert game install %s:%s#%s: %w",
				di.StoreID, di.StoreGameID, di.InstanceID, err)
		}

		if err := upsertGameDirTarget(ctx, qtx, id, di.InstallRoot); err != nil {
			return fmt.Errorf("error upserting target dir: %w", err)
		}

		if err := qtx.EnsureDefaultProfile(ctx, id); err != nil {
			return fmt.Errorf("error ensuring default profile for install_id=%d: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error committing transaction: %w", err)
	}

	return nil
}

// DiscoverSteamLibraries finds Steam library roots by locating and parsing
// steamapps/libraryfolders.vdf from common Steam installation roots.
//
// Returns:
// - libs: canonicalized, deduped library root paths
// - didScan: true if at least one libraryfolders.vdf was successfully parsed
// - warnings: non-fatal issues (missing files, parse errors, etc.)
func discoverSteamLibraries() ([]string, bool, []string, error) {
	roots := candidateSteamRoots()
	seenRoots := make(map[string]struct{}, len(roots))

	didScan := false
	warnings := []string{}

	// Deduplicate candidate roots (after best-effort canonicalization)
	var uniqRoots []string
	for _, r := range roots {
		r = expandHome(r)
		canon, err := canonicalizePathBestEffort(r)
		if err != nil {
			// root canonicalization failure isn't fatal; keep cleaned absolute
			warnings = append(warnings, fmt.Sprintf("steam root canonicalize failed (%s): %v", r, err))
			canon = filepath.Clean(r)
		}
		if _, ok := seenRoots[canon]; ok {
			continue
		}
		seenRoots[canon] = struct{}{}
		uniqRoots = append(uniqRoots, canon)
	}

	// Parse libraryfolders.vdf from any root that has it
	libSet := make(map[string]struct{})
	for _, root := range uniqRoots {
		vdfPath := filepath.Join(root, "steamapps", "libraryfolders.vdf")
		st, statErr := os.Stat(vdfPath)
		if statErr != nil {
			continue // not a steam root (or not installed here)
		}
		if st.IsDir() {
			warnings = append(warnings, fmt.Sprintf("unexpected directory at %s", vdfPath))
			continue
		}

		f, openErr := os.Open(vdfPath)
		if openErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to open %s: %v", vdfPath, openErr))
			continue
		}

		p := vdf.NewParser(f)
		parsed, parseErr := p.Parse()
		f.Close()
		if parseErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to parse %s: %v", vdfPath, parseErr))
			continue
		}

		paths := extractLibraryPaths(parsed)
		if len(paths) == 0 {
			// We successfully parsed a VDF file, so this still counts as a scan.
			didScan = true
			warnings = append(warnings, fmt.Sprintf("no libraries found in %s", vdfPath))
			continue
		}

		didScan = true
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			p = expandHome(p)
			canon, cerr := canonicalizePathBestEffort(p)
			if cerr != nil {
				// best-effort: still include cleaned absolute-ish path
				warnings = append(warnings, fmt.Sprintf("library path canonicalize failed (%s): %v", p, cerr))
				canon = filepath.Clean(p)
			}
			libSet[canon] = struct{}{}
		}
	}

	// Materialize deterministic output order
	libs := []string{}
	for p := range libSet {
		libs = append(libs, p)
	}
	sort.Strings(libs)

	return libs, didScan, warnings, nil
}

func assignSteamInstanceIDs(libs []string) map[string]string {
	if len(libs) == 0 {
		return map[string]string{}
	}

	// Choose default deterministically: lexicographically smallest
	// TODO improve later using "library containing Steam root"
	sorted := append([]string{}, libs...)
	sort.Strings(sorted)
	defaultLib := sorted[0]

	m := map[string]string{defaultLib: "default"}
	n := 2
	for _, lib := range sorted[1:] {
		m[lib] = fmt.Sprintf("library_%d", n)
		n++
	}
	return m
}

// DiscoverSteamInstalls enumerates installed Steam games by scanning
// <libraryRoot>/steamapps/appmanifest_*.acf for each library root.
//
// It returns db.UpsertGameInstallParams directly, leaving LastSeenAt unset
// so the caller can apply one consistent timestamp to all rows for the refresh.
func discoverSteamInstalls(
	libraryRoots []string, // canonical library roots
	instanceByLib map[string]string, // canonical lib root -> instance_id
) ([]dbq.UpsertGameInstallParams, []string, error) {
	// for each lib:
	// - list steamapps/appmanifest_*.acf
	// - parse
	// - get appid, name, installdir
	// - installRaw = <lib>/steamapps/common/<installdir>
	// - installCanon = canonicalizePathBestEffort(installRaw)
	// - metadata: include install_root_raw + library_root (+ manifest_path)
	warnings := []string{}
	installs := []dbq.UpsertGameInstallParams{}

	type key struct {
		appid    string
		instance string
	}
	seen := map[key]struct{}{}

	for _, libRoot := range libraryRoots {
		instID, ok := instanceByLib[libRoot]
		if !ok || strings.TrimSpace(instID) == "" {
			warnings = append(warnings, fmt.Sprintf("no instance_id mapping for library root: %s", libRoot))
			continue
		}

		steamapps := filepath.Join(libRoot, "steamapps")
		// If the library root is present but steamapps isn't, it might be an odd layout.
		// Not fatal.
		if st, statErr := os.Stat(steamapps); statErr != nil || !st.IsDir() {
			continue
		}

		glob := filepath.Join(steamapps, "appmanifest_*.acf")
		manifestPaths, globErr := filepath.Glob(glob)
		if globErr != nil {
			warnings = append(warnings, fmt.Sprintf("glob failed (%s): %v", glob, globErr))
			continue
		}

		// Deterministic ordering helps tests/logging
		sort.Strings(manifestPaths)

		for _, manifestPath := range manifestPaths {
			appid, name, installdir, parseWarn, perr := parseAppManifest(manifestPath)
			if parseWarn != "" {
				warnings = append(warnings, parseWarn)
			}
			if perr != nil {
				// non-fatal: skip this manifest
				continue
			}

			// Build install paths
			installRaw := filepath.Join(steamapps, "common", installdir)
			installCanon, cerr := canonicalizePathBestEffort(installRaw)
			if cerr != nil {
				// best-effort: still usable, but warn
				warnings = append(warnings, fmt.Sprintf("install_root canonicalize failed (%s): %v", installRaw, cerr))
				installCanon = filepath.Clean(installRaw)
			}

			display := strings.TrimSpace(name)
			if display == "" {
				display = fmt.Sprintf("Steam %s", appid)
			}

			// Metadata: keep raw + provenance.
			meta := map[string]any{
				"install_root_raw": installRaw,
				"library_root":     libRoot,
				"manifest_path":    manifestPath,
				"steamapps_root":   steamapps,
			}
			metaJSON, merr := json.Marshal(meta)
			if merr != nil {
				// should never happen, but don't fail discovery over it
				warnings = append(warnings, fmt.Sprintf("metadata marshal failed (%s): %v", manifestPath, merr))
			}

			k := key{appid: appid, instance: instID}
			if _, dup := seen[k]; dup {
				// Rare, but can happen if filesystem has duplicates or weird symlinks.
				// Prefer first occurrence.
				continue
			}
			seen[k] = struct{}{}

			installs = append(installs, dbq.UpsertGameInstallParams{
				StoreID:         "steam",
				StoreGameID:     appid,
				InstanceID:      instID,
				CanonicalGameID: sql.NullString{}, // not used for steam v1
				DisplayName:     display,
				InstallRoot:     installCanon,
				Metadata:        nullStringFromBytes(metaJSON),
				LastSeenAt:      sql.NullString{String: nowISO8601Z(), Valid: true}, // caller sets once per refresh
			})
		}
	}

	return installs, warnings, nil
}

func upsertGameDirTarget(ctx context.Context, q *dbq.Queries, gameInstallID int64, installRoot string) error {
	const targetName = "game_dir"

	t, err := q.GetTargetByName(ctx, dbq.GetTargetByNameParams{
		GameInstallID: gameInstallID,
		Name:          targetName,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("get target %s for install_id=%d: %w", targetName, gameInstallID, err)
		}
		// doesn't exist -> create
		return q.UpsertDiscoveredTarget(ctx, dbq.UpsertDiscoveredTargetParams{
			GameInstallID: gameInstallID,
			Name:          targetName,
			RootPath:      installRoot,
			Metadata:      sql.NullString{},
		})
	}

	// don't overwrite if user has specified something manually
	if t.Origin == "user_override" {
		return nil
	}

	return q.UpsertDiscoveredTarget(ctx, dbq.UpsertDiscoveredTargetParams{
		GameInstallID: gameInstallID,
		Name:          targetName,
		RootPath:      installRoot,
		Metadata:      sql.NullString{},
	})
}

// canonicalizePathBestEffort returns an absolute, cleaned path, attempting to
// resolve symlinks. If EvalSymlinks fails, it returns the cleaned absolute
// path anyway.
func canonicalizePathBestEffort(p string) (string, error) {
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", err
		}
		p = abs
	}
	real, err := filepath.EvalSymlinks(p)
	if err == nil {
		return filepath.Clean(real), nil
	}
	// best effort: return cleaned absolute even if symlink resolution fails
	return p, nil
}

func candidateSteamRoots() []string {
	home, _ := os.UserHomeDir()

	// Primary: XDG data home + Steam
	roots := []string{
		filepath.Join(xdg.DataHome, "Steam"),
		// Common non-XDG path still seen in the wild:
		filepath.Join(home, ".local", "share", "Steam"),
		// Legacy symlink-style installs:
		filepath.Join(home, ".steam", "steam"),
		// Flatpak Steam:
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam"),
	}

	return roots
}

func expandHome(p string) string {
	if p == "" {
		return p
	}
	if p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// extractLibraryPaths supports both the old and new libraryfolders.vdf formats.
//
// Old-ish format (seen historically):
// "libraryfolders" { "1" "/path/to/library" "2" "/path" }
//
// New-ish format:
//
//	"libraryfolders" {
//	  "1" { "path" "/path/to/library" "label" "" ... }
//	  "2" { "path" "/path" ... }
//	}
func extractLibraryPaths(parsed any) []string {
	root, ok := parsed.(map[string]any)
	if !ok {
		return nil
	}

	lf, ok := root["libraryfolders"].(map[string]any)
	if !ok {
		// Sometimes the parser yields map[string]interface{} with different key casing,
		// but in practice "libraryfolders" is stable. If it isn't there, give up.
		return nil
	}

	var out []string
	for k, v := range lf {
		// Library entries are usually numeric keys ("0", "1", "2", ...)
		// but there are also non-library keys like "contentstatsid".
		if _, err := strconv.Atoi(k); err != nil {
			continue
		}

		switch vv := v.(type) {
		case string:
			// old format: "1" "/path"
			out = append(out, vv)
		case map[string]any:
			// new format: "1" { "path" "/path" ... }
			if p, ok := vv["path"].(string); ok && strings.TrimSpace(p) != "" {
				out = append(out, p)
			}
		}
	}

	return out
}

// parseAppManifest parses a single Steam appmanifest_*.acf and extracts:
// - appid (required)
// - name (optional)
// - installdir (required)
//
// Returns a warning string for non-fatal issues, and an error if the manifest
// should be skipped.
func parseAppManifest(manifestPath string) (appid, name, installdir, warning string, err error) {
	f, openErr := os.Open(manifestPath)
	if openErr != nil {
		return "", "", "", fmt.Sprintf("failed to open %s: %v", manifestPath, openErr), openErr
	}
	defer f.Close()

	p := vdf.NewParser(f)
	parsed, perr := p.Parse()
	if perr != nil {
		// Steam may be writing while we read; treat as non-fatal
		w := fmt.Sprintf("failed to parse %s: %v", manifestPath, perr)
		return "", "", "", w, perr
	}

	// appmanifest files are usually:
	// { "AppState": { "appid": "1091500", "name": "...", "installdir": "..." ... } }
	appStateAny, ok := parsed["AppState"]
	if !ok {
		// Some parsers/libraries might lower-case keys; be tolerant
		appStateAny, ok = parsed["appstate"]
	}
	appState, ok := appStateAny.(map[string]any)
	if !ok {
		w := fmt.Sprintf("manifest missing AppState map %s", manifestPath)
		return "", "", "", w, fmt.Errorf("%s", w)
	}

	appid = asString(appState["appid"])
	name = asString(appState["name"])
	installdir = asString(appState["installdir"])

	appid = strings.TrimSpace(appid)
	installdir = strings.TrimSpace(installdir)

	if appid == "" || installdir == "" {
		w := fmt.Sprintf("manifest missing required fields (appid/installdir) %s", manifestPath)
		return "", "", "", w, fmt.Errorf("%s", w)
	}

	return appid, name, installdir, "", nil
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		// vdf parser typically yields strings; if not, try sprint
		return fmt.Sprint(v)
	}
}

func nullStringFromBytes(b []byte) sql.NullString {
	if len(b) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

func nowISO8601Z() string {
	// Match SQLite default format: %Y-%m-%dT%H:%M:%fZ
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}
