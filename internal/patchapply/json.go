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

package patchapply

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// applyJSON applies patch entries to a json file.
// Key ordering and whitespace are normalized by encoding/json.
// Comments are not supported in JSON.
func applyJSON(entries []Entry, input []byte) (Result, error) {
	var res Result

	// Parse into a generic map to preserve unknown keys
	var doc map[string]interface{}
	if len(bytes.TrimSpace(input)) == 0 {
		doc = make(map[string]interface{})
	} else if err := json.Unmarshal(input, &doc); err != nil {
		return res, fmt.Errorf("parse json: %w", err)
	}
	if doc == nil {
		doc = make(map[string]interface{})
	}
	if doc == nil {
		doc = make(map[string]interface{})
	}

	for _, e := range entries {
		switch e.PatchType {
		case "json_set":
			// Parse value as JSON first to preserve types (numbers, booleans, etc.)
			// Fall back to string if not valid JSON
			var parsed interface{}
			if err := json.Unmarshal([]byte(e.EntryValue), &parsed); err != nil {
				// treat as plain string
				doc[e.EntryKey] = e.EntryValue
			} else {
				doc[e.EntryKey] = parsed
			}
			res.Applied++

		case "json_unset":
			if _, exists := doc[e.EntryKey]; !exists {
				res.Skipped++
				continue
			}
			delete(doc, e.EntryKey)
			res.Applied++
		}
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return res, fmt.Errorf("serialize json: %w", err)
	}
	out = append(out, '\n')
	res.Output = out
	return res, nil
}
