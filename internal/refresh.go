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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/andygrunwald/vdf"
	"github.com/mfinelli/modctl/dbq"
	"go.finelli.dev/util"
)

// steamSkippedPrefixes are display names (or prefixes thereof) that are
// internal Steam software, not real modding targets.
var steamSkippedPrefixes = []string{
	"Proton Experimental",
	"Steam Linux Runtime",
	"Steamworks Common Redistributables",
}

type RefreshResult struct {
	New      []string // display names of newly discovered installs
	Updated  []string // display names of installs seen again (already present)
	Returned []string // display names of installs that were missing and came back
	Missing  []string // display names of installs that disappeared
	Skipped  []string // display names of installs that were filtered out

	// Warnings are the non-fatal problems found along the way, in the order
	// they were found.
	Warnings []string

	// Changes is what happened to each install, in the order to show them:
	// the installs that disappeared first, then the ones that were found.
	Changes []RefreshChange
}

// RefreshChangeKind says what a refresh found out about an install.
type RefreshChangeKind int

const (
	RefreshNew      RefreshChangeKind = iota // seen for the first time
	RefreshReturned                          // was missing and has come back
	RefreshUpdated                           // seen again, as before
	RefreshMissing                           // was present and has gone
)

// RefreshChange is what a refresh found out about one install.
type RefreshChange struct {
	Name string
	Kind RefreshChangeKind
}

// knownInstall is an install modctl already has a record of.
type knownInstall struct {
	storeGameID, instanceID, displayName string
	present                              bool
}

// foundInstall is an install seen during a scan.
type foundInstall struct {
	storeGameID, instanceID, displayName string
}

// classifyInstalls works out what a scan changed. Installs that were present
// but were not found come first, by name so that the order is stable; then
// the installs that were found, in the order they were found, each as new
// (never seen), returned (known but missing) or updated (known and present).
func classifyInstalls(known []knownInstall, found []foundInstall) []RefreshChange {
	type key struct{ storeGameID, instanceID string }

	knownByKey := make(map[key]knownInstall, len(known))
	for _, k := range known {
		knownByKey[key{k.storeGameID, k.instanceID}] = k
	}
	foundSet := make(map[key]struct{}, len(found))
	for _, f := range found {
		foundSet[key{f.storeGameID, f.instanceID}] = struct{}{}
	}

	var missing []string
	for _, k := range known {
		if !k.present {
			continue
		}
		if _, ok := foundSet[key{k.storeGameID, k.instanceID}]; !ok {
			missing = append(missing, k.displayName)
		}
	}
	sort.Strings(missing)

	changes := make([]RefreshChange, 0, len(missing)+len(found))
	for _, name := range missing {
		changes = append(changes, RefreshChange{name, RefreshMissing})
	}
	for _, f := range found {
		prev, isKnown := knownByKey[key{f.storeGameID, f.instanceID}]
		switch {
		case !isKnown:
			changes = append(changes, RefreshChange{f.displayName, RefreshNew})
		case !prev.present:
			changes = append(changes, RefreshChange{f.displayName, RefreshReturned})
		default:
			changes = append(changes, RefreshChange{f.displayName, RefreshUpdated})
		}
	}
	return changes
}

// addChanges records the changes in the per-kind lists as well as in Changes.
func (r *RefreshResult) addChanges(changes []RefreshChange) {
	r.Changes = append(r.Changes, changes...)
	for _, c := range changes {
		switch c.Kind {
		case RefreshNew:
			r.New = append(r.New, c.Name)
		case RefreshReturned:
			r.Returned = append(r.Returned, c.Name)
		case RefreshUpdated:
			r.Updated = append(r.Updated, c.Name)
		case RefreshMissing:
			r.Missing = append(r.Missing, c.Name)
		}
	}
}

type steamInstall struct {
	params       dbq.UpsertGameInstallParams
	steamappsDir string // e.g. ~/.local/share/Steam/steamapps
}

// ScanStores rescans every enabled store and updates the game installs in the
// database. It prints nothing: what it found is in the result. If it fails
// part way, the result still holds the warnings found up to then.
func ScanStores(ctx context.Context, db *sql.DB) (RefreshResult, error) {
	q := dbq.New(db)
	stores, err := q.ListEnabledStores(ctx)
	if err != nil {
		return RefreshResult{}, err
	}

	var combined RefreshResult
	for _, store := range stores {
		switch store.Implementation {
		case "steam":
			result, err := refreshSteam(ctx, db, q)

			combined.New = append(combined.New, result.New...)
			combined.Updated = append(combined.Updated, result.Updated...)
			combined.Returned = append(combined.Returned, result.Returned...)
			combined.Missing = append(combined.Missing, result.Missing...)
			combined.Skipped = append(combined.Skipped, result.Skipped...)
			combined.Warnings = append(combined.Warnings, result.Warnings...)
			combined.Changes = append(combined.Changes, result.Changes...)

			if err != nil {
				return combined, err
			}
		default:
			combined.Warnings = append(combined.Warnings,
				fmt.Sprintf("store implementation %q is not supported", store.Implementation))
		}
	}

	return combined, nil
}

