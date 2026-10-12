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

package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// activeFilename is the file, in the state directory, that has the active
// selection.
const activeFilename = "active.json"

type Active struct {
	ActiveStoreID             string `json:"active_store_id,omitempty"`
	ActiveGameInstallID       int64  `json:"active_game_install_id,omitempty"`
	ActiveGameInstallSelector string `json:"active_game_install_selector,omitempty"`
	UpdatedAt                 string `json:"updated_at,omitempty"`
}

// LoadActive reads the active selection from the state directory dir. There
// is none until one is saved, which is an empty Active and not an error, and
// nothing is made by looking: not the directory, and not the file.
func LoadActive(dir string) (Active, error) {
	p := filepath.Join(dir, activeFilename)

	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Active{}, nil
		}
		return Active{}, fmt.Errorf("read %s: %w", p, err)
	}

	var a Active
	if err := json.Unmarshal(b, &a); err != nil {
		return Active{}, fmt.Errorf("parse %s: %w", p, err)
	}
	return a, nil
}

// SaveActive writes the active selection a to the state directory dir, which
// is made if it is not there. UpdatedAt is set to now, whatever a has in it.
func SaveActive(dir string, a Active) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("make %s: %w", dir, err)
	}
	p := filepath.Join(dir, activeFilename)

	a.UpdatedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal active: %w", err)
	}
	b = append(b, '\n')

	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}

	// Atomic on POSIX if same filesystem
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, p, err)
	}

	return nil
}
