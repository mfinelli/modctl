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

// Package argresolver turns what a command is given (a flag or an argument)
// into what it names, in the database of modctl, and says what is wrong when it
// can't. The functions are named after what they return:
//
//   - GameInstall and Profile are for a flag that can be left out, which then
//     means the one that is active (the game install by itself, the profile
//     of a game install);
//   - ExplicitGameInstall is for a game install that is always given;
//   - ModPage, ModFileVersion and EnabledProfileVersion are for mods, by their
//     id or by the name of their mod page;
//   - ProfileItemByVersion is for the item that a version is in a profile;
//   - Relpath is for a path in a target, which has no database to look in but
//     has to be inside of it.
package argresolver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/state"
)

// GameInstall is GameInstallIn for the state directory of the user.
//
// Transitional: see state.DefaultDir.
func GameInstall(ctx context.Context, q *dbq.Queries, arg string) (dbq.GameInstall, error) {
	return GameInstallIn(ctx, q, state.DefaultDir(), arg)
}

// GameInstallIn resolves the game install that a command is for: the one
// that arg says (see ExplicitGameInstall), or the active one, from the state
// directory stateDir, if arg is empty, which is what leaving out --game means.
func GameInstallIn(ctx context.Context, q *dbq.Queries, stateDir string, arg string) (dbq.GameInstall, error) {
	if arg == "" {
		active, err := state.LoadActiveIn(stateDir)
		if err != nil {
			return dbq.GameInstall{}, fmt.Errorf("load active selection: %w", err)
		}
		if active.ActiveGameInstallID == 0 {
			return dbq.GameInstall{}, errors.New("no active game selected; run `modctl games set-active ...` or pass --game")
		}
		arg = strconv.FormatInt(active.ActiveGameInstallID, 10)
	}

	return ExplicitGameInstall(ctx, q, arg)
}

func ExplicitGameInstall(ctx context.Context, q *dbq.Queries, arg string) (dbq.GameInstall, error) {
	// Fast path: numeric ID
	if id, ok := internal.ParseInt64(arg); ok {
		gi, err := q.GetGameInstallByID(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return dbq.GameInstall{}, fmt.Errorf("no game install with id %d", id)
			}
			return dbq.GameInstall{}, fmt.Errorf("get game install by id: %w", err)
		}
		return gi, nil
	}

	// Selector path: only attempt if arg contains ':'
	if strings.Contains(arg, ":") {
		// If user provided an explicit instance, lookup is unambiguous.
		storeID, storeGameID, instanceID, parseErr := internal.ParseSelector(arg)
		if parseErr == nil {
			gi, err := q.GetGameInstallBySelector(ctx, dbq.GetGameInstallBySelectorParams{
				StoreID:     storeID,
				StoreGameID: storeGameID,
				InstanceID:  instanceID,
			})

			if err == nil {
				return gi, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return dbq.GameInstall{}, fmt.Errorf("get game install: %w", err)
			}

			// DB miss: if instance was explicitly provided, don't fall through
			// to name search, the user was specific about what they wanted.
			// ParseSelector defaults to "default" when omitted, so we need to distinguish.
			// We'll treat the input containing '#' as "explicit instance".
			if strings.Contains(arg, "#") {
				return dbq.GameInstall{}, fmt.Errorf("no game install found for %s",
					internal.FullSelector(storeID, storeGameID, instanceID))
			}

			// No explicit instance and selector missed: also try the multi-instance
			// path before falling through, in case store:gameid is unambiguous.
			rows, lerr := q.ListGameInstallsByStoreGameID(ctx, dbq.ListGameInstallsByStoreGameIDParams{
				StoreID:     storeID,
				StoreGameID: storeGameID,
			})
			if lerr != nil {
				return dbq.GameInstall{}, fmt.Errorf("list candidates: %w", lerr)
			}
			if len(rows) == 1 {
				return rows[0], nil
			}
			if len(rows) > 1 {
				var b strings.Builder
				fmt.Fprintf(&b, "Multiple installs found for %s:%s. Choose one:\n\n", storeID, storeGameID)
				for _, r := range rows {
					sel := internal.FullSelector(r.StoreID, r.StoreGameID, r.InstanceID)
					present := "present"
					if r.IsPresent == 0 {
						present = "missing"
					}
					lastSeen := ""
					if r.LastSeenAt.Valid {
						lastSeen = r.LastSeenAt.String
					}
					fmt.Fprintf(&b, "  %s  (%s)  %s  %s\n", sel, r.DisplayName, present, lastSeen)
				}
				return dbq.GameInstall{}, errors.New(strings.TrimRight(b.String(), "\n"))
			}
			// Zero results from store:gameid lookup -> fall through to name search
			// using the raw arg (e.g. "My Game: the sequel")
		}
		// ParseSelector failed or store:gameid found nothing -> fall through to name search
	}

	// Name search: case-insensitive, all installs including missing
	rows, err := q.GetGameInstallsByName(ctx, arg)
	if err != nil {
		return dbq.GameInstall{}, fmt.Errorf("get game installs by name: %w", err)
	}
	switch len(rows) {
	case 0:
		return dbq.GameInstall{}, fmt.Errorf("no game install found for %q", arg)
	case 1:
		return rows[0], nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "Multiple installs found for %q. Be more specific:\n\n", arg)
		for _, r := range rows {
			sel := internal.FullSelector(r.StoreID, r.StoreGameID, r.InstanceID)
			present := "present"
			if r.IsPresent == 0 {
				present = "missing"
			}
			lastSeen := ""
			if r.LastSeenAt.Valid {
				lastSeen = r.LastSeenAt.String
			}
			fmt.Fprintf(&b, "  %s  (%s)  %s  %s\n", sel, r.DisplayName, present, lastSeen)
		}
		return dbq.GameInstall{}, errors.New(strings.TrimRight(b.String(), "\n"))
	}
}
