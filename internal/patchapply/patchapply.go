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

// Package patchapply applies structured patch entries to ini, yaml, and json
// files. It is used by the apply pipeline and the patch preview command.
package patchapply

import "fmt"

// Entry represents a single patch operation to apply
type Entry struct {
	PatchType    string // ini_set, ini_unset, yaml_set, yaml_unset, json_set, json_unset, xml_clear, xml_set, xml_unset
	EntrySection string // ini only, may be empty
	EntryKey     string
	EntryValue   string // empty for unset operations
}

// Result is the outcome of applying a set of patch entries to a file
type Result struct {
	Output  []byte
	Applied int // number of entries successfully applied
	Skipped int // number of entries that had no effect (key not found for unset)
}

// Apply applies the given patch entries to the input content and returns
// the patched result. All entries must be of the same patch family
// (ini, json, yaml, or xml) (this is enforced by the override_type on the
// overrides row upstream)
func Apply(entries []Entry, input []byte) (Result, error) {
	if len(entries) == 0 {
		return Result{Output: input}, nil
	}

	// Determine family from first entry's patch type
	switch family(entries[0].PatchType) {
	case "ini":
		return applyINI(entries, input)
	case "json":
		return applyJSON(entries, input)
	case "yaml":
		return applyYAML(entries, input)
	case "xml":
		return applyXML(entries, input)
	default:
		return Result{}, fmt.Errorf("unknown patch type family: %s", entries[0].PatchType)
	}
}

func family(patchType string) string {
	switch patchType {
	case "ini_set", "ini_unset":
		return "ini"
	case "json_set", "json_unset":
		return "json"
	case "yaml_set", "yaml_unset":
		return "yaml"
	case "xml_set", "xml_unset", "xml_clear":
		return "xml"
	default:
		return ""
	}
}
