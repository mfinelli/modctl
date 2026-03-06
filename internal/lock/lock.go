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

// Package lock provides per-game-install exclusive locking for apply and
// unapply operations. It uses flock(2) to prevent concurrent modifications
// to the same game directory.
package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// GameInstall acquires an exclusive non-blocking flock on a lockfile for the
// given game install ID. It returns an unlock function that must be called
// when the operation is complete.
//
// If another process already holds the lock, an error is returned immediately
// rather than blocking. The lock is also released automatically by the OS if
// the process exits or crashes.
//
// locksDir should be a stable per-installation directory, e.g.
// $XDG_STATE_HOME/modctl/locks.
func GameInstall(locksDir string, gameInstallID int64) (unlock func(), err error) {
	if err := os.MkdirAll(locksDir, 0o755); err != nil {
		return nil, fmt.Errorf("create locks dir: %w", err)
	}

	lockPath := filepath.Join(locksDir, fmt.Sprintf("%d.lock", gameInstallID))

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lockfile %q: %w", lockPath, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf(
				"game install %d is already being modified by another process\n"+
					"  if you are sure no other modctl process is running, delete %q",
				gameInstallID, lockPath,
			)
		}
		return nil, fmt.Errorf("acquire lock for game install %d: %w", gameInstallID, err)
	}

	unlock = func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}

	return unlock, nil
}
