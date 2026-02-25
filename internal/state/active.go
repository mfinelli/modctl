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

	"github.com/adrg/xdg"
)

type Active struct {
	ActiveStoreID string `json:"active_store_id,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

func LoadActive() (Active, error) {
	p, err := xdg.StateFile(filepath.Join("modctl", "active.json"))
	if err != nil {
		return Active{}, err
	}

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

func SaveActive(a Active) error {
	p, err := xdg.StateFile(filepath.Join("modctl", "active.json"))
	if err != nil {
		return err
	}

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
