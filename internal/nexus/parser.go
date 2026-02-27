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
	"net/url"
	"strconv"
	"strings"
)

type ModRef struct {
	GameDomain string
	ModID      int64
}

// ParseModURL extracts (game_domain, mod_id) from a Nexus Mods mod page URL.
//
// Expected paths look like:
//
//	/<game_domain>/mods/<mod_id>
//
// e.g. https://www.nexusmods.com/skyrimspecialedition/mods/266
func ParseModURL(raw string) (ModRef, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return ModRef{}, fmt.Errorf("parse url: %w", err)
	}
	if u.Host == "" {
		return ModRef{}, fmt.Errorf("invalid nexus url: missing host")
	}

	// We’re not trying to support every Nexus subdomain; just accept
	// nexusmods.com broadly
	host := strings.ToLower(u.Host)
	if !strings.Contains(host, "nexusmods.com") {
		return ModRef{}, fmt.Errorf("not a nexusmods.com url: host=%q", u.Host)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// Need at least: <game>/mods/<id>
	if len(parts) < 3 {
		return ModRef{}, fmt.Errorf("invalid nexus url path: %q", u.Path)
	}

	game := parts[0]
	if game == "" {
		return ModRef{}, fmt.Errorf("invalid nexus url: missing game domain")
	}

	// Find the "mods" segment and take the next token as mod id
	// This is tolerant of extra segments
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "mods" {
			idStr := parts[i+1]
			id, convErr := strconv.ParseInt(idStr, 10, 64)
			if convErr != nil || id <= 0 {
				return ModRef{}, fmt.Errorf("invalid nexus mod id %q in path %q", idStr, u.Path)
			}
			return ModRef{GameDomain: game, ModID: id}, nil
		}
	}

	return ModRef{}, fmt.Errorf("invalid nexus url: missing /mods/<id> in path %q", u.Path)
}
