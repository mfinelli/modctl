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

package restore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/fsutil"
)

// ErrUnsafePaths is the error of a bundle that has paths that would take what
// is done with them outside of the directory they are for.
var ErrUnsafePaths = errors.New("bundle is not safe to import")

// maxUnsafeShown is how many of the paths that are not safe an error lists.
const maxUnsafeShown = 5

// checkRelpaths is nil if all of the paths (of what, in the plural, is the kind
// of thing that they are the paths of) are ones that are inside the directory
// that they are relative to, and an ErrUnsafePaths that lists the ones that are
// not if some are not.
func checkRelpaths(what string, relpaths []string) error {
	var bad []string
	for _, p := range relpaths {
		if err := fsutil.ValidateRelPath(p); err != nil {
			bad = append(bad, fmt.Sprintf("%q (%v)", p, err))
		}
	}
	if len(bad) == 0 {
		return nil
	}

	shown := bad
	if len(shown) > maxUnsafeShown {
		shown = shown[:maxUnsafeShown]
	}
	msg := strings.Join(shown, ", ")
	if len(bad) > len(shown) {
		msg += fmt.Sprintf(" and %d more", len(bad)-len(shown))
	}

	return fmt.Errorf("%w: %d of its %s have a path that is outside of the directory it is for: %s",
		ErrUnsafePaths, len(bad), what, msg)
}

// validateGamePaths checks the paths in the part of a bundle that is for a
// game, which is only the paths of its overrides: what the bundle has of a game
// is not what is on disk.
func validateGamePaths(ctx context.Context, bq *dbq.Queries, gameInstallID int64) error {
	overrides, err := bq.ExportGetOverridesForGameInstall(ctx, gameInstallID)
	if err != nil {
		return fmt.Errorf("get overrides: %w", err)
	}

	relpaths := make([]string, len(overrides))
	for i, o := range overrides {
		relpaths[i] = o.Relpath
	}

	return checkRelpaths("overrides", relpaths)
}

// validateFullPaths checks the paths in all of a bundle that a full restore
// would put in the database: those of the overrides and, when the restore
// keeps what is on disk (a restore on the same machine, which is the only one
// that doesn't zero it), those of the backups and of the files that are
// installed.
func validateFullPaths(ctx context.Context, bq *dbq.Queries, sameMachine bool) error {
	overrides, err := bq.ListAllOverrideRelpaths(ctx)
	if err != nil {
		return fmt.Errorf("get overrides: %w", err)
	}
	if err := checkRelpaths("overrides", overrides); err != nil {
		return err
	}

	if !sameMachine {
		return nil
	}

	backups, err := bq.ListAllBackupRelpaths(ctx)
	if err != nil {
		return fmt.Errorf("get backups: %w", err)
	}
	if err := checkRelpaths("backups", backups); err != nil {
		return err
	}

	installed, err := bq.ListAllInstalledFileRelpaths(ctx)
	if err != nil {
		return fmt.Errorf("get installed files: %w", err)
	}

	return checkRelpaths("installed files", installed)
}

// CheckPaths checks the paths in a bundle as strictly as a restore could: as if
// what is on disk was kept, which is the case of a restore on the same machine.
// It is for saying whether a bundle is fine to import, without importing it.
func CheckPaths(ctx context.Context, bundle *Bundle) error {
	return validateFullPaths(ctx, dbq.New(bundle.BundleDB), true)
}
