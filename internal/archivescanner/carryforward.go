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

// cachedHash is the content hash recorded for an inventory entry by an
// earlier scan (apply fills these in lazily, keyed by archive and position).
type cachedHash struct {
	Position int64
	Type     string
	Size     int64
	Sha256   string
}

// carryForwardHashes decides which previously cached content hashes are still
// valid for a freshly scanned set of entries, returning them keyed by
// position. A hash is the digest of the bytes at that position in a
// content-addressed archive, so it survives a rescan even if the entry's path
// was recorded differently (which is the point of rescanning). The type and
// size are compared as a guard against the listing having changed shape.
func carryForwardHashes(old []cachedHash, entries []Entry) map[int64]string {
	byPosition := make(map[int64]cachedHash, len(old))
	for _, h := range old {
		byPosition[h.Position] = h
	}

	kept := make(map[int64]string)
	for _, e := range entries {
		h, ok := byPosition[int64(e.Position)]
		if !ok {
			continue
		}
		if h.Type != string(e.Type) || h.Size != e.SizeBytes {
			continue
		}
		kept[h.Position] = h.Sha256
	}
	return kept
}