func refreshSteam(ctx context.Context, db *sql.DB, q *dbq.Queries) (RefreshResult, error) {
	steam, err := discoverSteamLibraries()
	if err != nil {
		return RefreshResult{Warnings: steam.Warnings},
			fmt.Errorf("error scanning for steam libraries: %w", err)
	}

	return refreshSteamLibraries(ctx, db, q, steam)
}

// refreshSteamLibraries is refreshSteam for the Steam libraries that have been
// found, which is what lets a test have any it likes.
func refreshSteamLibraries(
	ctx context.Context,
	db *sql.DB,
	q *dbq.Queries,
	steam steamLibraries,
) (RefreshResult, error) {
	var result RefreshResult
	result.Warnings = append(result.Warnings, steam.Warnings...)

	if !steam.DidScan {
		// discovery did not meaningfully run -> do NOT mark installs missing
		return result, nil
	}

	// Pre-fetch existing installs for this store so we can classify changes,
	// and so that the libraries that have an instance id keep it
	existing, err := q.ListGameInstallsByStore(ctx, "steam")
	if err != nil {
		return result, fmt.Errorf("list existing steam installs: %w", err)
	}

	inLibraries := make([]installLibrary, len(existing))
	for i, e := range existing {
		inLibraries[i] = installLibrary{
			InstanceID:  e.InstanceID,
			LibraryRoot: libraryOfInstall(e.Metadata),
			Present:     e.IsPresent != 0,
		}
	}
	instanceOfLib, reservedIDs := existingLibraryInstances(inLibraries)

	instanceByLib := assignSteamInstanceIDs(steam.Libs, steam.Roots, instanceOfLib, reservedIDs)
	installs, skips, warns, err := discoverSteamInstalls(steam.Libs, instanceByLib)
	result.Skipped = append(result.Skipped, skips...)
	result.Warnings = append(result.Warnings, warns...)
	if err != nil {
		return result, fmt.Errorf("error enumerating steam installs: %w", err)
	}

	known := make([]knownInstall, len(existing))
	for i, e := range existing {
		known[i] = knownInstall{e.StoreGameID, e.InstanceID, e.DisplayName, e.IsPresent != 0}
	}
	found := make([]foundInstall, len(installs))
	for i, di := range installs {
		found[i] = foundInstall{di.params.StoreGameID, di.params.InstanceID, di.params.DisplayName}
	}
	changes := classifyInstalls(known, found)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("error starting transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	if err := qtx.MarkStoreInstallsNotPresent(ctx, "steam"); err != nil {
		return result, fmt.Errorf("error marking steam installs not present: %w", err)
	}

	for _, di := range installs {
		id, err := qtx.UpsertGameInstall(ctx, di.params)
		if err != nil {
			return result, fmt.Errorf("upsert game install %s:%s#%s: %w",
				di.params.StoreID, di.params.StoreGameID, di.params.InstanceID, err)
		}

		if err := upsertGameDirTarget(ctx, qtx, id, di.params.InstallRoot); err != nil {
			return result, fmt.Errorf("error upserting game_dir target: %w", err)
		}

		if err := upsertProtonPrefixTarget(ctx, qtx, id, di.steamappsDir, di.params.StoreGameID); err != nil {
			return result, fmt.Errorf("error upserting proton_prefix target: %w", err)
		}

		if err := qtx.EnsureDefaultProfile(ctx, id); err != nil {
			return result, fmt.Errorf("error ensuring default profile for install_id=%d: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("error committing transaction: %w", err)
	}

	// only report changes that were actually saved
	result.addChanges(changes)
	return result, nil
}

// steamLibraries is what looking for Steam found.
type steamLibraries struct {
	// Libs are the canonicalized, deduped library root paths, sorted.
	Libs []string

	// Roots are the Steam installations that the libraries were read from
	// (canonicalized, and in the order that they are looked for in, which is
	// the order of preference). Each of them is the location of the library
	// that is part of the installation itself.
	Roots []string

	// DidScan is true if at least one libraryfolders.vdf was successfully
	// parsed.
	DidScan bool

	// Warnings are the non-fatal issues (missing files, parse errors, etc.).
	Warnings []string
}

// discoverSteamLibraries finds Steam library roots by locating and parsing
// steamapps/libraryfolders.vdf from common Steam installation roots.
func discoverSteamLibraries() (steamLibraries, error) {
	return discoverSteamLibrariesIn(candidateSteamRoots()), nil
}

// discoverSteamLibrariesIn is discoverSteamLibraries for the given places to
// look for Steam installations, instead of the usual ones.
func discoverSteamLibrariesIn(candidates []string) steamLibraries {
	seenRoots := make(map[string]struct{}, len(candidates))

	didScan := false
	warnings := []string{}
	foundRoots := []string{}

	// Deduplicate candidate roots (after best-effort canonicalization)
	var uniqRoots []string
	for _, r := range candidates {
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
		foundRoots = append(foundRoots, root)
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

	return steamLibraries{
		Libs:     libs,
		Roots:    foundRoots,
		DidScan:  didScan,
		Warnings: warnings,
	}
}

// installLibrary is where a Steam install that is already known was found.
type installLibrary struct {
	InstanceID string

	// LibraryRoot is the library the install was found in, "" if it is not
	// known (for an install that was found by something that didn't write it
	// down).
	LibraryRoot string

	Present bool
}

// libraryOfInstall is the library that a Steam install was found in, from the
// metadata that was saved with it, or "" when that doesn't say.
func libraryOfInstall(metadata sql.NullString) string {
	if !metadata.Valid {
		return ""
	}

	var meta struct {
		LibraryRoot string `json:"library_root"`
	}
	if err := json.Unmarshal([]byte(metadata.String), &meta); err != nil {
		return ""
	}

	return meta.LibraryRoot
}

// instanceOrder is the place of an instance id in the order they are given
// out in: "default", "library_2", "library_3", ... and then anything else.
func instanceOrder(id string) int {
	if id == "default" {
		return 0
	}
	if n, ok := strings.CutPrefix(id, "library_"); ok {
		if v, err := strconv.Atoi(n); err == nil {
			return v
		}
	}
	return int(^uint(0) >> 1)
}

// existingLibraryInstances works out which instance id each library already
// has, from the installs that are known: the id they were given when they
// were found in it. It also returns every instance id that is in use, which
// includes those of libraries that are not there now (a disk that isn't
// plugged in), since the ids are what the profiles and mods of an install are
// attached to, and have to stay where they are for when it comes back.
//
// What is attached to an install follows its id, so a library is taken to
// have the one that most of its installs have. If libraries claim the same id
// (which is what a library that got renumbered in the past looks like) it
// goes to the one that has most installs with it, and then to the one that
// has the most that are present, and then the lowest in order of path.
func existingLibraryInstances(installs []installLibrary) (byLib map[string]string, inUse map[string]struct{}) {
	inUse = make(map[string]struct{})

	type claim struct{ lib, id string }
	type score struct{ installs, present int }
	scores := make(map[claim]*score)

	for _, in := range installs {
		if in.InstanceID == "" {
			continue
		}
		inUse[in.InstanceID] = struct{}{}

		if in.LibraryRoot == "" {
			continue
		}
		c := claim{in.LibraryRoot, in.InstanceID}
		if scores[c] == nil {
			scores[c] = &score{}
		}
		scores[c].installs++
		if in.Present {
			scores[c].present++
		}
	}

	claims := make([]claim, 0, len(scores))
	for c := range scores {
		claims = append(claims, c)
	}
	sort.Slice(claims, func(i, j int) bool {
		a, b := claims[i], claims[j]
		sa, sb := scores[a], scores[b]
		switch {
		case sa.installs != sb.installs:
			return sa.installs > sb.installs
		case sa.present != sb.present:
			return sa.present > sb.present
		case instanceOrder(a.id) != instanceOrder(b.id):
			return instanceOrder(a.id) < instanceOrder(b.id)
		case a.id != b.id:
			return a.id < b.id
		}
		return a.lib < b.lib
	})

	// a library has one id, and an id belongs to one library
	byLib = make(map[string]string)
	idTaken := make(map[string]struct{})
	for _, c := range claims {
		if _, has := byLib[c.lib]; has {
			continue
		}
		if _, taken := idTaken[c.id]; taken {
			continue
		}
		byLib[c.lib] = c.id
		idTaken[c.id] = struct{}{}
	}

	return byLib, inUse
}

// assignSteamInstanceIDs gives each Steam library the instance id that the
// installs in it get: "default" for the main library, and "library_2",
// "library_3", ... for the others.
//
// A library keeps the id that it already has (known, which is what
// existingLibraryInstances makes of the installs that are known), so that
// nothing changes for what is attached to its installs when libraries are
// added or removed. A library that has none gets one that is not in use, which
// is not an id that is in inUse (that of an install that is known, whether or
// not its library is there now) and not one that another library has been
// given: the first unused number, in order of path.
//
// The main library is the one in the Steam installation itself, which is there
// for as long as Steam is, and it is the default when none is yet. roots are
// the Steam installations that were found, in order of preference: the first
// of them that is a library without an id is the one. With none of them among
// those (which should not happen) it is the first library in order of path.
func assignSteamInstanceIDs(
	libs, roots []string,
	known map[string]string,
	inUse map[string]struct{},
) map[string]string {
	if len(libs) == 0 {
		return map[string]string{}
	}

	sorted := append([]string{}, libs...)
	sort.Strings(sorted)

	m := make(map[string]string, len(sorted))
	given := make(map[string]struct{}, len(sorted))
	taken := func(id string) bool {
		if _, ok := given[id]; ok {
			return true
		}
		_, ok := inUse[id]
		return ok
	}

	// what the libraries already have
	var unknown []string
	for _, lib := range sorted {
		id, ok := known[lib]
		if _, isGiven := given[id]; !ok || id == "" || isGiven {
			unknown = append(unknown, lib)
			continue
		}
		m[lib] = id
		given[id] = struct{}{}
	}

	// the main library, if nothing has been
	if !taken("default") && len(unknown) > 0 {
		defaultLib := unknown[0]
		for _, root := range roots {
			if slices.Contains(unknown, root) {
				defaultLib = root
				break
			}
		}

		m[defaultLib] = "default"
		given["default"] = struct{}{}
		unknown = slices.DeleteFunc(unknown, func(lib string) bool { return lib == defaultLib })
	}

	// and the others
	n := 2
	for _, lib := range unknown {
		id := fmt.Sprintf("library_%d", n)
		for taken(id) {
			n++
			id = fmt.Sprintf("library_%d", n)
		}
		m[lib] = id
		given[id] = struct{}{}
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
) ([]steamInstall, []string, []string, error) {
	// for each lib:
	// - list steamapps/appmanifest_*.acf
	// - parse
	// - get appid, name, installdir
	// - installRaw = <lib>/steamapps/common/<installdir>
	// - installCanon = canonicalizePathBestEffort(installRaw)
	// - metadata: include install_root_raw + library_root (+ manifest_path)
	warnings := []string{}
	skipped := []string{}
	installs := []steamInstall{}

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

			if isSteamInternalTitle(display) {
				skipped = append(skipped, display)
				continue
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

			installs = append(installs, steamInstall{
				params: dbq.UpsertGameInstallParams{
					StoreID:         "steam",
					StoreGameID:     appid,
					InstanceID:      instID,
					CanonicalGameID: sql.NullString{},
					DisplayName:     display,
					InstallRoot:     installCanon,
					Metadata:        util.NullString(string(metaJSON)),
					IsPresent:       util.SqliteBoolToInt(true),
					LastSeenAt:      sql.NullString{String: nowISO8601Z(), Valid: true},
				},
				steamappsDir: steamapps,
			})
		}
	}

	return installs, skipped, warnings, nil
}

func upsertGameDirTarget(ctx context.Context, q *dbq.Queries, gameInstallID int64, installRoot string) error {
	_, err := q.UpsertDiscoveredTarget(ctx, dbq.UpsertDiscoveredTargetParams{
		GameInstallID: gameInstallID,
		Name:          "game_dir",
		RootPath:      installRoot,
	})
	if err != nil {
		return fmt.Errorf("upsert game_dir target for install_id=%d: %w", gameInstallID, err)
	}
	return nil
}

func upsertProtonPrefixTarget(ctx context.Context, q *dbq.Queries, gameInstallID int64, steamappsDir, appid string) error {
	const targetName = "proton_prefix"

	prefixPath := filepath.Join(steamappsDir, "compatdata", appid, "pfx", "drive_c")
	if _, err := os.Stat(prefixPath); err != nil {
		if os.IsNotExist(err) {
			// No Proton prefix for this game (native Linux or never launched).
			return nil
		}
		// Stat failed for some other reason; non-fatal, skip.
		return nil
	}

	canon, err := canonicalizePathBestEffort(prefixPath)
	if err != nil {
		canon = filepath.Clean(prefixPath)
	}

	_, err = q.UpsertDiscoveredTarget(ctx, dbq.UpsertDiscoveredTargetParams{
		GameInstallID: gameInstallID,
		Name:          targetName,
		RootPath:      canon,
	})
	return err
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
		appStateAny = parsed["appstate"]
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

func nowISO8601Z() string {
	// Match SQLite default format: %Y-%m-%dT%H:%M:%fZ
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func isSteamInternalTitle(name string) bool {
	for _, prefix := range steamSkippedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
