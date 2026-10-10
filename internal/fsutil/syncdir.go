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

package fsutil

import "os"

// SyncDir calls fsync(2) on a directory to ensure that metadata changes
// within that directory are durably persisted to disk.
//
// Why this is needed:
// After renaming a file into its final location (os.Rename), the file’s
// contents are durable (because we fsync’d the temp file), but the directory
// entry itself may still be sitting in the kernel’s metadata buffers.
// If the system crashes at that exact moment, the file could theoretically
// disappear after reboot even though the rename returned successfully.
//
// By opening the directory and calling Sync() on it, we force the directory
// metadata (including the new filename entry) to be flushed to stable storage.
//
// This is best-effort: some filesystems may ignore directory fsync or relax
// guarantees, but on modern Linux filesystems (ext4, xfs, btrfs) this provides
// proper crash-consistency for atomic rename patterns.
//
// It is intentionally non-fatal in callers because durability is strongly
// desired but not worth aborting the operation if unsupported.
func SyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
