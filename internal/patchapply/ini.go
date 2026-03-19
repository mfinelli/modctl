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
	"fmt"

	"gopkg.in/ini.v1"
)

// applyINI applies patch entries to an ini file.
// Comments are preserved by gopkg.in/ini.v1.
func applyINI(entries []Entry, input []byte) (Result, error) {
	var res Result

	cfg, err := ini.LoadSources(ini.LoadOptions{
		// Preserve original formatting as much as possible
		IgnoreInlineComment:         false,
		UnescapeValueDoubleQuotes:   true,
		UnescapeValueCommentSymbols: false,
	}, input)
	if err != nil {
		return res, fmt.Errorf("parse ini: %w", err)
	}

	for _, e := range entries {
		sectionName := e.EntrySection
		if sectionName == "" {
			sectionName = ini.DefaultSection
		}

		switch e.PatchType {
		case "ini_set":
			sec, err := cfg.GetSection(sectionName)
			if err != nil {
				// section doesn't exist: create it
				sec, err = cfg.NewSection(sectionName)
				if err != nil {
					return res, fmt.Errorf("create section %q: %w", sectionName, err)
				}
			}
			if sec.HasKey(e.EntryKey) {
				sec.Key(e.EntryKey).SetValue(e.EntryValue)
			} else {
				if _, err := sec.NewKey(e.EntryKey, e.EntryValue); err != nil {
					return res, fmt.Errorf("set key %q in section %q: %w", e.EntryKey, sectionName, err)
				}
			}
			res.Applied++

		case "ini_unset":
			sec, err := cfg.GetSection(sectionName)
			if err != nil {
				// section doesn't exist: nothing to unset
				res.Skipped++
				continue
			}
			if !sec.HasKey(e.EntryKey) {
				res.Skipped++
				continue
			}
			sec.DeleteKey(e.EntryKey)
			res.Applied++
		}
	}

	var buf bytes.Buffer
	if _, err := cfg.WriteTo(&buf); err != nil {
		return res, fmt.Errorf("serialize ini: %w", err)
	}
	res.Output = buf.Bytes()
	return res, nil
}
