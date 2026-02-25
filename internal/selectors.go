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

package internal

import (
	"errors"
	"fmt"
	"strings"
)

// FullSelector always includes the instance (even if it's "default").
//
// Example: steam:1091500#default
func FullSelector(storeID, storeGameID, instanceID string) string {
	storeID = strings.ToLower(strings.TrimSpace(storeID))
	storeGameID = strings.TrimSpace(storeGameID)
	instanceID = strings.TrimSpace(instanceID)

	if instanceID == "" {
		instanceID = "default"
	}

	return fmt.Sprintf("%s:%s#%s", storeID, storeGameID, instanceID)
}

// ShortSelector includes the instance only if it isn't "default".
//
// Example: steam:1091500
// Example: steam:1091500#library_2
func ShortSelector(storeID, storeGameID, instanceID string) string {
	storeID = strings.ToLower(strings.TrimSpace(storeID))
	storeGameID = strings.TrimSpace(storeGameID)
	instanceID = strings.TrimSpace(instanceID)

	if instanceID == "" || instanceID == "default" {
		return fmt.Sprintf("%s:%s", storeID, storeGameID)
	}

	return fmt.Sprintf("%s:%s#%s", storeID, storeGameID, instanceID)
}

// ParseSelector parses either:
// - store:game
// - store:game#instance
//
// If instance is omitted, it returns instanceID="default".
func ParseSelector(s string) (storeID, storeGameID, instanceID string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", "", errors.New("empty selector")
	}

	// Split store from the rest (store:game[#instance])
	colon := strings.IndexByte(s, ':')
	if colon <= 0 || colon == len(s)-1 {
		return "", "", "", fmt.Errorf(
			"invalid selector %q (expected store:game or store:game#instance)",
			s,
		)
	}

	storeID = strings.ToLower(strings.TrimSpace(s[:colon]))
	rest := strings.TrimSpace(s[colon+1:])

	if storeID == "" || rest == "" {
		return "", "", "", fmt.Errorf("invalid selector %q", s)
	}

	// Strictly split on "#"
	parts := strings.Split(rest, "#")
	if len(parts) > 2 {
		return "", "", "", fmt.Errorf(
			"invalid selector %q (multiple '#' characters)",
			s,
		)
	}

	storeGameID = strings.TrimSpace(parts[0])
	if storeGameID == "" {
		return "", "", "", fmt.Errorf(
			"invalid selector %q (missing game id)",
			s,
		)
	}

	// No instance specified -> default
	if len(parts) == 1 {
		return storeID, storeGameID, "default", nil
	}

	instanceID = strings.TrimSpace(parts[1])
	if instanceID == "" {
		return "", "", "", fmt.Errorf(
			"invalid selector %q (missing instance id after '#')",
			s,
		)
	}

	return storeID, storeGameID, instanceID, nil
}
