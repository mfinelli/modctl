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

package cmd

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/spf13/viper"
)

// The packages in internal/ are given what they work on: they don't look for
// the database, or the directory of the state, by themselves. This is where the
// commands get it from, for what they do all the time, so that a command
// doesn't have to know where it is (and can't have it wrong).

// databasePath is the file of the database that is configured.
func databasePath() string {
	return viper.GetString("database")
}

// stateDir is the directory that modctl keeps its state in, which is the
// active selection and what has to be there while something is being done (the
// locks, in a directory of their own).
func stateDir() string {
	return filepath.Join(xdg.StateHome, "modctl")
}

// cacheDir is the directory that the cache of what Nexus Mods said is kept in.
func cacheDir() string {
	return viper.GetString("cache_dir")
}

// nexusCacheDBPath is the file of the cache of what Nexus Mods said.
func nexusCacheDBPath() string {
	return nexusclient.CacheDBPath(cacheDir())
}

// scratchDir is the directory that has room for a copy of what is exported or
// imported: the snapshots of the databases that are made for an export, and the
// bundle that is extracted for an import (which has all of the blobs in it).
// It is the temporary directory of the system, and not tmp_dir: that one is for
// the staging of the archives that are applied, and is by default on tmpfs,
// which is not where a bundle that is as big as all of the mods has to go.
func scratchDir() string {
	return os.TempDir()
}

// openDB opens the database that is configured, which has to be there (it is
// made by init) and is migrated to the latest schema. The caller closes it.
func openDB(ctx context.Context) (*sql.DB, error) {
	return database.Open(ctx, databasePath())
}

// gameInstall resolves the game install that a command is for, from what it was
// given (the --game flag, which can be left out for the active game).
func gameInstall(ctx context.Context, q *dbq.Queries, arg string) (dbq.GameInstall, error) {
	return argresolver.GameInstall(ctx, q, stateDir(), arg)
}

// completer is what completes the arguments of the commands, for the database
// and the state that are configured. It is made when a completion is asked
// for, since the commands are set up before the configuration is read.
func completer() completion.Completer {
	return completion.Completer{
		DatabasePath: databasePath(),
		StateDir:     stateDir(),
	}
}

// nexusClient is the client of the API of Nexus Mods, with the cache and the
// rate limits that are configured.
func nexusClient(ctx context.Context, apiKey string, logger *slog.Logger) (*nexusclient.Client, error) {
	return nexusclient.New(ctx, apiKey, logger, rootCmd.Version, cacheDir(), stateDir())
}

// nexusCacheReader is what reads the cache of what Nexus Mods said, for the
// commands that don't need the API.
func nexusCacheReader(ctx context.Context, logger *slog.Logger) (*nexusclient.CacheReader, error) {
	return nexusclient.NewCacheReader(ctx, logger, cacheDir())
}

// loadRateLimitState is the rate limits that the API said last.
func loadRateLimitState() (*nexusclient.RateLimitState, error) {
	return nexusclient.LoadRateLimitState(stateDir())
}
