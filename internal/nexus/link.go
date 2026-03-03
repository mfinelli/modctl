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

package nexus

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mfinelli/modctl/internal/nexusclient"
)

// MatchConfidence indicates how certain we are about a file identification
type MatchConfidence int

const (
	MatchConfidenceCertain   MatchConfidence = iota // exact filename match
	MatchConfidenceConfident                        // timestamp, size+timestamp, or label+size
	MatchConfidenceNone                             // ambiguous or no match
)

func (m MatchConfidence) String() string {
	switch m {
	case MatchConfidenceCertain:
		return "certain"
	case MatchConfidenceConfident:
		return "confident"
	default:
		return "none"
	}
}

// IdentifyResult holds the result of a successful identification
type IdentifyResult struct {
	File       nexusclient.ModFileInfo
	Confidence MatchConfidence
	Strategy   string // human readable description of what matched
}

// IdentifyNexusFile attempts to identify which ModFileInfo corresponds to a
// locally imported archive. Returns nil if identification was ambiguous or failed.
func IdentifyNexusFile(
	originalBasename string,
	archiveSize int64,
	label string, // empty string means no --label provided
	files []nexusclient.ModFileInfo,
) (*IdentifyResult, []string, error) {
	var warnings []string

	// Step 1: exact filename match against full pool (case-insensitive)
	for _, f := range files {
		if strings.EqualFold(originalBasename, f.FileName) {
			return &IdentifyResult{
				File:       f,
				Confidence: MatchConfidenceCertain,
				Strategy:   "exact filename match",
			}, warnings, nil
		}
	}

	// Step 2: apply label pre-filter if provided (case-insensitive)
	candidates := files
	labelFiltered := false
	if label != "" {
		var filtered []nexusclient.ModFileInfo
		for _, f := range files {
			if strings.EqualFold(f.Name, label) {
				filtered = append(filtered, f)
			}
		}
		if len(filtered) == 0 {
			// Warn but fall back to full pool rather than giving up entirely
			warnings = append(warnings, fmt.Sprintf(
				"label %q did not match any files on the Nexus mod page; searching full file list",
				label,
			))
		} else {
			candidates = filtered
			labelFiltered = true
		}
	}

	// Step 3: timestamp parse from originalBasename, match against uploaded_timestamp
	ts, ok := parseTimestampFromFilename(originalBasename)
	if ok {
		var matches []nexusclient.ModFileInfo
		for _, f := range candidates {
			if f.UploadedTimestamp == ts {
				matches = append(matches, f)
			}
		}
		if len(matches) == 1 {
			return &IdentifyResult{
				File:       matches[0],
				Confidence: MatchConfidenceConfident,
				Strategy:   "timestamp match",
			}, warnings, nil
		}
		if len(matches) > 1 {
			return nil, warnings, nil // ambiguous
		}
	}

	// Step 4: size + timestamp
	if ok {
		var matches []nexusclient.ModFileInfo
		for _, f := range candidates {
			if f.UploadedTimestamp == ts && f.SizeInBytes == archiveSize {
				matches = append(matches, f)
			}
		}
		if len(matches) == 1 {
			return &IdentifyResult{
				File:       matches[0],
				Confidence: MatchConfidenceConfident,
				Strategy:   "size and timestamp match",
			}, warnings, nil
		}
		if len(matches) > 1 {
			return nil, warnings, nil // ambiguous
		}
	}

	// Step 5: label + size (only meaningful if label filter succeeded and left
	// exactly one candidate)
	if labelFiltered && len(candidates) == 1 && candidates[0].SizeInBytes == archiveSize {
		return &IdentifyResult{
			File:       candidates[0],
			Confidence: MatchConfidenceConfident,
			Strategy:   "label and size match",
		}, warnings, nil
	}

	// Step 6: size only against candidate pool — not confident, skip
	if archiveSize > 0 {
		var matches []nexusclient.ModFileInfo
		for _, f := range candidates {
			if f.SizeInBytes == archiveSize {
				matches = append(matches, f)
			}
		}
		if len(matches) == 1 {
			// Single size match but not confident — caller should warn and skip
			return nil, warnings, nil
		}
	}

	// Step 7: no match
	return nil, warnings, nil
}

// parseTimestampFromFilename attempts to extract the uploaded_timestamp from a
// Nexus-style filename: {name}-{mod_id}-{version_dashes}-{timestamp}.ext
// Returns the timestamp and true if successful, 0 and false otherwise.
func parseTimestampFromFilename(basename string) (int64, bool) {
	// Strip extension
	name := strings.TrimSuffix(basename, filepath.Ext(basename))

	// Split on '-' and try to parse the last segment as a unix timestamp
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		return 0, false
	}

	last := parts[len(parts)-1]
	var ts int64
	if _, err := fmt.Sscanf(last, "%d", &ts); err != nil {
		return 0, false
	}

	// Sanity check: Nexus timestamps are unix seconds, should be plausible
	// (after 2001 when Nexus according to wikipedia was established, before 2100)
	if ts < 946681200 || ts > 4133890800 {
		return 0, false
	}

	return ts, true
}
