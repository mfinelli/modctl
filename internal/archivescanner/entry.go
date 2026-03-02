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

package archivescanner

// EntryType represents the type of an entry within an archive,
// as reported by bsdtar.
type EntryType string

const (
	EntryTypeFile    EntryType = "file"
	EntryTypeDir     EntryType = "dir"
	EntryTypeSymlink EntryType = "symlink"
	EntryTypeOther   EntryType = "other"
)

// Entry represents a single entry parsed from `bsdtar -tvvf` output.
// It reflects the raw archive contents before any remap rules are applied.
// Paths are not normalized or validated here - that is the planner's job.
type Entry struct {
	// Position is the zero-based index of this entry in the archive listing
	// Used as the canonical key since archives may contain duplicate paths
	Position int

	// RawPath is the path as it appears in the archive, unmodified
	RawPath string

	// Type is the entry type derived from the permission string
	Type EntryType

	// SizeBytes is the uncompressed size as reported by the archive header
	// Zero for directories; may be zero for symlinks
	SizeBytes int64

	// LinkTarget is the symlink destination, only set when Type == EntryTypeSymlink
	LinkTarget string

	// ParseError is non-empty if this line could not be fully parsed.
	// The entry is still recorded with whatever fields were extractable,
	// and Type will be EntryTypeOther
	ParseError string
}
