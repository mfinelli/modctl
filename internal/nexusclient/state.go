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

package nexusclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// rateLimitFilename is the file, in the state directory, that has the rate
// limits that the last response of the API said.
const rateLimitFilename = "nexus.json"

type RateLimitState struct {
	HourlyLimit     int       `json:"hourly_limit"`
	HourlyRemaining int       `json:"hourly_remaining"`
	HourlyReset     time.Time `json:"hourly_reset"`
	DailyLimit      int       `json:"daily_limit"`
	DailyRemaining  int       `json:"daily_remaining"`
	DailyReset      time.Time `json:"daily_reset"`
}

// EffectiveRemaining returns the remaining counts, treating expired windows
// as fully refreshed
func (r *RateLimitState) EffectiveRemaining() (hourly, daily int) {
	now := time.Now()
	hourly = r.HourlyRemaining
	daily = r.DailyRemaining
	if now.After(r.HourlyReset) {
		hourly = r.HourlyLimit
	}
	if now.After(r.DailyReset) {
		daily = r.DailyLimit
	}
	return
}

// LoadRateLimitState reads the rate limits from the state directory dir. There
// are none until a response of the API has said what they are, which is a state
// with nothing in it and not an error, and nothing is made by looking: not the
// directory, and not the file. A file that is not what it should be is no
// state either.
func LoadRateLimitState(dir string) (*RateLimitState, error) {
	data, err := os.ReadFile(filepath.Join(dir, rateLimitFilename))
	if os.IsNotExist(err) {
		// No state yet, return a zeroed state (first API call will
		// populate it)
		return &RateLimitState{}, nil
	}
	if err != nil {
		return nil, err
	}

	var state RateLimitState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupted state file, treat as empty
		return &RateLimitState{}, nil
	}
	return &state, nil
}

// SaveRateLimitState writes the rate limits to the state directory dir, which
// is made if it is not there.
func SaveRateLimitState(dir string, state *RateLimitState) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, rateLimitFilename), data, 0o644)
}
