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

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Parse reads the output of `bsdtar -tvvf <archive>` and returns one Entry
// per line. It never returns an error for individual bad lines - those are
// recorded in Entry.ParseError so the caller gets a complete picture of the
// archive. A non-nil error return means the reader itself failed.
//
// bsdtar -tvvf produces lines like:
//
//	-rw-r--r--  0 0      0      123456 Jan  1 00:00 path/to/file
//	lrwxrwxrwx  0 0      0           0 Jan  1 00:00 link -> target
//	drwxr-xr-x  0 0      0           0 Jan  1 00:00 some/dir/
//
// bsdtar prints a summary trailer as the final line, e.g.:
//
//	"Archive Format: POSIX ustar format,  Compression: gzip"
//	"Archive Format: RAR,  Compression: none"
//	"Archive Format: 7-Zip,  Compression: none"
//
// This line is skipped and does not produce an Entry.
func Parse(r io.Reader) ([]Entry, error) {
	var entries []Entry
	position := 0

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// bsdtar prints a summary trailer on the last line, e.g.:
		//   "Archive Format: POSIX ustar format,  Compression: gzip"
		// Skip it rather than recording a parse error
		if strings.HasPrefix(line, "Archive Format:") {
			continue
		}
		entry := parseLine(line, position)
		entries = append(entries, entry)
		position++
	}

	if err := scanner.Err(); err != nil {
		return entries, fmt.Errorf("reading bsdtar output: %w", err)
	}

	return entries, nil
}

// parseLine parses a single line of `bsdtar -tvvf` output into an Entry.
//
// The expected format is:
//
//	<perms> <uid> <gid> <size> <month> <day> <time/year> <path> [-> <target>]
//
// Field indices after splitting on whitespace:
//
//	0: permissions (e.g. "-rw-r--r--")
//	1: link count   (e.g. "0")
//	2: uid/owner    (numeric or named, e.g. "0", "501", "root")
//	3: gid/group    (numeric or named, e.g. "0", "20", "root")
//	4: size in bytes
//	5: month
//	6: day
//	7: time or year
//	8+: path (and optionally "-> target" for symlinks)
//
// bsdtar uses this same format across zip, rar, 7z, tar.gz and other
// archive types since libarchive normalises the listing. uid/gid may be
// numeric or named depending on archive type and creating platform.
//
// The path is taken verbatim from the line: it starts after the single
// separator following the time/year field. Leading, trailing and repeated
// whitespace inside the path is significant (an archive can contain a file
// literally named " foo.dll") and must survive so that the recorded path
// matches what bsdtar extracts.
func parseLine(line string, position int) Entry {
	entry := Entry{
		Position: position,
		Type:     EntryTypeOther,
	}

	fields, pathAndTarget, ok := splitListingLine(line)

	// Minimum viable line: perms + links + uid + gid + size + month + day + time + path
	if !ok {
		entry.ParseError = fmt.Sprintf("too few fields (%d): %q", len(strings.Fields(line)), line)
		return entry
	}

	perms := fields[0]
	if len(perms) == 0 {
		entry.ParseError = fmt.Sprintf("empty permissions field: %q", line)
		return entry
	}

	// Entry type from first character of permission string
	switch perms[0] {
	case '-':
		entry.Type = EntryTypeFile
	case 'd':
		entry.Type = EntryTypeDir
	case 'l':
		entry.Type = EntryTypeSymlink
	default:
		entry.Type = EntryTypeOther
	}

	// Size field (index 4)
	size, err := strconv.ParseInt(fields[4], 10, 64)
	if err != nil {
		// Non-fatal: record what we can, flag the parse issue
		entry.ParseError = fmt.Sprintf("could not parse size %q: %v", fields[4], err)
	} else {
		entry.SizeBytes = size
	}

	// pathAndTarget (everything after the metadata fields) is the path,
	// possibly followed by " -> target" for symlinks.

	if entry.Type == EntryTypeSymlink {
		// Symlink format: "some/path -> target/path"
		// The arrow " -> " is the delimiter; use the last occurrence
		// since either side could theoretically contain " -> "
		if idx := strings.LastIndex(pathAndTarget, " -> "); idx >= 0 {
			entry.RawPath = pathAndTarget[:idx]
			entry.LinkTarget = pathAndTarget[idx+4:]
		} else {
			// Malformed symlink line - record path as-is, flag it
			entry.RawPath = pathAndTarget
			entry.ParseError = fmt.Sprintf("symlink entry missing ' -> ' separator: %q", line)
		}
	} else {
		entry.RawPath = pathAndTarget
	}

	return entry
}

// listingMetaFields is the number of whitespace-separated metadata fields
// that precede the path in a `bsdtar -tvv` line (see parseLine).
const listingMetaFields = 8

// splitListingLine splits a `bsdtar -tvv` line into its leading metadata
// fields and the remainder (path and optional symlink target) without
// altering the remainder's whitespace. Metadata fields are separated by one
// or more spaces (columns are padded); the remainder begins after exactly
// one separator following the last metadata field. ok is false if the line
// has fewer than listingMetaFields fields or no remainder.
func splitListingLine(line string) (fields []string, rest string, ok bool) {
	isSpace := func(b byte) bool { return b == ' ' || b == '\t' }

	i := 0
	fields = make([]string, 0, listingMetaFields)
	for len(fields) < listingMetaFields {
		for i < len(line) && isSpace(line[i]) {
			i++
		}
		start := i
		for i < len(line) && !isSpace(line[i]) {
			i++
		}
		if start == i {
			return nil, "", false
		}
		fields = append(fields, line[start:i])
	}

	// skip exactly one separator; whatever follows is the path
	if i+1 >= len(line) {
		return nil, "", false
	}
	return fields, line[i+1:], true
}
