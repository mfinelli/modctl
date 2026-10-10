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

package planner

import (
	"context"
	"errors"
	"io/fs"

	"github.com/mfinelli/modctl/internal/fsutil"
)

// DriftState is how a file on disk compares with the content modctl recorded
// when it installed the file.
type DriftState int

const (
	// DriftNone means the file matches what modctl installed.
	DriftNone DriftState = iota
	// DriftMissing means there is no file at the path (any more), so there is
	// nothing that could have drifted.
	DriftMissing
	// DriftModified means the content of the file differs from what modctl
	// installed.
	DriftModified
	// DriftUnknown means the file could not be read, so there is no telling
	// whether it has changed.
	DriftUnknown
)

// DriftResult is the outcome of CheckDrift.
type DriftResult struct {
	State DriftState

	// Err is why the file could not be read, when State is DriftUnknown.
	Err error
}

// CheckDrift hashes the file at absPath and compares it with installedSha, the
// content hash modctl recorded when it installed the file.
//
// A file that isn't there is reported as DriftMissing. A file that is there but
// can't be read is reported as DriftUnknown and not as an error: what to do
// about not knowing is up to the caller. The one error is being told
// to stop (ctx is done), in which case the result is not meaningful, and the
// caller must not go on to overwrite a file that it didn't get to check.
func CheckDrift(ctx context.Context, absPath, installedSha string) (DriftResult, error) {
	sha, err := fsutil.HashFile(ctx, absPath)
	if err != nil {
		if ctx.Err() != nil {
			// report that we were told to stop, not whatever the hash ran into
			return DriftResult{}, ctx.Err()
		}

		if errors.Is(err, fs.ErrNotExist) {
			return DriftResult{State: DriftMissing}, nil
		}

		return DriftResult{State: DriftUnknown, Err: err}, nil
	}

	if sha != installedSha {
		return DriftResult{State: DriftModified}, nil
	}

	return DriftResult{State: DriftNone}, nil
}
